# Release process

`msgin` is a multi-module monorepo (ADR 0003, amended by ADR 0011 for the `sql` adapter split — spec 002 §8).
Releases are **tag-driven**: pushing a tag is the distribution event; `.github/workflows/release.yml`
publishes a GitHub Release with auto-generated notes, nothing is compiled or uploaded (CLAUDE.md
"License & release"). This document is the release choreography referenced from spec 002 §8 and ADR 0011.

## Modules in this repo

| Module | Path | go.mod |
| --- | --- | --- |
| engine (root) | `github.com/kartaladev/msgin` | `/go.mod` |
| harness | `github.com/kartaladev/msgin/adapter/database/sql/harness` | `adapter/database/sql/harness/go.mod` |
| postgres | `github.com/kartaladev/msgin/adapter/database/sql/postgres` | `adapter/database/sql/postgres/go.mod` |
| mysql | `github.com/kartaladev/msgin/adapter/database/sql/mysql` | `adapter/database/sql/mysql/go.mod` |
| dbtest (runner, never published/tagged — nobody imports it) | `github.com/kartaladev/msgin/adapter/database/sql/dbtest` | `adapter/database/sql/dbtest/go.mod` |

`dbtest` is a leaf test-only runner (spec 002 §4, "Structure Z") that a real consumer never imports, so it is
never tagged or released independently — it only needs to stay green in CI.

## Tag order — root FIRST, then dialect/harness modules

Each non-root module's `go.mod` currently carries a **dev-time-only** `replace github.com/kartaladev/msgin =>
../../../..` (and `dbtest` additionally replaces the sibling dialect/harness modules) so the on-branch,
unpublished engine resolves locally. That `replace` is what CI's `GOWORK=off` per-module jobs and the local
`go.work` workspace both rely on. It must be swapped for a pinned `require` before a dialect/harness module can
be tagged and consumed by the outside world, and that swap can only happen **after** the root itself has a
published tag to pin to. Hence the fixed order:

1. **Tag the root module first:** `git tag -a vX.Y.Z -m "vX.Y.Z" && git push origin vX.Y.Z`. The `release`
   workflow fires on the `v[0-9]+.[0-9]+.[0-9]+*` tag pattern and publishes the GitHub Release for the engine.
2. **For each dialect/harness module being released** (`harness`, `postgres`, `mysql` — `sqlite` joins this
   list in increment B, ADR 0012):
   - Edit that module's `go.mod`: remove the dev-time
     `replace github.com/kartaladev/msgin => ../../../..` line, and change
     `require github.com/kartaladev/msgin v0.0.0` to the pinned, just-published version, e.g.
     `require github.com/kartaladev/msgin vX.Y.Z`.
   - Run `GOWORK=off go mod tidy` inside that module directory and commit the updated `go.mod`/`go.sum`.
   - Tag it with its **module-path-prefixed** SemVer, matching its module path exactly:
     `git tag -a adapter/database/sql/postgres/vA.B.C -m "postgres vA.B.C"` (same pattern for `mysql` and
     `harness`), then `git push origin adapter/database/sql/postgres/vA.B.C`.
   - The `release` workflow's module-prefixed tag patterns
     (`adapter/database/sql/{postgres,mysql,harness}/v[0-9]+.[0-9]+.[0-9]+*`) pick this up and publish a
     GitHub Release titled with the module path and version.
3. A tag with a `-` suffix in its version (e.g. `v0.0.1-rc.1`, `adapter/database/sql/postgres/v0.1.0-rc.1`) is
   published as a **pre-release** — the workflow detects the `-` and passes `--prerelease` to
   `gh release create`.

**Until step 2 happens for a given module, that module is not independently consumable** — it is still
resolved via the dev-time `replace`, which is exactly why CI's per-module jobs (`.github/workflows/ci.yml`)
run with `GOWORK=off`: they prove each module builds/tests/tidies against what its own `go.mod` actually
declares (the `replace`d local engine right now; a pinned `require` after this dance), not against the
convenience of the local `go.work` workspace.

## `go.work`

The repo-root `go.work` is **committed** (unlike the historical default of gitignoring it) — it ties the
5 modules (root + harness + postgres + mysql + dbtest) together for local development, so a contributor gets
one coherent workspace with `go build ./...` resolving across all of them without hand-editing `replace`
directives. `go.work.sum` is deliberately **not** committed (still gitignored): it accumulates whatever the
workspace happens to have built locally (including `dbtest`'s heavy testcontainers/Docker/OTel closure) and is
not a stable, reproducible artifact across machines — each module's own `go.sum` remains the source of truth.

CI never relies on `go.work` for the per-module correctness jobs (`GOWORK=off`, see above); a separate
`workspace` job in `ci.yml` builds every module with the workspace active (`GOWORK` unset/default) purely to
prove the workspace itself stays coherent.

## A known, temporary go.sum artifact of the dev-time local `replace`

While a dialect/harness module still points its `replace github.com/kartaladev/msgin` at the local filesystem
(step 1 not yet done for the current release), `go mod tidy` under `GOWORK=off` pulls the **root module's own
test-only dependencies** (`stretchr/testify`, `go.uber.org/goleak`, and their transitives) into that module's
`go.sum` — e.g. `postgres/go.sum` currently carries ~12 lines for `clockwork` + `testify` + `goleak` +
transitives, even though `postgres` never imports either package. This happens because a module replaced by a
**local directory** is not `go.sum`-verified, so Go's module-graph pruning cannot rely on cached/checksummed
data the way it does for a real, tagged, proxy-resolved dependency — it conservatively loads the replaced
module's full test-dependency graph too. **This resolves itself automatically at step 2**: once the `replace`
is swapped for a pinned `require github.com/kartaladev/msgin vX.Y.Z` resolved via the module proxy, standard
Go 1.17+ graph pruning applies normally and a dialect consumer's `go.sum` no longer carries the engine's
test-only dependencies.

This is *not* the isolation defect this refactor fixes (see below) — `testify`/`goleak` are the **engine's
own** lightweight test deps, never the heavy driver/testcontainers/Docker/OpenTelemetry closure. It is called
out here so a future release engineer isn't surprised by it, and isn't tempted to "fix" it by hand-editing
`go.sum` (`go mod tidy` under `GOWORK=off` is deterministic and idempotent — verified — so the committed
`go.sum` for `harness`/`postgres`/`mysql` on this branch is exactly its current tidy output).

## The isolation goal (spec 002 §1, audit finding F1) — empirically verified

The entire point of the engine/dialect module split is that a consumer importing dialect *production* code
must not inherit the driver/testcontainers/Docker/OpenTelemetry closure that only `dbtest`'s container tests
need. This was re-verified with two throwaway consumer probes (Plan 006 Task 6), each resolving the
unpublished modules via **its own local `replace`** (a dependency's own `replace` directives are never seen by
a downstream consumer, so the probe must declare them itself) and then `GOFLAGS=-mod=mod GOWORK=off go mod
tidy`:

| Probe | Imports | Heavy lines in `go.sum`\* | Result |
| --- | --- | --- | --- |
| engine-only | `github.com/kartaladev/msgin/adapter/database/sql` (`NewOutboundAdapter`) | **0** | builds clean |
| postgres | `github.com/kartaladev/msgin/adapter/database/sql/postgres` (`postgres.LeaseDialect()`) | **0** | builds + runs clean |

\* "Heavy lines" = `go.sum` lines matching `pgx|go-sql-driver|testcontainers|docker|moby|containerd|opentelemetry|gopsutil` (spec 002 §1's empirically-measured baseline for the **old**, co-located layout was **102** such lines; audit F1 measured **0** for the leaf-test layout — both probes above confirm **0** on the actual refactored tree).

Each probe's full (non-heavy) `go.sum` is the same ~6-package set described above (`clockwork` + the dev-time
`testify`/`goleak` leak) — proof that the split achieves goal (a)/(b) of spec 002 §2: the root module and any
core-only or single-dialect consumer carry zero driver/testcontainers/Docker/OTel dependency, even before the
root is tagged.

## Related documents

- [Spec 002 — sql multi-module split + SQLite](specs/002-sql-multi-module-and-sqlite.md) §8 (workspace,
  tagging, CI) and §1 (the isolation motivation/measurement).
- [ADR 0011 — Split the sql adapter into a driver-free engine + per-dialect modules](adrs/0011-sql-engine-dialect-module-split.md).
- [Plan 006 — sql engine/dialect module split](plans/006-sql-engine-dialect-split.md), Task 6.

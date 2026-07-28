# Derivation-run brief (read fully before acting)

## What this is

A **mechanical derivation run** for the msgin pre-v1 core refactor. Two hand-written design audits failed
(3/3 auditors each) because the move-list was **hand-typed and asserted as verified**. The method changed:
perform the migration first, let the compiler prove it, then **generate** the move-list from the green tree.

**This run produces FACTS, not a shipping deliverable.** Do not optimise for elegance; optimise for
*a green tree whose structure can be measured*.

## Where to work

- **Work tree:** `/Users/zakyalvan/Documents/RND/msgin`, branch `claude/repo-structure-refactor-jt79t1`.
  The migration was COMMITTED (`c83dde9`) and fast-forwarded into this branch on 2026-07-28.
- `docs/` here is the LIVE, authoritative design set — 19 files, **uncommitted**. Read it; edit only what
  your task names. The old `../msgin-derive` worktree is now redundant; ignore it.
- **Do not run `git checkout`, `git stash`, `git reset`, or `git clean` in either tree.**
- **Do not `git commit`** anywhere without explicit user approval.

## Environment (mandatory)

```bash
export GOTOOLCHAIN=go1.25.12                 # bare go1.25 is rejected
export PATH="$(go env GOPATH)/bin:$PATH"     # goimports, gofumpt, gopls, apidiff, gorelease live here, NOT on PATH
```

`gopls` has **no Move refactoring** and has been unreliable inside subagents. Use the scripts below.

## Tools already built (use these, do not rebuild)

| Tool | Purpose |
|---|---|
| `/tmp/msgin-derive/decls <dir>` | AST dump of every top-level decl: `file⇥line⇥kind⇥name⇥exported`. This is how every table gets generated. |
| `/tmp/msgin-derive/qualify <pkgdir> msgin <Sym>...` | **AST-based** rewrite of bare identifiers to `msgin.X`. Operates on the AST, so **comments and strings are never touched** (a regex version corrupted EIP pattern names in godoc — do not go back to regex). |
| `/tmp/msgin-derive/extract.sh <pkg> <files...>` | git mv + package clause + qualify + goimports + build. |

Regenerate the qualification target set after **every** move (root shrinks as you go):

```bash
/tmp/msgin-derive/decls . | grep -v '_test\.go' \
  | awk -F'\t' '$5=="exported" && $3!="method" {print $4}' | sort -u > /tmp/msgin-derive/root-exported.txt
```

## The green gate — run after EVERY step

`go build ./...` is **not sufficient** and is exactly how the last audit's biggest defect survived: it does
not compile tests, and it cannot see the six satellite modules.

```bash
go build ./...                                   # fast inner loop
go vet ./...                                     # compiles TEST binaries too
for d in . adapter/database/sql/harness adapter/database/sql/postgres adapter/database/sql/mysql \
         adapter/database/sql/sqlite adapter/database/sql/dbtest adapter/cron/crontest; do
  (cd "$d" && GOWORK=off go build ./... >/dev/null 2>&1 && GOWORK=off go vet ./... >/dev/null 2>&1 \
    && echo "GREEN: $d") || echo "RED: $d"
done
```

Full `GOWORK=off go test ./... -race` at each task boundary. `dbtest`/`crontest` need Docker.

## Settled decisions — these are USER decisions, do not revisit

| # | Decision |
|---|---|
| D-A | `BackoffStrategy` stays in root; `ExponentialBackoff`+`jitter` → `resilience`. The `endpoint→resilience` edge is **removed** via a local bounded `pollErrorBackoff`. **DONE.** |
| D-B | `TopicPublisher`/`TopicSubscriber` → **root `spi.go`**; `PubSub`/`NewPubSub`/`topicSubscription` → `channel`. **DONE.** |
| D-C | `Subscription` folds into **root `channel.go`**. **DONE.** |
| D-D | Delete `cfg.optErr` + its `NewAggregator` guard. **DONE.** |
| D-E | `WithReleaseStrategy(ReleaseStrategy)` (fallible) + `WithReleaseWhen(func(MessageGroup) bool)`. **DONE.** |
| D-F | Add `channel.WithSingleSubscriber()` reusing `ErrChannelSubscribed`, **off by default**. **NOT DONE.** |
| D-G | Task 8 splits into 8a/8b. (sizing only) |
| D-H | **FORCED:** `endpoint` must not read `Message`'s unexported fields. Rewrite over `msgin.NewMessage[T](payload, m.Headers())` + `m.Payload()`. **NEVER `msgin.New[T]`** — it re-stamps `msgin.message-id`/`msgin.timestamp` and no assertion would catch the regression. |

## Progress so far — the migration is COMPLETE and GREEN

Committed as `c83dde9` and merged into `claude/repo-structure-refactor-jt79t1` (2026-07-28).

Done: Task 1 (expr removal, root `doc.go`, `expr-lang` dropped), Task 3.5 (exported
`IsPermanent`/`RetryAfterOf`/`NewID`; `RetryPolicy.delayFor` -> package-local `retryDelay`), the full
extraction of `endpoint`/`routing`/`transform`/`channel`/`resilience`, the 44-file test placement, and the
adapter + satellite-module requalification.

Root is **14** non-test files: `backoff.go channel.go codec.go doc.go errors.go flowcontrol.go
groupstore.go handler.go message.go payload.go reliability.go retry.go spi.go store.go`.

**All seven modules GREEN**: build + vet + `test -race` (Docker-backed `dbtest`/`crontest` run for real).

Findings live in `docs/plans/027-derivation-findings.md` (in-repo, authoritative) — F0..F9, every number
paired with the command that produced it. **Append every new finding there**; a number with no pasted
command is worthless to this run.

### Coverage — read before touching any verification step

Default per-package coverage shows root at **81.8%** (was 99.3%), *below* CLAUDE.md's 85% gate. With
`-coverpkg=./...` the workspace is **93.2%** (baseline 91.9%). Nothing regressed: blackbox tests moved to
sibling packages and coverage is credited where the *test* lives. **Always measure with `-coverpkg=./...`
and compare against a `-coverpkg` baseline** — a default-vs-default comparison across the split is not
like-for-like and fails the gate falsely.

## Remaining work

- **Task D** — D-F (`channel.WithSingleSubscriber()`), Plan 027 Task 2 (`MessageChannel` segregation), and
  Plan 027 Task 3 (`StreamingSource` -> `EventDrivenSource`).
- **Task E** — generate the §3.2 declaration-level split tables and the `apidiff` surface diff, then rewrite
  Spec 014 §3 + Plan 027 from the generated output and run the round-3 audit.

## Snapshots

`/tmp/msgin-derive/snapshots/after-task1.tgz`. Take one at each green boundary:
`tar czf /tmp/msgin-derive/snapshots/<name>.tgz --exclude='.git' .`

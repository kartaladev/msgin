# HANDOVER — msgin

> **Next session: read this first, then trust the referenced files over any memory.** Read, in order:
> `CLAUDE.md`, then the **audited, implementation-ready** design bundle for the latest increment —
> `docs/specs/008-expr-endpoints.md`, `docs/adrs/0019-runtime-expression-evaluation.md`, and
> `docs/plans/014-expr-endpoints.md`. **Plan 014 is DONE and MERGED to `main`** (merge `c53ea09`, `--no-ff`;
> pushed; `feat/expr-endpoints` deleted, was local-only). Start the NEXT increment from a fresh branch off `main`.
> The SDD progress ledger `.superpowers/sdd/progress.md` (gitignored, local) holds per-task history; trust it +
> `git log` over memory.

## Latest increment — Plan 014: runtime-expression endpoints (`expr-lang/expr`) (2026-07-20, MERGED)

**What shipped.** Spring-SpEL-style **runtime-expression** Filter/Router endpoints in the core `msgin` package —
predicates/routes supplied as *strings* at runtime — with **no change to the `Filter`/`Router` runtime**.

- **`FilterExpr[A any](expression string, opts ...FilterOption) (Step, error)`** — a bool predicate expr; compiles
  once (`expr.AsBool`), returns `ErrInvalidExpression` on a bad/mistyped expr at construction, then **delegates to
  the existing `Filter`**.
- **`RouterExpr[A any](keyExpr string, routes map[string]MessageChannel, opts ...RouterOption) (*Router, error)`** —
  a routing-key expr (`expr.AsKind(String)`) → `routes[key]` → default/`ErrNoRoute`; **delegates to `NewRouter`**.
  Empty/nil routes or a literal-nil channel value → `ErrInvalidExpression`. (A ternary key gives multi-way routing;
  a separate `RouterExprCases` was **cut** — O8-2.)
- **Env / evaluation model (SpEL-shaped):** an expression references `payload` (the typed `A`, field access
  type-checked when `A` is concrete) and `header("key")` (function form — zero-alloc, preserves the `Headers`
  immutability invariant). Shared unexported `compile[A]` primitive; extensible context (custom functions) deferred.
- **Dependency:** accepts **`github.com/expr-lang/expr` v1.17.8** as the **4th core dependency** — verified
  **zero-transitive** (the acceptance gate: `go.sum` vs main added only `expr-lang/expr`). CLAUDE.md's dependency
  policy updated to list all four core deps (clockwork, cenkalti/backoff, robfig/cron, expr).
- **Errors/security (accurate, documented):** construction errors up front; eval errors flow into retry/DLQ. expr
  is sandboxed (non-Turing-complete, no I/O) with default limits (`MaxNodes=1e4`/`MemoryBudget=1e6`); the residual
  caveat (no time budget; `vm.Run` not context-cancellable) is on the godoc. Suitable for operator/config-authored
  expressions; the Go-func `Filter`/`Router` remain the compile-time default.

**Process & gate.** Design **2-round adversarial-audited → SOUND** (round 1 verified every expr API assumption via a
real v1.17.8 compile+run spike). Built via `superpowers:subagent-driven-development` (Task 1 FilterExpr+dep+design
docs `200b9b2`; Task 2 RouterExpr `90d0ec9`; Task 3 examples+doc `120f589`; review fix-pass `a808ba8`), each
task-reviewed. **Whole-branch gate PASSED:** `/code-review` (3 findings — a concurrent `-race` test
`TestFilterExpr_Concurrent` proving the compiled `*vm.Program` is concurrent-safe under the worker pool + RouterExpr
collector isolation FIXED; header-closure per-eval alloc triaged/accepted) + `/security-review` (**no findings** —
expressions are caller/operator-authored, not untrusted-data-derived; no injection/RCE/leak path); `go test ./...
-race` green; `golangci-lint` 0; `govulncheck` clean; zero-transitive acceptance gate; `CGO_ENABLED=0` ok; tidy
clean. Additive API → minor SemVer.

**Exact state.** `main` @ merge `c53ea09`. Working tree: only `.claude/settings.json` modified (pre-existing,
unrelated, intentionally never staged).

## Roadmap / next actions

- **Spec 008 expr futures (deferred, non-breaking):** an extensible evaluation context — `WithExprEnv`/custom
  functions & variables (O8-3, SpEL's real power); expr on more endpoints (Transformer projection, header
  enrichment, and — the canonical SpEL use — a future `Aggregator` correlation/release, `Splitter` collection),
  each reusing the `compile[A]` primitive; export the primitive when those land (O8-5).
- **Spec 006 O6-1 — Redis/etcd cron coordinators** (optional modules) remains open from Plan 011.
- **Spec 007 futures:** other `ChannelStore` backends (Redis/pgx/NATS, O7-1); a priority channel (O7-3); the
  id-addressable `MessageStore` base with Claim Check.
- Long-standing deferrals: EIP Delayer, `memory` delayed-send, pgx/redis/nats/http adapters, Plan 005 T11.

## Prior increments (for reference)

- **Plan 013 — persistent queue channel + `ChannelStore` SPI**: DONE and MERGED (merge `26f16b8`). `QueueChannel` +
  `memory.QueueStore`/`sql.QueueStore`. Spec 007 / ADR 0018. (Plus the ADR 0002 addendum on channel-adapter
  classification, merge `da04591`.)
- **Plan 012 — `cron.WithSeconds()`**: MERGED (`d315f52`). **Plan 011 — cron source + coordination**: MERGED
  (`c62da27`). Spec 006 / ADR 0016/0017.

_Updated 2026-07-20: Plan 014 (expr runtime-expression endpoints) merged to `main` (`c53ea09`)._

# HANDOVER — msgin

> **Next session: read this first, then trust the referenced files over any memory.** Read, in order:
> `CLAUDE.md`, then the design bundle for the active increment — `docs/specs/006-cron-source.md`,
> `docs/adrs/0016-robfig-cron-dependency.md`, `docs/adrs/0017-cron-source.md`. **Plan 011 is NOT yet written —
> §5 below is your first task.** §6 carries the grounded implementation facts so you can write Plan 011 WITHOUT
> re-exploring the codebase.

_Updated 2026-07-19: **Cron/recurring-source increment is DESIGNED through spec + both ADRs (Spec 006, ADR 0016,
ADR 0017) but NOT yet planned or audited or coded.** This is a clean design safepoint (docs only on branch
`feat/cron-source`; tree builds; no `.go` changes). Reached proactively for context hygiene after delivering
Plans 009 and 010 this session. **Next: write Plan 011 → 2-round adversarial audit → ask user go-ahead + SDD mode
before any code.**_

## 1. Objective & roadmap position

`msgin` (`github.com/kartaladev/msgin`) — Go 1.25 EIP library, minimal deps, multi-module monorepo. `main` @
`410a45e` carries Plans 001–010 (core + reliability + resilience + `sql`/`memory`/dialects + composition Phase 1 +
Phase 3 Publish-Subscribe + scheduled/delayed send), all merged.

**Active increment = Spec 006 — cron / recurring message source + distributed coordination.** A recurring/cron
`Source[T]` (a `StreamingSource`) that emits a caller-defined message on each schedule fire, driven by the
existing runtime; PLUS msgin-native multi-instance single-fire via an `Elector` (leader) + `Locker` (per-fire)
seam with **dependency-free SQL-backed concrete implementations of both**. This is the home for the "rich cron
trigger kinds" the user asked for (correctly on the inbound/source side, not the delayed-send path). Un-defers
Spec 005 O5-5.

**Roadmap after this:** (Spec 006 open items) Redis/etcd-backed coordinators (O6-1, optional modules), seconds-field
cron `WithSeconds` (O6-2); plus still-deferred: EIP `Delayer` composition step (Spec 005 O5-1), memory delayed-send
(O5-2), pgx/redis/nats/http adapters, Plan 005 T11 examples, Phase-4 fluent DSL (gated).

## 2. Exact state (safepoint — design spec+ADRs done, no plan, no code)

- **Branch:** `feat/cron-source` (off `main` @ `410a45e`). Nothing committed on it yet.
- **`git status --short`:** `.claude/settings.json` (user's own — leave untouched) + **untracked**
  `docs/specs/006-cron-source.md`, `docs/adrs/0016-robfig-cron-dependency.md`, `docs/adrs/0017-cron-source.md`,
  and this HANDOVER (modified). No `.go` changes → `go build ./...` and the suite are unaffected.
- **Nothing audited, nothing coded.** `robfig/cron/v3` is NOT yet in any `go.mod`.

## 3. Locked design decisions (Spec 006 §3 D1–D12 / ADR 0017)

- **D1 — cron source in the ROOT module** (`adapter/cron` package, like `adapter/memory`), NOT a separate module —
  user's explicit choice. **`robfig/cron/v3` accepted as the 3rd core dependency** (ADR 0016), justified because it
  is **MIT + zero-dependency + pure-Go** (VERIFIED 2026-07-19: its go.mod declares no requires; LICENSE is MIT ©
  2012 Rob Figueiredo). Alternatives (own module / in-house parser) declined.
- **D2** — `Source[T]` is a `StreamingSource` (no core runtime change); **D4** also `LiveValueSource` (live values,
  no codec, like memory). **D3** — `cron.NewSource[T](spec, factory func(fire time.Time) T, opts...)`.
- **D5** — goleak-clean loop (memory.Broker.Stream template, NO background goroutine); recompute `next` each
  iteration → **skip-missed on overrun**. **D6** — at-most-once, no-op Ack/Nack (runtime RetryPolicy handles
  transient handler failure in-process).
- **D7** — `WithClock(clockwork.Clock)` (nil=real), `WithLocation(*time.Location)` default **UTC**. **D8** —
  construction validates: `ErrInvalidSchedule`, `ErrNilFactory`.
- **D9 — coordination checked ON-DEMAND per fire (no heartbeat goroutine).** `Elector{ IsLeader(ctx) (bool,error) }`
  gates ALL fires; `Locker{ Claim(ctx, scope string, fire time.Time) (won bool, err error) }` gates ONE fire.
  `WithElector`/`WithLocker`; at most one (both → `ErrConflictingCoordinator`); a coordinator ERROR → **skip the
  fire fail-safe** (never N-fold fire). No coordinator → fires on every instance (documented footgun).
- **D10 — SQL `Locker` = per-fire dedup-INSERT** on `(scope, fire_ts)` — reuses the InboxDeduper mechanism; winner
  emits. Recommended primitive (no failover gap). **D11 — SQL `Elector` = leader-lease** `(scope PK, holder,
  expires_at)` atomic acquire-or-renew on-demand; failover latency ≤ lease TTL (documented). **D12** —
  `WithInstanceID(string)` (default per-process random), the holder/claimant identity.
- **Both SQL coordinators:** `database/sql` only (driver injected), DB-server-clock for all time math (skew-free),
  a `LockerDialect`/`ElectorDialect` seam (PG/MySQL/SQLite) mirroring `InboxDialect`, opt-in `EnsureSchema`/DDL.
- **Delivery:** ONE phased Plan 011 (user chose not to split), phases per Spec 006 §6.

## 4. Decisions/deviations & the design-time gate

- **Design bundle is NOT audited yet.** CLAUDE.md hard rule: after spec + ADR(s) + plan all exist, run a fresh
  Opus adversarial audit of the WHOLE bundle (2-round norm), fold findings, THEN ask the user for go-ahead + SDD
  mode before code. **Plan 011 must be written first, then the audit.**
- gocron was researched and REJECTED as the engine (3 deps + scheduler goroutine fighting goleak/clockwork);
  robfig/cron parser + our own loop chosen. gocron is reserved for a possible future Redis/etcd-locker analogue,
  not this increment.

## 5. Next actions (in order)

1. **Write Plan 011** (`docs/plans/011-cron-source.md`) using `superpowers:writing-plans`. It MUST re-state the Go-skills
   hard rule in header + Global Constraints (CLAUDE.md writing-plans override). Phases per Spec 006 §6:
   - **Task 1:** add `robfig/cron/v3` to root `go.mod` + `adapter/cron` scaffold + `Source[T]` (parse via
     `cron.ParseStandard`, the loop, factory, clock/location, sentinels) — fake-clock unit tests (root, no DB).
   - **Task 2:** coordination SPI (`Elector`/`Locker` + `WithElector`/`WithLocker` gating + fail-safe skip +
     `ErrConflictingCoordinator`) — fake-coordinator unit tests (root, no DB).
   - **Task 3:** SQL `Locker` (`NewSQLLocker`, `LockerDialect` PG/MySQL/SQLite, DDL builders, `Purge`, dedup verdict
     per §6) — driver-free Go-logic unit test in root (mirror `fakedialect_test.go` style); REAL-DB conformance in
     the leaf module (Task 5).
   - **Task 4:** SQL `Elector` (`NewSQLElector`, `ElectorDialect` PG/MySQL/SQLite, atomic acquire-or-renew, TTL) —
     driver-free Go-logic unit test in root; REAL-DB conformance in the leaf module (Task 5). HARDEST correctness
     surface — the audit scrutinizes this.
   - **Task 5:** NEW leaf-test module `adapter/cron/crontest` (own go.mod, testcontainers, added to `go.work`) — real
     PG/MySQL/MariaDB/SQLite conformance for BOTH coordinators (keeps root driver/testcontainer-free per Plan 006).
   - **Task 6:** end-to-end example (`NewConsumer` over a cron `Source`) + package doc + whole-branch gate.
2. **2-round adversarial Opus audit** of spec+ADR0016+ADR0017+Plan011 together; fold findings; re-audit if
   destabilized. Save records to `.superpowers/sdd/plan-011-audit-round-{1,2}.md`.
3. **Ask the user** for explicit go-ahead + execution mode (SDD recommended). Do NOT code before that.
4. Execute via SDD (fresh implementer per task → coordinator verify+commit → adversarial reviewer). Per-task commits
   pre-authorized by the approved plan; **merge/push needs explicit approval.**

## 6. GROUNDED implementation facts (so you can write Plan 011 without re-exploring)

**robfig/cron/v3 API** (verified): `type Schedule interface { Next(time.Time) time.Time }`;
`func ParseStandard(spec string) (Schedule, error)` accepts 5-field cron + `@every 1h30m` + descriptors
(`@daily/@hourly/@weekly/@monthly/@yearly/@midnight`). TZ: pass `schedule.Next(clock.Now().In(location))`; delay =
`next.Sub(clock.Now())`. Import `github.com/robfig/cron/v3`.

**Source loop** (mirror `adapter/memory/memory.go:59` `Broker.Stream`; NO goroutine): per iteration
`next := sched.Next(clock.Now().In(loc))`; `select { case <-clock.After(next.Sub(clock.Now())): [gate?] emit; case
<-ctx.Done(): return ctx.Err() }`; emit builds `msgin.New[any](factory(next), msgin.WithClock(clock))` (stamps
`msgin.id`+`msgin.timestamp`) and `select { case out<-Delivery{Msg,Ack:noop,Nack:noop}: case <-ctx.Done(): return }`.
Compile asserts: `_ msgin.StreamingSource`, `_ msgin.LiveValueSource` (EmitsLiveValue→true).

**Dedup SQL for the Locker** (mirror InboxDeduper `InsertInboxIfAbsent`), verdict = did-I-insert:
- **PG** (`postgres/dialect.go:258`): `INSERT INTO %s (scope, fire_ts) VALUES ($1,$2) ON CONFLICT (scope,fire_ts) DO
  NOTHING RETURNING scope` → `.Scan`; `errors.Is(err, sql.ErrNoRows)` ⇒ lost (already), a row ⇒ won.
- **SQLite** (`sqlite/dialect.go:177`): same `ON CONFLICT ... DO NOTHING RETURNING` shape; `ErrNoRows` ⇒ lost.
- **MySQL/MariaDB** (`mysql/dialect.go:365`): `INSERT IGNORE INTO %s (scope, fire_ts) VALUES (?,?)`; if
  `RowsAffected()==1` ⇒ won; if 0 ⇒ verify with `SELECT ... WHERE scope=? AND fire_ts=? LOCK IN SHARE MODE` (NOT
  `FOR SHARE`, MariaDB-compat): row present ⇒ lost, `ErrNoRows` ⇒ a demoted data error → return an
  `ErrInboxInsertFailed`-style error (never silently drop). Composite PK `(scope, fire_ts)`.

**Elector lease SQL** (mirror lease/claim table shape; DB-clock for expiry): row `(scope PK, holder, expires_at)`.
`IsLeader`: atomic acquire-or-renew — PG/SQLite `INSERT ... ON CONFLICT(scope) DO UPDATE SET holder=?,
expires_at=<db_now>+ttl WHERE <table>.holder=? OR <table>.expires_at < <db_now> RETURNING holder` then compare
holder==instanceID (RETURNING empty ⇒ not leader). MySQL: conditional `UPDATE ... SET holder=?,
expires_at=UTC_TIMESTAMP(6)+INTERVAL ? MICROSECOND WHERE scope=? AND (holder=? OR expires_at<UTC_TIMESTAMP(6))`;
`RowsAffected()>=1` ⇒ leader, else try `INSERT` (fails on dup ⇒ not leader). DB-now exprs: PG `now()`, MySQL
`UTC_TIMESTAMP(6)`, SQLite `CAST(unixepoch('now','subsec')*1000000 AS INTEGER)`.

**Dialect seam rules** (`adapter/database/sql/dialect.go`): every method takes `q Querier` + `table string`;
call `ValidateIdent(name)` (guard `^[A-Za-z_][A-Za-z0-9_]*$` → `ErrInvalidTableName`) via a quote helper FIRST;
`Querier` = ExecContext/QueryContext/QueryRowContext (both `*sql.DB` and `*sql.Tx` satisfy it). **DDL is NOT an
interface method** — expose `LockerDDL`/`ElectorDDL` as package-level `ValidateIdent`-first string builders (PG/SQLite
= separate table+index `IF NOT EXISTS` statements; MySQL = inline index in one CREATE TABLE).

**Leaf-test module** `adapter/cron/crontest` (mirror `adapter/database/sql/dbtest`): own go.mod, `go 1.25.0`,
require root + drivers (`jackc/pgx/v5 v5.10.0`, `go-sql-driver/mysql v1.10.0`, `modernc.org/sqlite v1.54.0`) +
testcontainers (`testcontainers-go v0.43.0` + modules `/postgres`,`/mysql`,`/mariadb` v0.43.0) + testify + goleak;
`replace github.com/kartaladev/msgin => ../../..` (crontest→root is THREE levels up, vs dbtest's four); **add
`./adapter/cron/crontest` to `go.work` use list**. Container helpers to mirror (in a `*_test.go`, package
`crontest_test`): `RunTestDatabase(t, opts...) *sql.DB` (PG, driver `pgx`, `postgres:16.10-alpine`), `RunTestMySQL`
(`mysql:8.0.40`), `RunTestMariaDB` (`mariadb:11.4`, both driver `mysql`), `RunTestSQLite` (no Docker;
`sql.Open("sqlite", sqlite.DSN(filepath.Join(t.TempDir(),"msgin.db")))`, blank-import `_ "modernc.org/sqlite"`).
`TestMain` = `goleak.VerifyTestMain(m, <testcontainers Ryuk/Docker-pool ignore list from dbtest>)`. Root go.mod must
stay driver/testcontainer-free (Plan 006 acceptance gate) — the ONLY new root dep is `robfig/cron/v3`.

**Instance identity** (`WithInstanceID` default): mirror `randomLockedBy()` (`options.go:231`) / `randomID()`
(`message.go:174`): `var b [16]byte; rand.Read(b[:]); hex.EncodeToString(b[:])` (crypto/rand + encoding/hex);
empty-string-means-unset defaulting at construction.

## 7. Gotchas / environment

- **Go 1.25 pinned:** `GOTOOLCHAIN=go1.25.12`. New root dep = `robfig/cron/v3` ONLY (verify `go mod graph` shows no
  transitive dep; `go mod tidy` clean). Root must NOT gain driver/testcontainer deps — those live in the crontest leaf.
- **Docker IS available** here (dbtest/crontest testcontainers run). `use-testcontainers` mandatory for real-DB tests.
- Custom skills (mandatory): start Go work from `cc-skills-golang:golang-how-to`; TDD; `gopls`; `table-test` /
  `use-mockgen` / `use-testcontainers`; blackbox `_test` packages.
- **`.claude/settings.json`** shows modified — user's own file; do not stage/commit.
- **Design bundle uncommitted** — see the offer to commit it (fresh-session-handoff exception) before continuing.

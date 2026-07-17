# Plan 007 — SQLite dialect (increment B, Structure Z)

> **For agentic workers:** REQUIRED SUB-SKILL — implement task-by-task via
> `superpowers:subagent-driven-development` (fresh implementer + reviewer per task). Every task is an
> independently green unit (the affected module's `GOTOOLCHAIN=go1.25.12 go test ./... -race` passes),
> blackbox (`package …_test`) where it applies, goleak-clean. Steps use checkbox (`- [ ]`) syntax.
> **Read `CLAUDE.md`, spec 002 (§7 SQLite, §9 sequencing), and ADR 0012 first; trust those over any memory.**
> Start Go work from `cc-skills-golang:golang-how-to`; use `gopls`/`LSP` for navigation + post-edit
> diagnostics. This plan is **purely additive** — it introduces a new `sqlite` dialect module and ONE
> behavior-preserving tweak to the `harness` module; it changes NO Postgres/MySQL behavior and NO engine API.

- **Spec:** [002 — sql multi-module split + SQLite](../specs/002-sql-multi-module-and-sqlite.md) (§7 SQLite is
  authoritative for this increment).
- **ADR:** [0012 — SQLite dialect](../adrs/0012-sqlite-dialect.md) (D1–D8); predecessor
  [0011](../adrs/0011-sql-engine-dialect-module-split.md) (Structure Z), builds on
  [0010](../adrs/0010-poller-sql-adapter.md).
- **Audit:** `.superpowers/sdd/plan-007-audit-round-1.md` (+ round 2 if the fixes destabilize the design) —
  run BEFORE execution, findings folded into this bundle (CLAUDE.md design-time gate).
- **Predecessor:** Plan 006 (increment A, MERGED to `main` @ `0e20df3`). **Successor:** Plan 005 **Task 11**
  (docs/examples, resumed after this increment) + the Plan-006 §4 minor follow-ups.
- **Branch:** `feat/sqlite-dialect` (fresh off `main`; already created).

**Goal:** Ship a driver-free `adapter/database/sql/sqlite` production module implementing the engine's
lease/claim + dedup-inbox SPIs for SQLite (lease-only — no lock strategy), certified against the existing
`harness` conformance suite via a Docker-free embedded run in `dbtest`.

**Architecture (Structure Z — spec 002 §4, unchanged):** the `sqlite` prod module requires the ENGINE ONLY
(dev-time `replace`, like `postgres`/`mysql`); it ships `LeaseDialect()`/`InboxDialect()`/`DDL`/`InboxDDL` plus
a pure-string `DSN` builder. The pure-Go, cgo-free driver `modernc.org/sqlite` is added to the `dbtest` runner
ONLY (nobody imports `dbtest`), which provisions an embedded SQLite via `RunTestSQLite` and runs the harness.
SQLite serializes writers, so the claim is a one-shot atomic `UPDATE … RETURNING` and there is no `LockDialect`.

**Tech Stack:** Go 1.25, stdlib `database/sql`. `sqlite` prod module: engine only (`clockwork` indirect), **no
test-deps**. `dbtest`: adds `modernc.org/sqlite` (pure-Go) + the `sqlite` module to its existing
harness/testify/testcontainers set.

## Global Constraints

- **Go 1.25**, `GOTOOLCHAIN=go1.25.12` (bumped from 1.25.0 in Plan 006 Task 6 after GO-2025-4007; the `go`
  directive in every `go.mod` stays `1.25.0`). No language/stdlib features > 1.25. `CGO_ENABLED=0` builds per
  module (`modernc.org/sqlite` is pure-Go — the no-cgo gate holds).
- **`sqlite` prod module requires the ENGINE ONLY** — zero driver/test-deps in its `go.mod`; `go mod tidy`
  leaves only `github.com/kartaladev/msgin` (+ `clockwork` indirect), matching `postgres`/`mysql`. Verify with
  `go list -deps` / `go mod graph`. The ONE exception: a stdlib-`testing`-only `dsn_test.go` in the module
  (imports nothing but the module itself + stdlib → adds nothing to `go.mod`); the dialect's own conformance
  tests live in `dbtest`.
- **Dependency points inward:** the engine imports no dialect and no driver; `sqlite` imports the engine only;
  `dbtest` imports `sqlite` + `harness` + `modernc.org/sqlite`. No cycles.
- **Identifier-injection discipline (ADR 0010 D3 / ADR 0011):** every `LeaseDialect`/`InboxDialect` method and
  both reference-DDL builders call `msginsql.ValidateIdent` (via `sqliteQuoteTable`, or directly where the
  identifier is also a bound param) BEFORE interpolating the identifier. There is NO string-returning DDL
  method on any SPI.
- **DB-clock invariant (ADR 0010 D3/D4, ADR 0012 D4):** every persisted timestamp comes from SQLite's own
  clock — `CAST(unixepoch('now','subsec')*1000000 AS INTEGER)` — never the app clock. Delays/lease-TTL are
  passed as integer microseconds (`time.Duration.Microseconds()`), matching PG/MySQL.
- **Lease-only (ADR 0012 D1):** `sqliteDialect` implements `LeaseDialect` + `InboxDialect`, NOT `LockDialect`.
  `WithStrategy(StrategyLockForUpdate)` with it returns the engine's existing `ErrLockStrategyUnsupported`.
- **SQLite version floor:** ≥3.42 (`unixepoch('now','subsec')`); `RETURNING` ≥3.35; `ON CONFLICT` ≥3.24 —
  all satisfied by `modernc.org/sqlite` (bundles ≥3.45). Documented in `doc.go`.
- **Testing:** blackbox `_test` packages; assert-closure tables (`table-test`); `t.Context()`; `dbtest` uses
  `testify` + `goleak`; SQLite conformance needs **no Docker**. Coverage of the `sqlite` module comes from
  **two profiles that must be merged** (audit R2-1): `dsn.go`'s option branches are covered by the in-module
  `dsn_test.go` (`go test -cover` in the sqlite module), while `dialect.go`/`quote.go`/`ddl.go` are covered by
  the `dbtest` conformance run with `-coverpkg` targeting the sqlite module (the increment-A R2-2
  cross-module methodology). Neither profile alone reflects the module; the ≥85% gate is on their union.
- **Commit trailers on every `feat`/`fix`/`refactor`:** `Spec: 002`, `Plan: 007`, `ADR: 0012`.

---

### Task 1: `sqlite` module scaffold + driver-free `DSN` builder

**Files:**
- Create: `adapter/database/sql/sqlite/go.mod`
- Create: `adapter/database/sql/sqlite/dsn.go`
- Create: `adapter/database/sql/sqlite/doc.go`
- Create: `adapter/database/sql/sqlite/dsn_test.go` (stdlib `testing` only — blackbox `sqlite_test`)
- Modify: `go.work` (add `./adapter/database/sql/sqlite` to the `use` block)

**Interfaces:**
- Produces: `sqlite.DSN(path string, opts ...sqlite.DSNOption) string`; `sqlite.DSNOption`;
  `sqlite.WithJournalMode(mode string) DSNOption`; `sqlite.WithBusyTimeout(d time.Duration) DSNOption`;
  `sqlite.WithSharedMemory() DSNOption`. (Consumed by callers and by Task 3's `RunTestSQLite` conceptually,
  though `RunTestSQLite` builds its own DSN — see Task 3.)

**DSN contract (ADR 0012 D6) — exact outputs the tests assert:**
- `path` is a **filesystem path** (not a URI); `DSN` prepends `file:`. Default journal mode `WAL`, default
  busy timeout `5s` (emitted as milliseconds: `busy_timeout(5000)`). Emits `modernc.org/sqlite` `_pragma=…`
  syntax.
- `WithJournalMode("")` OR `WithBusyTimeout(0)` OMITS that pragma. `WithSharedMemory()` uses base
  `file::memory:?cache=shared` (path ignored) and OMITS journal_mode (WAL is meaningless for an in-memory DB);
  busy_timeout still applies unless 0. Pragmas are joined with `?` after a file path, `&` after the
  shared-memory base (which already has `?cache=shared`).

- [ ] **Step 1: Create the module go.mod** (minimal — no engine require yet; audit F3)

Task 1's files (`dsn.go`, `doc.go`, `dsn_test.go`) import ONLY the stdlib and the module itself — they do NOT
import the engine. So the engine `require`/`replace` must NOT appear yet: `go mod tidy` would prune an unused
`require github.com/kartaladev/msgin`, breaking any stability check. The engine `require` + dev `replace`
arrive in **Task 3**, where `dialect.go` first imports `msginsql`.

`adapter/database/sql/sqlite/go.mod`:
```
module github.com/kartaladev/msgin/adapter/database/sql/sqlite

go 1.25.0
```

- [ ] **Step 2: Add the module to `go.work`**

Edit `go.work` — add `./adapter/database/sql/sqlite` to the `use (...)` block (keep the others):
```
use (
	./
	./adapter/database/sql/harness
	./adapter/database/sql/postgres
	./adapter/database/sql/mysql
	./adapter/database/sql/sqlite
	./adapter/database/sql/dbtest
)
```

- [ ] **Step 3: Write the failing DSN test**

`adapter/database/sql/sqlite/dsn_test.go` (blackbox, stdlib `testing`, assert-closure table):
```go
package sqlite_test

import (
	"testing"
	"time"

	"github.com/kartaladev/msgin/adapter/database/sql/sqlite"
)

func TestDSN(t *testing.T) {
	cases := []struct {
		name   string
		got    func() string
		assert func(t *testing.T, dsn string)
	}{
		{
			name: "default WAL + 5s busy_timeout",
			got:  func() string { return sqlite.DSN("/var/lib/app/msgin.db") },
			assert: func(t *testing.T, dsn string) {
				const want = "file:/var/lib/app/msgin.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
				if dsn != want {
					t.Fatalf("DSN = %q, want %q", dsn, want)
				}
			},
		},
		{
			name: "custom busy timeout in ms",
			got:  func() string { return sqlite.DSN("/x/y.db", sqlite.WithBusyTimeout(2*time.Second)) },
			assert: func(t *testing.T, dsn string) {
				const want = "file:/x/y.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(2000)"
				if dsn != want {
					t.Fatalf("DSN = %q, want %q", dsn, want)
				}
			},
		},
		{
			name: "custom journal mode",
			got:  func() string { return sqlite.DSN("/x/y.db", sqlite.WithJournalMode("DELETE")) },
			assert: func(t *testing.T, dsn string) {
				const want = "file:/x/y.db?_pragma=journal_mode(DELETE)&_pragma=busy_timeout(5000)"
				if dsn != want {
					t.Fatalf("DSN = %q, want %q", dsn, want)
				}
			},
		},
		{
			name: "busy timeout 0 omits the pragma",
			got:  func() string { return sqlite.DSN("/x/y.db", sqlite.WithBusyTimeout(0)) },
			assert: func(t *testing.T, dsn string) {
				const want = "file:/x/y.db?_pragma=journal_mode(WAL)"
				if dsn != want {
					t.Fatalf("DSN = %q, want %q", dsn, want)
				}
			},
		},
		{
			name: "empty journal mode omits the pragma",
			got:  func() string { return sqlite.DSN("/x/y.db", sqlite.WithJournalMode("")) },
			assert: func(t *testing.T, dsn string) {
				const want = "file:/x/y.db?_pragma=busy_timeout(5000)"
				if dsn != want {
					t.Fatalf("DSN = %q, want %q", dsn, want)
				}
			},
		},
		{
			name: "shared memory ignores path and omits WAL",
			got:  func() string { return sqlite.DSN("ignored", sqlite.WithSharedMemory()) },
			assert: func(t *testing.T, dsn string) {
				const want = "file::memory:?cache=shared&_pragma=busy_timeout(5000)"
				if dsn != want {
					t.Fatalf("DSN = %q, want %q", dsn, want)
				}
			},
		},
		{
			name: "shared memory with busy timeout 0 is bare",
			got:  func() string { return sqlite.DSN("", sqlite.WithSharedMemory(), sqlite.WithBusyTimeout(0)) },
			assert: func(t *testing.T, dsn string) {
				const want = "file::memory:?cache=shared"
				if dsn != want {
					t.Fatalf("DSN = %q, want %q", dsn, want)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t, tc.got())
		})
	}
}
```

- [ ] **Step 4: Run the test — expect FAIL (undefined: sqlite.DSN)**

Run: `cd adapter/database/sql/sqlite && GOTOOLCHAIN=go1.25.12 GOWORK=off go test ./... 2>&1 | head`
Expected: build error `undefined: sqlite.DSN` (and the `With*` / `DSNOption` symbols).

- [ ] **Step 5: Implement `dsn.go`**

`adapter/database/sql/sqlite/dsn.go`:
```go
package sqlite

import (
	"fmt"
	"strings"
	"time"
)

const (
	defaultJournalMode = "WAL"
	defaultBusyTimeout = 5 * time.Second
)

// dsnConfig holds resolved DSN options.
type dsnConfig struct {
	journalMode  string
	busyTimeout  time.Duration
	sharedMemory bool
}

// DSNOption customizes the DSN produced by DSN.
type DSNOption func(*dsnConfig)

// WithJournalMode overrides the journal_mode pragma (default "WAL"). An empty
// mode omits the pragma entirely. Ignored under WithSharedMemory (WAL is
// meaningless for an in-memory database).
func WithJournalMode(mode string) DSNOption { return func(c *dsnConfig) { c.journalMode = mode } }

// WithBusyTimeout overrides the busy_timeout pragma (default 5s), emitted in
// milliseconds. A zero (or negative) duration omits the pragma — do this only
// if you accept SQLITE_BUSY errors under write contention (see package doc).
func WithBusyTimeout(d time.Duration) DSNOption { return func(c *dsnConfig) { c.busyTimeout = d } }

// WithSharedMemory targets an in-memory database shared across the pool
// (file::memory:?cache=shared) instead of a file; the path argument to DSN is
// ignored. Intended for ephemeral/testing use — the database vanishes when the
// last connection closes.
func WithSharedMemory() DSNOption { return func(c *dsnConfig) { c.sharedMemory = true } }

// DSN builds an opinionated, overridable modernc.org/sqlite connection string
// for path (a filesystem path; DSN prepends the file: URI scheme). The default
// enables WAL journal mode and a 5s busy_timeout so concurrent consumers
// serialize on the single writer instead of failing with SQLITE_BUSY (see the
// package doc for why both are required). DSN imports no driver — it only
// assembles a string; the caller opens the *sql.DB with their chosen driver.
// For DSNs more exotic than these options cover, construct the string yourself.
func DSN(path string, opts ...DSNOption) string {
	cfg := dsnConfig{journalMode: defaultJournalMode, busyTimeout: defaultBusyTimeout}
	for _, o := range opts {
		o(&cfg)
	}

	base := "file:" + path
	if cfg.sharedMemory {
		base = "file::memory:?cache=shared"
	}

	var pragmas []string
	if !cfg.sharedMemory && cfg.journalMode != "" {
		pragmas = append(pragmas, fmt.Sprintf("_pragma=journal_mode(%s)", cfg.journalMode))
	}
	if cfg.busyTimeout > 0 {
		pragmas = append(pragmas, fmt.Sprintf("_pragma=busy_timeout(%d)", cfg.busyTimeout.Milliseconds()))
	}
	if len(pragmas) == 0 {
		return base
	}

	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + strings.Join(pragmas, "&")
}
```

- [ ] **Step 6: Write `doc.go`**

`adapter/database/sql/sqlite/doc.go`:
```go
// Package sqlite is the built-in SQLite dialect for the msgin sql adapter
// engine (github.com/kartaladev/msgin/adapter/database/sql). It implements the
// engine's lease/claim source SPI (LeaseDialect) and the dedup-inbox SPI
// (InboxDialect); pass sqlite.LeaseDialect() to NewPollingSource/
// NewOutboundAdapter and sqlite.InboxDialect() to NewInboxDeduper.
//
// # Driver
//
// This module imports no driver — the caller opens the *sql.DB. The recommended
// driver is modernc.org/sqlite (pure-Go, cgo-free), which bundles SQLite >=3.45
// and satisfies this dialect's floor: RETURNING (>=3.35), ON CONFLICT (>=3.24),
// and unixepoch('now','subsec') (>=3.42). The DSN builder below emits
// modernc-flavored pragmas; other drivers need their own DSN.
//
// # Delivery guarantee
//
// At-least-once, identical to the postgres/mysql dialects. SQLite serializes
// writers (a single database-wide write lock), so there is no lock/FOR UPDATE
// strategy: sqlite.LeaseDialect() does NOT implement LockDialect, and
// WithStrategy(StrategyLockForUpdate) with it returns ErrLockStrategyUnsupported.
// A worker pool still runs, but write operations serialize; throughput is
// bounded by the single writer.
//
// # Connection configuration (important)
//
// Because writers serialize, concurrent consumers (multiple pollers, or a worker
// pool acking/nacking in parallel) MUST use WAL journal mode and a non-zero
// busy_timeout, or they fail with SQLITE_BUSY under contention. Use DSN to build
// a connection string with those defaults, e.g.:
//
//	dsn := sqlite.DSN("/var/lib/app/msgin.db")
//	// file:/var/lib/app/msgin.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)
//	db, err := sql.Open("sqlite", dsn) // caller imports modernc.org/sqlite
//	src, err := msginsql.NewPollingSource(db, "queue", sqlite.LeaseDialect())
//
// # Schema
//
// Timestamp columns are INTEGER epoch microseconds written from SQLite's own
// clock. Provision the schema with the reference DDL (see DDL / InboxDDL) or
// EnsureSchema; msgin never runs DDL implicitly on the production path.
package sqlite
```

- [ ] **Step 7: Run the DSN test — expect PASS; verify the module has zero external deps**

Run:
```
cd adapter/database/sql/sqlite
GOTOOLCHAIN=go1.25.12 GOWORK=off go test ./... -race
GOTOOLCHAIN=go1.25.12 GOWORK=off go mod tidy && git diff --exit-code go.mod   # stays module+go only (no requires)
GOTOOLCHAIN=go1.25.12 GOWORK=off go vet ./... && gofmt -l .
CGO_ENABLED=0 GOTOOLCHAIN=go1.25.12 GOWORK=off go build ./...
go list -deps . | grep -iE 'modernc|mattn|testify|testcontainers|kartaladev' || echo "OK: stdlib-only, no external deps yet"
```
Expected: tests PASS; `go.mod` unchanged (still just `module` + `go 1.25.0` — Task 1 has no external imports);
vet/gofmt clean; CGO build succeeds; the `go list -deps` grep prints `OK` (the engine dep arrives in Task 3).

- [ ] **Step 8: Commit**
```bash
git add adapter/database/sql/sqlite/ go.work
git commit -m "$(cat <<'EOF'
feat(sqlite): scaffold sqlite dialect module + driver-free DSN builder

Adds the adapter/database/sql/sqlite module (engine-only) with the
opinionated, overridable sqlite.DSN builder (WAL + busy_timeout defaults,
WithSharedMemory/WithJournalMode/WithBusyTimeout) and package doc. No driver
import; the DSN is pure string assembly. go.work updated.

Spec: 002
Plan: 007
ADR: 0012
EOF
)"
```

---

### Task 2: two behavior-preserving `harness` changes for a lease-only single-writer dialect (ADR 0012 D8)

**Files:**
- Modify: `adapter/database/sql/harness/dialect.go` (the `InvalidIdentifierRejectedPerSPIMethod` subtest, ~L109–L165)
- Modify: `adapter/database/sql/harness/testkit.go` (add the `SingleWriter bool` field to `TestKit`)
- Modify: `adapter/database/sql/harness/outbox.go` (skip `CommitGatesVisibility` when `kit.SingleWriter`, ~L114)

**Interfaces:**
- Consumes: `msginsql.LockDialect` (engine).
- Produces: one new exported `TestKit` field — `SingleWriter bool` (zero value `false`). No other API change.
  Behavior: (1) `RunDialect` runs the `ClaimLock` invalid-identifier case ONLY when `kit.Lease` implements
  `msginsql.LockDialect`; (2) `RunOutbox` skips ONLY its `CommitGatesVisibility` subtest when
  `kit.SingleWriter` is true. Both gates default off → Postgres/MySQL are unaffected.

**Why (both empirically grounded — audit round 1):**
1. `RunDialect`'s per-SPI-method invalid-identifier subtest does
   `lockDialect, ok := kit.Lease.(msginsql.LockDialect); require.True(t, ok, …)` — hard-requiring `LockDialect`,
   which a lease-only dialect (SQLite) lacks. Make just the `ClaimLock` case conditional.
2. `RunOutbox/CommitGatesVisibility` opens a caller `*sql.Tx`, does the outbox `Insert` on it (holding
   SQLite's single writer lock, **uncommitted**), then runs a concurrent `Source.Poll` whose claim `UPDATE`
   hits the pool. On PG/MySQL the uncommitted row is MVCC/`SKIP LOCKED`-invisible → poll returns empty. On
   single-writer SQLite the pool `UPDATE` blocks the full `busy_timeout` then fails `SQLITE_BUSY` (the spike
   measured a 5.05 s stall → `database is locked`). It asserts an MVCC property SQLite lacks — not an F8
   correctness-only case — so gate it on `SingleWriter`. Outbox atomicity stays covered by `RunOutbox/Atomicity`.

- [ ] **Step 1: Read the current subtest**

Run: `sed -n '109,175p' adapter/database/sql/harness/dialect.go` — locate the
`lockDialect, ok := kit.Lease.(msginsql.LockDialect)` + `require.True(...)` lines and the `ClaimLock` case in
the `cases` slice.

- [ ] **Step 2: Make the ClaimLock case conditional**

Replace the hard `require.True` gate so the `ClaimLock` case is appended only when the type-assertion
succeeds. Concretely: remove the `require.True(t, ok, …)` line; keep `lockDialect, ok := …`; build the `cases`
slice with the lease + inbox cases unconditionally, and append the `ClaimLock` case inside `if ok { … }`.
Example shape (adapt to the existing `cases` construction):
```go
lockDialect, ok := kit.Lease.(msginsql.LockDialect)

// ... build `cases` with the LeaseDialect + InboxDialect method cases ...

// ClaimLock is the only LockDialect method with a bare-table-string signature;
// cover its invalid-identifier guard only when the dialect implements the
// segregated lock SPI. A lease-only dialect (e.g. sqlite) has no ClaimLock, so
// this case is skipped rather than hard-required (ADR 0012 D8). AckLock/NackLock
// take a *LockedRow, not a table string, so they are not covered here (as before).
if ok {
	cases = append(cases, validationCase{
		name: "ClaimLock",
		call: func(t *testing.T, ctx context.Context) error {
			_, err := lockDialect.ClaimLock(ctx, db, bad, dialectTestLockedBy)
			return err
		},
		assert: assertInvalid,
	})
}
```
Keep everything else (the `bad` constant, `assertInvalid`, the loop running `cases`) unchanged. Ensure
`lockDialect` is now referenced (so no "declared and not used" error) — it is, inside the `if ok` block.

- [ ] **Step 3: Add the `SingleWriter` field to `TestKit`**

In `adapter/database/sql/harness/testkit.go`, add to the `TestKit` struct (near `MySQLFamily`):
```go
	// SingleWriter reports whether the engine serializes all writers on a
	// single database-wide write lock (SQLite). It gates ONLY
	// RunOutbox/CommitGatesVisibility, which holds an uncommitted write
	// transaction open while a concurrent Source.Poll issues a claim UPDATE on
	// the pool: on an MVCC engine (Postgres/MySQL) the pool claim returns empty
	// cleanly, but on a single-writer engine it blocks on the held writer lock
	// for the full busy_timeout and then fails SQLITE_BUSY. The zero value
	// (false) preserves the existing Postgres/MySQL behavior (audit F1, ADR 0012 D8).
	SingleWriter bool
```

- [ ] **Step 4: Gate `CommitGatesVisibility` on `SingleWriter`**

In `adapter/database/sql/harness/outbox.go`, at the top of the `t.Run("CommitGatesVisibility", func(t *testing.T) {`
body (~L114), add:
```go
		if kit.SingleWriter {
			t.Skip("CommitGatesVisibility requires MVCC non-blocking reads during an open write tx; " +
				"a single-writer engine (sqlite) blocks->SQLITE_BUSY here — outbox atomicity is covered by Atomicity (ADR 0012 D8)")
		}
```

- [ ] **Step 5: Build + vet the harness module**

Run:
```
cd adapter/database/sql/harness
GOTOOLCHAIN=go1.25.12 GOWORK=off go build ./... && GOTOOLCHAIN=go1.25.12 GOWORK=off go vet ./... && gofmt -l .
```
Expected: clean (the harness module has no in-module tests; it compiles).

- [ ] **Step 6: Prove behavior-preserving via an existing engine (Docker) — Postgres Dialect + Outbox still green**

Run (Docker required):
```
cd adapter/database/sql/dbtest
GOTOOLCHAIN=go1.25.12 GOWORK=off go test -race -run 'TestPostgresConformance/(Dialect|Outbox)' ./...
```
Expected: PASS — Postgres's `Dialect/InvalidIdentifierRejectedPerSPIMethod/ClaimLock` STILL runs (it
implements `LockDialect`) AND `Outbox/CommitGatesVisibility` STILL runs (Postgres has `SingleWriter=false`),
confirming neither gate dropped existing coverage.

- [ ] **Step 7: Commit**
```bash
git add adapter/database/sql/harness/dialect.go adapter/database/sql/harness/testkit.go adapter/database/sql/harness/outbox.go
git commit -m "$(cat <<'EOF'
fix(harness): support lease-only single-writer dialects (sqlite)

Two behavior-preserving, gated changes so a lease-only single-writer dialect
can run the conformance suite (both default off -> postgres/mysql unaffected,
still fully covered by the dbtest run):

1. RunDialect's per-SPI-method invalid-identifier subtest hard-required
   kit.Lease to implement LockDialect; gate only the ClaimLock case on the
   type-assertion.
2. Add TestKit.SingleWriter and skip only RunOutbox/CommitGatesVisibility when
   set: that subtest needs MVCC non-blocking reads during an open write tx,
   which single-writer sqlite lacks (pool claim blocks -> SQLITE_BUSY,
   empirically confirmed). Outbox atomicity stays covered by Atomicity.

Spec: 002
Plan: 007
ADR: 0012
EOF
)"
```

---

### Task 3: SQLite dialect implementation + `dbtest` conformance run

**Files:**
- Create: `adapter/database/sql/sqlite/quote.go`
- Create: `adapter/database/sql/sqlite/ddl.go`
- Create: `adapter/database/sql/sqlite/dialect.go`
- Modify: `adapter/database/sql/dbtest/go.mod` (add `modernc.org/sqlite` + the `sqlite` module + `replace`)
- Modify: `adapter/database/sql/dbtest/testutils_test.go` (register the `sqlite` driver + `RunTestSQLite`)
- Create: `adapter/database/sql/dbtest/conformance_sqlite_test.go` (`sqliteKit`, `TestSQLiteConformance`,
  `TestSQLiteLockStrategyUnsupported`)

**Interfaces:**
- Consumes: engine SPIs `msginsql.{LeaseDialect,InboxDialect,Querier,ClaimedRow,ValidateIdent}`, `harness.{TestKit,
  RunSource,RunOutbound,RunOutbox,RunInbox,RunDialect}`, `msginsql.{NewPollingSource,WithStrategy,
  StrategyLockForUpdate,ErrLockStrategyUnsupported}`; the harness change from Task 2.
- Produces: `sqlite.LeaseDialect() msginsql.LeaseDialect`; `sqlite.InboxDialect() msginsql.InboxDialect`;
  `sqlite.DDL(table string) (string, error)`; `sqlite.InboxDDL(table string) (string, error)`. `dbtest`:
  `RunTestSQLite(t *testing.T, opts ...TestOption) *sql.DB`; a `sqlite`-specific option that selects shared
  memory.

**The DB-clock expression (used verbatim in every timestamp write/compare):**
`CAST(unixepoch('now','subsec')*1000000 AS INTEGER)`.

- [ ] **Step 1: Write `quote.go`**

`adapter/database/sql/sqlite/quote.go`:
```go
package sqlite

import (
	"strings"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
)

// sqliteQuote double-quotes a SQLite identifier. The name must already be
// validated (ValidateIdent admits no double-quote), so wrapping is safe;
// doubling any embedded `"` is defense-in-depth in case this is ever reached
// without prior validation.
func sqliteQuote(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// sqliteQuoteTable validates then quotes a table identifier for interpolation.
func sqliteQuoteTable(table string) (string, error) {
	if err := msginsql.ValidateIdent(table); err != nil {
		return "", err
	}
	return sqliteQuote(table), nil
}
```

- [ ] **Step 2: Write `ddl.go`**

`adapter/database/sql/sqlite/ddl.go`:
```go
package sqlite

import (
	"fmt"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
)

// nowMicros is the DB-clock expression: current UTC time as epoch microseconds
// (INTEGER). SQLite's 'subsec' modifier yields sub-second (millisecond)
// resolution; *1000000 expresses it in microseconds so all interval arithmetic
// matches the .Microseconds() convention the postgres/mysql dialects use
// (ADR 0012 D4). Requires SQLite >=3.42.
const nowMicros = `CAST(unixepoch('now','subsec')*1000000 AS INTEGER)`

// DDL returns the reference CREATE TABLE (+ claim index) for the lease/claim
// schema on SQLite, for callers to fold into their migration tool. It validates
// table before building; msgin never runs it on the production path (ADR 0010 D2).
func DDL(table string) (string, error) {
	if err := msginsql.ValidateIdent(table); err != nil {
		return "", err
	}
	return sqliteDialect{}.ddl(table), nil
}

// InboxDDL returns the reference CREATE TABLE (+ retention index) for the
// dedup-inbox schema on SQLite (ADR 0010 D10). It validates table as the sole
// entry point (no string-returning DDL method on the SPI).
func InboxDDL(table string) (string, error) {
	if err := msginsql.ValidateIdent(table); err != nil {
		return "", err
	}
	return sqliteInboxDDL(table), nil
}

// sqliteCreateTable builds the idempotent CREATE TABLE for the lease/claim
// schema. qt must be an already-quoted identifier. Timestamps are INTEGER epoch
// microseconds defaulted from the DB clock; id is a rowid alias (auto-increments).
func sqliteCreateTable(qt string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  id             INTEGER PRIMARY KEY,
  msg_id         TEXT    NOT NULL,
  headers        TEXT    NOT NULL,
  payload        BLOB    NOT NULL,
  locked_by      TEXT,
  locked_at      INTEGER,
  visible_after  INTEGER NOT NULL DEFAULT (%s),
  delivery_count INTEGER NOT NULL DEFAULT 0,
  lease_epoch    INTEGER NOT NULL DEFAULT 0,
  created_at     INTEGER NOT NULL DEFAULT (%s)
)`, qt, nowMicros, nowMicros)
}

// sqliteCreateIndex builds the partial claim index (SQLite supports partial
// indexes >=3.8.0). qt is the already-quoted table; qidx the quoted index name.
func sqliteCreateIndex(qt, qidx string) string {
	return fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (visible_after) WHERE locked_at IS NULL`, qidx, qt)
}

// sqliteCreateInboxTable builds the idempotent CREATE TABLE for the dedup inbox.
// msg_id is the TEXT PRIMARY KEY (the dedup key → a unique autoindex);
// processed_at (DB clock, µs) drives Purge retention.
func sqliteCreateInboxTable(qt string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  msg_id       TEXT    PRIMARY KEY,
  processed_at INTEGER NOT NULL DEFAULT (%s)
)`, qt, nowMicros)
}

// sqliteCreateInboxIndex builds the retention index on processed_at.
func sqliteCreateInboxIndex(qt, qidx string) string {
	return fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (processed_at)`, qidx, qt)
}

// ddl builds the combined reference DDL (table + index) for an already-validated
// table name. Unexported (a string return cannot revalidate); the only public
// entry point is DDL, which ValidateIdent first.
func (sqliteDialect) ddl(table string) string {
	qt := sqliteQuote(table)
	qidx := sqliteQuote(table + "_claim_idx")
	return sqliteCreateTable(qt) + ";\n" + sqliteCreateIndex(qt, qidx) + ";"
}

// sqliteInboxDDL builds the combined reference inbox DDL for an already-validated
// table name. Unexported; the only public entry point is InboxDDL.
func sqliteInboxDDL(table string) string {
	qt := sqliteQuote(table)
	qidx := sqliteQuote(table + "_processed_idx")
	return sqliteCreateInboxTable(qt) + ";\n" + sqliteCreateInboxIndex(qt, qidx) + ";"
}
```

- [ ] **Step 3: Write the failing conformance wiring in `dbtest`**

First add deps to `adapter/database/sql/dbtest/go.mod`: add to the first `require` block
`github.com/kartaladev/msgin/adapter/database/sql/sqlite v0.0.0` and `modernc.org/sqlite v1.54.0` (the version
verified by the design spike — Step 7 tidies to the resolved version), and to the `replace` block
`github.com/kartaladev/msgin/adapter/database/sql/sqlite => ../sqlite`. Then **populate go.sum first**
(including transitive checksums) so Step 4's failure is a clean compile error, not a `missing go.sum entry`
error (audit F5/R2-2 — use the no-arg form, not a single-module download):
```
cd adapter/database/sql/dbtest && GOTOOLCHAIN=go1.25.12 GOWORK=off go mod download
```

`adapter/database/sql/dbtest/conformance_sqlite_test.go`:
```go
package dbtest_test

import (
	"database/sql"
	"testing"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/kartaladev/msgin/adapter/database/sql/harness"
	"github.com/kartaladev/msgin/adapter/database/sql/sqlite"
	"github.com/stretchr/testify/require"
)

// sqliteKit builds the harness TestKit for the built-in SQLite dialect. SQLite
// uses double-quoted identifiers, "?" placeholders, INTEGER epoch-microsecond
// timestamps (the DB-clock NowExpr), and a plain TEXT headers column (identity
// HeadersTextExpr — no jsonb cast). MySQLFamily=false: SQLite's ON CONFLICT ...
// RETURNING inbox is exact (no INSERT-IGNORE demotion), so the harness skips the
// MySQL-specific assertion. There is NO OpenDB (RunLock is not run — sqlite has
// no LockDialect).
func sqliteKit() harness.TestKit {
	return harness.TestKit{
		Name:            "sqlite",
		MySQLFamily:     false,
		SingleWriter:    true, // skips RunOutbox/CommitGatesVisibility (ADR 0012 D8, audit F1)
		Lease:           sqlite.LeaseDialect(),
		Inbox:           sqlite.InboxDialect(),
		Quote:           func(ident string) string { return `"` + ident + `"` },
		Placeholder:     func(int) string { return "?" },
		NowExpr:         func() string { return `CAST(unixepoch('now','subsec')*1000000 AS INTEGER)` },
		HeadersTextExpr: func(col string) string { return col },
		DDL:             sqlite.DDL,
		InboxDDL:        sqlite.InboxDDL,
		// OpenDB intentionally nil: RunLock is not run for SQLite.
	}
}

// TestSQLiteConformance runs the msgin sql-adapter conformance harness against
// an embedded SQLite database (no Docker). RunLock is omitted — sqlite is
// lease-only (ADR 0012 D1). One DB backs every suite; each harness Run*
// provisions its own fresh tables.
func TestSQLiteConformance(t *testing.T) {
	db := RunTestSQLite(t)
	kit := sqliteKit()

	t.Run("Source", func(t *testing.T) { harness.RunSource(t, kit, db) })
	t.Run("Outbound", func(t *testing.T) { harness.RunOutbound(t, kit, db) })
	t.Run("Outbox", func(t *testing.T) { harness.RunOutbox(t, kit, db) })
	t.Run("Inbox", func(t *testing.T) { harness.RunInbox(t, kit, db) })
	t.Run("Dialect", func(t *testing.T) { harness.RunDialect(t, kit, db) })
}

// TestSQLiteLockStrategyUnsupported asserts the lease-only sqlite dialect does
// NOT satisfy LockDialect and that selecting the lock strategy fails at
// construction with ErrLockStrategyUnsupported (ADR 0012 D1).
func TestSQLiteLockStrategyUnsupported(t *testing.T) {
	_, isLock := sqlite.LeaseDialect().(msginsql.LockDialect)
	require.False(t, isLock, "sqlite.LeaseDialect() must NOT implement LockDialect")

	db := RunTestSQLite(t)
	_, err := msginsql.NewPollingSource(db, "msgin_msgs", sqlite.LeaseDialect(),
		msginsql.WithStrategy(msginsql.StrategyLockForUpdate))
	require.ErrorIs(t, err, msginsql.ErrLockStrategyUnsupported)
}
```

Add `RunTestSQLite` + driver registration to `adapter/database/sql/dbtest/testutils_test.go`:
```go
// add to the import block:
//   "path/filepath"
//   "github.com/kartaladev/msgin/adapter/database/sql/sqlite"  // for sqlite.DSN (dogfood the builder)
//   _ "modernc.org/sqlite"   // registers the "sqlite" database/sql driver (pure-Go, cgo-free)

// WithSharedMemory selects an in-memory shared-cache SQLite DB instead of the
// default WAL temp-file, for RunTestSQLite.
func WithSharedMemory() TestOption {
	return func(c *testConfig) { c.sqliteSharedMemory = true }
}

// RunTestSQLite opens an embedded SQLite *sql.DB (modernc.org/sqlite, pure-Go —
// no Docker) for the conformance run. It builds its DSN via sqlite.DSN (audit
// F6 — dogfooding the production builder and proving modernc accepts its
// output). The default is a WAL temp-file (removed with the test's TempDir by
// the framework) with a busy_timeout so the harness's concurrency cases
// serialize rather than hitting SQLITE_BUSY; WithSharedMemory selects
// file::memory:?cache=shared instead. The connection is closed via t.Cleanup.
func RunTestSQLite(t *testing.T, opts ...TestOption) *sql.DB {
	t.Helper()

	cfg := &testConfig{}
	for _, o := range opts {
		o(cfg)
	}

	dsn := sqlite.DSN(filepath.Join(t.TempDir(), "msgin.db"))
	if cfg.sqliteSharedMemory {
		dsn = sqlite.DSN("", sqlite.WithSharedMemory())
	}

	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err, "open sqlite database")
	t.Cleanup(func() { _ = db.Close() })

	pingCtx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	require.NoError(t, db.PingContext(pingCtx), "ping sqlite database")

	return db
}
```
Also add the `sqliteSharedMemory bool` field to the existing `testConfig` struct.

- [ ] **Step 4: Build dbtest — expect FAIL (undefined: sqlite.LeaseDialect / sqlite.InboxDialect)**

Run: `cd adapter/database/sql/dbtest && GOTOOLCHAIN=go1.25.12 GOWORK=off go vet ./... 2>&1 | head`
Expected: a clean COMPILE error — `sqlite.LeaseDialect`/`sqlite.InboxDialect` undefined (the `sqlite` package
is still just `dsn.go`+`doc.go` from Task 1; `dialect.go` arrives in Step 5). NOT a go.sum/checksum error
(modernc was downloaded in Step 3) and NOT a resolution error (the `replace => ../sqlite` resolves the local
module under GOWORK=off).

- [ ] **Step 5: Implement `dialect.go` (and add the engine require to `sqlite/go.mod`)**

`dialect.go`, `quote.go`, and `ddl.go` import `msginsql`, so the engine `require` + dev `replace` now arrive
in `adapter/database/sql/sqlite/go.mod` (mirroring `postgres`/`mysql`):
```
require github.com/kartaladev/msgin v0.0.0

require github.com/jonboulle/clockwork v0.5.0 // indirect

// Dev-time only: no published tag of the root module carries this engine yet
// (ADR 0011/0012). Swapped for a pinned require once the root is tagged (spec 002 §8).
replace github.com/kartaladev/msgin => ../../../..
```
(Step 7 runs `go mod tidy` in the sqlite module to settle the exact indirect set.)

`adapter/database/sql/sqlite/dialect.go`:
```go
package sqlite

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"time"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
)

// sqliteDialect is the built-in LeaseDialect + InboxDialect for SQLite. It is
// stateless; a single value is shared by all callers. It implements the
// lease/claim strategy only: SQLite serializes writers and has no FOR UPDATE, so
// it deliberately does NOT implement msginsql.LockDialect (ADR 0012 D1) —
// WithStrategy(StrategyLockForUpdate) with it returns ErrLockStrategyUnsupported.
// All persisted timestamps use the DB clock (unixepoch), never the app clock.
type sqliteDialect struct{}

// Compile-time assertions. Note there is intentionally NO
// `_ msginsql.LockDialect = sqliteDialect{}` — sqlite is lease-only.
var (
	_ msginsql.LeaseDialect = sqliteDialect{}
	_ msginsql.InboxDialect = sqliteDialect{}
)

// LeaseDialect returns the built-in SQLite LeaseDialect (lease/claim strategy).
// Pass it as the required dialect argument to NewPollingSource/
// NewOutboundAdapter. It does NOT implement LockDialect (sqlite is lease-only).
func LeaseDialect() msginsql.LeaseDialect { return sqliteDialect{} }

// InboxDialect returns the built-in SQLite InboxDialect — the same stateless
// value as LeaseDialect(), narrowed to the dedup-inbox SPI. Pass it to
// NewInboxDeduper.
func InboxDialect() msginsql.InboxDialect { return sqliteDialect{} }

// Claim leases up to limit claimable rows for lockedBy, treating any lease older
// than leaseTTL as expired. It is a ONE-SHOT atomic UPDATE ... RETURNING: SQLite
// serializes writers (the statement holds the DB write lock for its duration),
// so no SKIP LOCKED or explicit two-step transaction is needed — mirroring
// Postgres's one-shot claim. RETURNING yields the post-update delivery_count/
// lease_epoch directly (ADR 0012 D2).
func (sqliteDialect) Claim(ctx context.Context, q msginsql.Querier, table string, limit int, lockedBy string, leaseTTL time.Duration) ([]msginsql.ClaimedRow, error) {
	qt, err := sqliteQuoteTable(table)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`UPDATE %[1]s SET
  locked_by = ?, locked_at = %[2]s,
  lease_epoch = lease_epoch + 1, delivery_count = delivery_count + 1
WHERE id IN (
  SELECT id FROM %[1]s
  WHERE visible_after <= %[2]s
    AND (locked_at IS NULL OR locked_at <= %[2]s - ?)
  ORDER BY visible_after
  LIMIT ?
)
RETURNING id, msg_id, headers, payload, delivery_count, lease_epoch`, qt, nowMicros)

	rows, err := q.QueryContext(ctx, query, lockedBy, leaseTTL.Microseconds(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []msginsql.ClaimedRow
	for rows.Next() {
		var r msginsql.ClaimedRow
		if err := rows.Scan(&r.ID, &r.MsgID, &r.Headers, &r.Payload, &r.DeliveryCount, &r.LeaseEpoch); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Ack deletes a fenced row (id + locked_by + lease_epoch); applied=false on a
// fence miss (the lease expired and another worker re-claimed it).
func (sqliteDialect) Ack(ctx context.Context, q msginsql.Querier, table string, id int64, lockedBy string, epoch int64) (bool, error) {
	qt, err := sqliteQuoteTable(table)
	if err != nil {
		return false, err
	}
	res, err := q.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE id = ? AND locked_by = ? AND lease_epoch = ?`, qt),
		id, lockedBy, epoch)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Nack returns a fenced row to the queue: clear the lock and push visible_after
// out by delay (µs). applied=false on a fence miss.
func (sqliteDialect) Nack(ctx context.Context, q msginsql.Querier, table string, id int64, lockedBy string, epoch int64, delay time.Duration) (bool, error) {
	qt, err := sqliteQuoteTable(table)
	if err != nil {
		return false, err
	}
	res, err := q.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s SET locked_by = NULL, locked_at = NULL,
  visible_after = %s + ?
WHERE id = ? AND locked_by = ? AND lease_epoch = ?`, qt, nowMicros),
		delay.Microseconds(), id, lockedBy, epoch)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Insert writes a new message; visible_after = now + delay (µs). headers is the
// framed JSON bound as TEXT; payload as a BLOB.
func (sqliteDialect) Insert(ctx context.Context, q msginsql.Querier, table, msgID string, headers, payload []byte, delay time.Duration) error {
	qt, err := sqliteQuoteTable(table)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (msg_id, headers, payload, visible_after)
VALUES (?, ?, ?, %s + ?)`, qt, nowMicros),
		msgID, string(headers), payload, delay.Microseconds())
	return err
}

// EnsureSchema idempotently creates the table then its partial claim index
// (separate IF NOT EXISTS statements).
func (sqliteDialect) EnsureSchema(ctx context.Context, q msginsql.Querier, table string) error {
	qt, err := sqliteQuoteTable(table)
	if err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, sqliteCreateTable(qt)); err != nil {
		return err
	}
	qidx := sqliteQuote(table + "_claim_idx")
	if _, err := q.ExecContext(ctx, sqliteCreateIndex(qt, qidx)); err != nil {
		return err
	}
	return nil
}

// SchemaExists probes sqlite_master (SQLite has no information_schema). table is
// a bound parameter, but validated anyway so the exported SPI never runs on an
// unvalidated identifier.
func (sqliteDialect) SchemaExists(ctx context.Context, q msginsql.Querier, table string) (bool, error) {
	if err := msginsql.ValidateIdent(table); err != nil {
		return false, err
	}
	var one int
	err := q.QueryRowContext(ctx,
		`SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&one)
	if errors.Is(err, stdsql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// InsertInboxIfAbsent records msgID via INSERT ... ON CONFLICT DO NOTHING
// RETURNING — EXACT like Postgres (a returned row → inserted; ErrNoRows →
// genuine duplicate). Runs on q (the caller's business tx). SQLite has no
// INSERT-IGNORE demotion problem, so there is no ErrInboxInsertFailed path.
func (sqliteDialect) InsertInboxIfAbsent(ctx context.Context, q msginsql.Querier, table, msgID string) (bool, error) {
	qt, err := sqliteQuoteTable(table)
	if err != nil {
		return false, err
	}
	var returned string
	err = q.QueryRowContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (msg_id, processed_at) VALUES (?, %s)
ON CONFLICT (msg_id) DO NOTHING
RETURNING msg_id`, qt, nowMicros),
		msgID).Scan(&returned)
	if errors.Is(err, stdsql.ErrNoRows) {
		return true, nil // conflict: already recorded (a genuine duplicate)
	}
	if err != nil {
		return false, err
	}
	return false, nil // inserted: first sighting
}

// PurgeInbox deletes dedup rows older than olderThan (DB clock, µs).
func (sqliteDialect) PurgeInbox(ctx context.Context, q msginsql.Querier, table string, olderThan time.Duration) (int64, error) {
	qt, err := sqliteQuoteTable(table)
	if err != nil {
		return 0, err
	}
	res, err := q.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE processed_at < %s - ?`, qt, nowMicros),
		olderThan.Microseconds())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// EnsureInboxSchema idempotently creates the dedup table then its processed_at
// retention index (separate IF NOT EXISTS statements).
func (sqliteDialect) EnsureInboxSchema(ctx context.Context, q msginsql.Querier, table string) error {
	qt, err := sqliteQuoteTable(table)
	if err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, sqliteCreateInboxTable(qt)); err != nil {
		return err
	}
	qidx := sqliteQuote(table + "_processed_idx")
	if _, err := q.ExecContext(ctx, sqliteCreateInboxIndex(qt, qidx)); err != nil {
		return err
	}
	return nil
}

// MsgIDUniqueIndexExists reports whether msg_id participates in a unique index
// (a TEXT PRIMARY KEY or an explicit UNIQUE both create a "unique"=1 index that
// appears in pragma_index_list). table is validated even though it is bound.
func (sqliteDialect) MsgIDUniqueIndexExists(ctx context.Context, q msginsql.Querier, table string) (bool, error) {
	if err := msginsql.ValidateIdent(table); err != nil {
		return false, err
	}
	var one int
	err := q.QueryRowContext(ctx,
		`SELECT 1
FROM pragma_index_list(?) il
JOIN pragma_index_info(il.name) ii
WHERE il."unique" = 1 AND ii.name = 'msg_id'
LIMIT 1`, table).Scan(&one)
	if errors.Is(err, stdsql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
```

> **Implementer note (bound-param pragma — F4 resolved by the design spike):** the bound `?` form
> `pragma_index_list(?)` + the nested `pragma_index_info(il.name)` join is **verified working** against
> `modernc.org/sqlite` v1.54.0 (spike checks 5a/5b/5c: bound form works, literal fallback works, and the
> no-constraint negative case correctly returns `sql.ErrNoRows`). Use the bound form as written. If a future
> driver ever rejects it, the injection-safe fallback is to interpolate the already-`ValidateIdent`-checked +
> quoted table name as a string literal.

- [ ] **Step 6: Run the SQLite conformance — expect PASS**

Run: `cd adapter/database/sql/dbtest && GOTOOLCHAIN=go1.25.12 GOWORK=off go test -race -run 'TestSQLite' ./...`
Expected: PASS — `TestSQLiteConformance` (Source/Outbound/Outbox[/CommitGatesVisibility SKIPPED]/Inbox/Dialect)
and `TestSQLiteLockStrategyUnsupported` all green, no Docker used. If a specific harness assertion fails, fix
the dialect SQL (not the harness) — the harness is the contract. **Watch two spike-flagged spots:** (F7) if a
harness scan of an INTEGER `locked_at`/`visible_after` fails, it means a non-NULL timestamp is being scanned
into a `time.Time`/`NullTime` — the spike found the only such scan reads a NULL there, so this should pass;
report it rather than working around it. (F1) `Outbox/CommitGatesVisibility` must show as SKIPPED (not run),
proving the `SingleWriter` gate fired.

- [ ] **Step 7: Tidy dbtest deps; verify sqlite prod module stays driver-free; full module suites**

Run:
```
cd adapter/database/sql/dbtest
GOTOOLCHAIN=go1.25.12 GOWORK=off go mod tidy      # pins modernc.org/sqlite + sqlite module version
GOTOOLCHAIN=go1.25.12 GOWORK=off go build ./... && GOTOOLCHAIN=go1.25.12 GOWORK=off go vet ./... && gofmt -l .
# sqlite prod module must remain engine-only:
cd ../sqlite && GOTOOLCHAIN=go1.25.12 GOWORK=off go mod tidy && git diff --exit-code go.mod
go list -deps . | grep -iE 'modernc|testify|testcontainers' || echo "OK: sqlite prod module is driver/test-dep free"
```
Expected: dbtest tidies (adds `modernc.org/sqlite`); the `sqlite` module `go.mod` is unchanged (engine +
`clockwork` only); the grep prints `OK`.

- [ ] **Step 8: Run the WHOLE dbtest suite with Docker (regression — PG/MySQL/MariaDB still green alongside SQLite)**

Run (Docker required): `cd adapter/database/sql/dbtest && GOTOOLCHAIN=go1.25.12 GOWORK=off go test ./... -race`
Expected: PASS — Postgres, MySQL, MariaDB, AND SQLite conformance all green, goleak-clean.

- [ ] **Step 9: Measure sqlite prod-code coverage — MERGE two profiles (audit R2-1)**

`dsn.go` is covered only by the in-module `dsn_test.go`; `dialect.go`/`quote.go`/`ddl.go` only by the dbtest
conformance run. Measure both and merge for the ≥85% gate:
```
# Profile A — dsn.go, from the sqlite module's own test:
cd adapter/database/sql/sqlite
GOTOOLCHAIN=go1.25.12 GOWORK=off go test -covermode=atomic -coverprofile=/tmp/sqlite-a.cover ./...

# Profile B — dialect.go/quote.go/ddl.go, from the dbtest conformance run:
cd ../dbtest
GOTOOLCHAIN=go1.25.12 GOWORK=off go test -covermode=atomic -run 'TestSQLite' \
  -coverpkg=github.com/kartaladev/msgin/adapter/database/sql/sqlite/... \
  -coverprofile=/tmp/sqlite-b.cover ./...

# Merge (concatenate, dropping profile B's duplicate mode header) and report:
{ cat /tmp/sqlite-a.cover; tail -n +2 /tmp/sqlite-b.cover; } > /tmp/sqlite-merged.cover
go tool cover -func=/tmp/sqlite-merged.cover | tail -1
```
(Both use `-covermode=atomic` so the two profiles are mergeable by concatenation.) Expected: the UNION is
≥85% of the `sqlite` module. Hot-path/typed-error branches: `dsn.go`'s default + each `With*` override branch
by `dsn_test.go`; every dialect method's `ValidateIdent` early-return by
`RunDialect/InvalidIdentifierRejectedPerSPIMethod`; Claim empty/non-empty by `RunSource`/`RunDialect`;
Ack/Nack fence-hit + fence-miss by `RunDialect`; inbox already/not-already + no-unique-constraint by `RunInbox`;
`SchemaExists` both branches by `RunDialect`. If any hot-path branch is uncovered, add the missing case to the
appropriate fixture (do NOT lower the bar).

- [ ] **Step 10: Commit**
```bash
git add adapter/database/sql/sqlite/ adapter/database/sql/dbtest/
git commit -m "$(cat <<'EOF'
feat(sqlite): implement lease/inbox dialect + dbtest conformance run

sqlite.LeaseDialect()/InboxDialect() implement the engine's lease/claim +
dedup-inbox SPIs: one-shot atomic UPDATE ... RETURNING claim (no FOR UPDATE;
SQLite serializes writers), exact ON CONFLICT ... RETURNING inbox, INTEGER
epoch-microsecond DB-clock timestamps, sqlite_master/pragma_index_list probes,
reference DDL. Lease-only: no LockDialect. Certified against the harness via an
embedded (Docker-free) run in dbtest (RunTestSQLite, WAL temp-file default +
WithSharedMemory), plus a lock-strategy-unsupported assertion. modernc.org/sqlite
lands in dbtest only; the sqlite prod module stays engine-only.

Spec: 002
Plan: 007
ADR: 0012
EOF
)"
```

---

### Task 4: CI matrix + pin the `@latest` tool versions, release workflow, RELEASE.md

**Files:**
- Modify: `.github/workflows/ci.yml` (add the `sqlite` module to the `GOWORK=off` per-module matrix + the
  aggregate workspace loop; **pin `govulncheck` and `golangci-lint` off `@latest`/`latest` — the CI problem**)
- Modify: `.github/workflows/release.yml` (accept the `adapter/database/sql/sqlite/vX.Y.Z` prefixed tag)
- Modify: `docs/RELEASE.md` (add `sqlite` to the module list + tag-order choreography)

**Interfaces:** none (CI/release/docs only).

**The CI problem (diagnosed this session).** All substantive CI checks (build/vet/gofmt/CGO/tidy/`govulncheck`/
`golangci-lint`/`-race`) pass locally on `main`; the defect is the **non-reproducible `@latest` pins** (handover
§4 follow-up #1): `go run golang.org/x/vuln/cmd/govulncheck@latest` and `golangci-lint-action` `version: latest`
let a green CI spontaneously go red when a tool release drifts (a new advisory, a new default linter, a config-
schema bump). Fix: pin both to the versions verified this session — `govulncheck` `golang.org/x/vuln@v1.6.0`,
`golangci-lint` `v2.12.2`.

- [ ] **Step 1: Add `sqlite` to the CI matrix AND pin the `@latest` tool versions**

In `.github/workflows/ci.yml`:

(a) Add to the `matrix.module` list (grouped with the other sql modules):
```yaml
          - name: sqlite
            dir: adapter/database/sql/sqlite
```
and add `adapter/database/sql/sqlite` to the aggregate-workspace `for dir in …` loop (~L119). Keep
`GOWORK=off` for the per-module jobs (audit F5). The `sqlite` module needs no Docker (only the pure
`dsn_test.go`); `dbtest` already runs the Docker step and now also exercises SQLite embedded within it — no new
service setup.

(b) Pin `govulncheck` (the `govulncheck` step, ~L85):
```yaml
        run: go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...
```

(c) Pin `golangci-lint` (the `golangci-lint` step, ~L88–90) — change `version: latest` to:
```yaml
          version: v2.12.2
```
Leave `golangci/golangci-lint-action@v9` and `.golangci.yml`'s `version: "2"` as-is (both already consistent
with golangci-lint v2).

- [ ] **Step 2: Add the sqlite prefixed tag to the release workflow**

In `.github/workflows/release.yml`, wherever module-path-prefixed tags are matched/handled (the
`adapter/database/sql/{postgres,mysql}/v*` pattern), add `adapter/database/sql/sqlite/v*`. Mirror the existing
postgres/mysql handling exactly (same job/step shape).

- [ ] **Step 3: Update `docs/RELEASE.md`**

Add `adapter/database/sql/sqlite` to the module inventory and to the tag-order choreography (root tagged
FIRST; then each dialect module swaps its dev `replace` → a pinned `require` and is tagged
`adapter/database/sql/sqlite/vX.Y.Z`). Mirror the postgres/mysql wording.

- [ ] **Step 4: Validate the workflow YAML + confirm nothing else regressed**

Run:
```
cd "$(git rev-parse --show-toplevel)"
python3 -c "import yaml,sys; [yaml.safe_load(open(f)) for f in ['.github/workflows/ci.yml','.github/workflows/release.yml']]; print('YAML OK')"
GOTOOLCHAIN=go1.25.12 go build ./...        # workspace build (root) still clean
gofmt -l .
```
Expected: `YAML OK`; workspace build clean; gofmt reports nothing.

- [ ] **Step 5: Commit**
```bash
git add .github/workflows/ci.yml .github/workflows/release.yml docs/RELEASE.md
git commit -m "$(cat <<'EOF'
build(sqlite): add sqlite to CI matrix; pin govulncheck/golangci-lint versions

Adds the adapter/database/sql/sqlite module to the GOWORK=off per-module CI
matrix and the aggregate workspace job, the adapter/database/sql/sqlite/v*
prefixed-tag handling to release.yml, and the sqlite module to RELEASE.md's
tag-order choreography. No Docker needed for the sqlite module; dbtest already
runs SQLite embedded.

Also pins the two @latest CI tools for reproducibility (handover follow-up #1):
govulncheck -> golang.org/x/vuln@v1.6.0, golangci-lint -> v2.12.2, so a green
CI cannot spontaneously break on upstream tool drift.

Spec: 002
Plan: 007
ADR: 0012
EOF
)"
```

---

## Whole-branch delivery gate (before any merge/push)

After Task 4, run the CLAUDE.md whole-branch gate over `main..HEAD` (NOT per-commit):

- [ ] `/code-review` over the branch diff; resolve/triage every finding.
- [ ] `/security-review` over the branch diff; resolve everything flagged (focus: the identifier-injection
      discipline in the new dialect, the `pragma_index_list` argument handling, the DSN builder's string
      assembly).
- [ ] Per-module gate (`GOWORK=off`, `go1.25.12`) for `sqlite` + `dbtest` + `harness` + root: `go build`,
      `go vet`, `gofmt -l`, `CGO_ENABLED=0 go build`, `go mod tidy` stable, `go test ./... -race` (dbtest with
      Docker). `govulncheck ./...` clean per module.
- [ ] Confirm the coverage gate (Task 3 Step 9) and that every hot-path/typed-error branch has a covering test.
- [ ] Confirm dependency hygiene: the `sqlite` prod module requires the engine ONLY; `modernc.org/sqlite` is
      confined to `dbtest`; the isolation probe (a throwaway consumer of `adapter/database/sql/sqlite`) inherits
      no driver/test deps in its `go.sum`.
- [ ] Update `docs/HANDOVER.md` at a green safepoint.

Then request approval to merge to `main` (fast-forward), tag per `docs/RELEASE.md` if releasing, and delete the
`feat/sqlite-dialect` branch.

## Self-review (against spec 002 §7 + ADR 0012)

- **Driver `modernc.org/sqlite`, cgo-free, dbtest-only** → Task 3 (dep in dbtest; Step 7 proves sqlite module
  is driver-free). ✓
- **One-shot atomic Claim (no FOR UPDATE)** → Task 3 `Claim` (ADR 0012 D2). ✓
- **Lease-only, no LockDialect → ErrLockStrategyUnsupported** → Task 3 dialect (no `_ LockDialect` assertion)
  + `TestSQLiteLockStrategyUnsupported` (D1). ✓
- **Exact ON CONFLICT … RETURNING inbox; MySQLFamily=false; pragma_index_list/sqlite_master probes** → Task 3
  `InsertInboxIfAbsent`/`SchemaExists`/`MsgIDUniqueIndexExists` + `sqliteKit` (D3). ✓
- **INTEGER unix-micros timestamps, DB-clock only** → `nowMicros`, DDL, every method (D4). ✓
- **Reference DDL (validated, no SPI DDL method)** → Task 3 `ddl.go` (D5). ✓
- **Driver-free DSN builder, opinionated+overridable** → Task 1 `dsn.go` + `dsn_test.go` (D6). ✓
- **RunTestSQLite WAL-temp-file default (via sqlite.DSN) + WithSharedMemory; omit RunLock; SingleWriter skip
  of CommitGatesVisibility** → Task 3 (D7). ✓
- **Two behavior-preserving harness changes** (conditional `ClaimLock` case + `TestKit.SingleWriter` gate on
  `CommitGatesVisibility`) → Task 2 (D8, audit F1). ✓
- **go.work / CI matrix / release / RELEASE.md + pin `@latest` CI tools (the CI problem)** → Task 1 (go.work) +
  Task 4 (CI/release/docs/pins). ✓
- **Audit round 1 findings folded** (F1 blocker → Task 2 SingleWriter gate + spec/ADR correction; F2 → spike-
  proven claim; F3 → Task 1 minimal go.mod; F4 → verified bound pragma; F5 → Task 3 download-then-build red;
  F6 → RunTestSQLite dogfoods sqlite.DSN; F7 noted in Task 3 Step 6). ✓
- **Coverage ≥85% + every hot-path/typed-error branch covered** → Task 3 Step 9 + whole-branch gate. ✓

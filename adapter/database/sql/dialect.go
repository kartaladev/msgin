package sql

import (
	"context"
	stdsql "database/sql"
	"fmt"
	"regexp"
	"time"
)

// RecommendedMaxPayloadBytes is the recommended value to pass to the runtime's
// WithMaxPayloadBytes when consuming from a sql source (ADR 0010 D7). The byte
// cap is a runtime concern, not an adapter default — the adapter never sees the
// typed payload and cannot divert an over-size row — so the sql package ships
// this copy-pasteable recommendation (1 MiB) rather than forcing a default. A
// DB message table is populated by trusted producers, so it is less exposed
// than an untrusted network firehose; size this to your largest legitimate
// message.
const RecommendedMaxPayloadBytes = 1 << 20 // 1 MiB

// Querier is the subset of *database/sql.DB and *database/sql.Tx that the
// LeaseDialect uses. Both *sql.DB and *sql.Tx satisfy it, so a LeaseDialect method runs
// unchanged on the connection pool or inside a caller-supplied transaction
// (e.g. the transactional-outbox Insert on a *sql.Tx).
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (stdsql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*stdsql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *stdsql.Row
}

// ClaimedRow is one row returned by LeaseDialect.Claim: the persisted envelope plus
// the lease bookkeeping. Headers and Payload are the raw framed bytes (JSON
// headers, wire body); the runtime, not the adapter, decodes the payload into
// its typed value. DeliveryCount is the post-increment claim count (populates
// the msgin.delivery-count header); LeaseEpoch is the fence token used to make
// Ack/Nack idempotent against an expired-and-reclaimed lease.
type ClaimedRow struct {
	ID            int64
	MsgID         string
	Headers       []byte
	Payload       []byte
	DeliveryCount int
	LeaseEpoch    int64
}

// LeaseDialect is the exported SPI a caller supplies, as the required dialect
// argument to NewPollingSource/NewOutboundAdapter, to teach the sql adapter a
// database's exact SQL — the extension point for wire-compatible derivatives
// and per-engine quirks. The built-ins are PostgresDialect()/MySQLDialect().
// Every method fully owns its statement(s) and any
// multi-statement transaction orchestration; no cross-dialect SQL runs. All
// persisted timestamps use the DB server clock (now()), never the app clock,
// and durations are passed as interval-typed parameters, so there is no
// app↔DB skew in lease-expiry or visibility comparisons (ADR 0010 D3/D4).
//
// The lease/claim strategy this LeaseDialect implements is: Claim leases claimable
// rows (bumping delivery_count and lease_epoch), Ack DELETEs a fenced row, and
// Nack clears the lock and pushes visible_after out by the requested delay.
//
// This is a pre-1.0 (v0) contract that may still evolve; the lock/FOR UPDATE
// strategy's ClaimLock method is added in a later increment.
type LeaseDialect interface {
	// Claim leases up to limit claimable rows for lockedBy, treating any lease
	// older than leaseTTL as expired (and therefore claimable again — the
	// reaper is inlined into the claim predicate). It returns the leased rows
	// with delivery_count and lease_epoch already incremented.
	Claim(ctx context.Context, q Querier, table string, limit int, lockedBy string, leaseTTL time.Duration) ([]ClaimedRow, error)

	// Ack settles a delivery by deleting its row, fenced on id + lockedBy +
	// epoch. applied is false (with a nil error) when the fence matches no row
	// — the lease expired and another worker re-claimed it — so the caller can
	// suppress a phantom success for a settle it did not actually perform.
	Ack(ctx context.Context, q Querier, table string, id int64, lockedBy string, epoch int64) (applied bool, err error)

	// Nack returns a delivery to the queue, fenced on id + lockedBy + epoch: it
	// clears the lock and sets visible_after to now()+delay so the row is
	// invisible until the delay elapses. applied is false (nil error) on a
	// fence miss, as for Ack.
	Nack(ctx context.Context, q Querier, table string, id int64, lockedBy string, epoch int64, delay time.Duration) (applied bool, err error)

	// Insert writes a new message with the already-framed headers and payload,
	// becoming visible after delay (delay 0 = immediately). q may be the pool
	// or a caller's transaction (transactional outbox).
	Insert(ctx context.Context, q Querier, table, msgID string, headers, payload []byte, delay time.Duration) error

	// EnsureSchema idempotently creates the table and its claim index
	// (CREATE ... IF NOT EXISTS). It is optional and opt-in; msgin never runs
	// DDL implicitly on the production path.
	EnsureSchema(ctx context.Context, q Querier, table string) error

	// SchemaExists reports whether the table exists, via a portable
	// information_schema probe that imports no SQL driver (ADR 0010 D2).
	SchemaExists(ctx context.Context, q Querier, table string) (bool, error)
}

// Note: reference-DDL generation is deliberately NOT a LeaseDialect interface
// method. A string-returning method structurally cannot return
// ErrInvalidTableName, so exposing DDL(table) on the interface would be an
// unvalidated SQL-injection path (the identifier cannot be a bound parameter).
// The only public reference-DDL entry point is the package-level PostgresDDL,
// which validates the table first; each dialect keeps its builder unexported
// (ADR 0010 D3 identifier-safety, review finding I1).

// InboxDialect is the narrow, segregated SPI for the idempotent-consumer dedup
// inbox (ADR 0010 D10). It is deliberately NOT the fat source LeaseDialect
// (interface-segregation): a derivative author fixing one Claim quirk must not
// be forced to implement inbox-dedup SQL, and a deduper never needs claim/lease
// SQL. The built-in PostgresInboxDialect()/MySQLInboxDialect() (the same stateless
// values as PostgresDialect()/MySQLDialect()) satisfy it and are passed as the
// required dialect argument to NewInboxDeduper; a caller may also supply their
// own InboxDialect for a wire-compatible derivative. Like
// LeaseDialect, every method fully owns its SQL and uses the DB server clock for
// processed_at; no cross-dialect SQL runs.
//
// Reference-DDL generation is deliberately NOT a method here, for the same
// identifier-injection reason as LeaseDialect (above): the sole public entry point is
// the package-level InboxDDL, which validates the table first and dispatches to
// the built-in for its exact CREATE TABLE.
type InboxDialect interface {
	// InsertInboxIfAbsent records msgID in the dedup table on q (the caller's
	// business transaction) and reports whether it was ALREADY present. The
	// verdict is derived precisely, never from rowsAffected: Postgres via
	// INSERT ... ON CONFLICT (msg_id) DO NOTHING RETURNING (exact), MySQL via
	// INSERT IGNORE plus a verifying SELECT (INSERT IGNORE demotes non-duplicate
	// data errors to rowsAffected==0, which the SELECT distinguishes from a
	// genuine duplicate — ErrInboxInsertFailed on the demoted case). So
	// already==true means genuinely a duplicate, nothing else (ADR 0010 D10).
	InsertInboxIfAbsent(ctx context.Context, q Querier, table, msgID string) (already bool, err error)

	// PurgeInbox deletes dedup rows whose processed_at is older than olderThan
	// (evaluated on the DB clock) and returns the number removed.
	PurgeInbox(ctx context.Context, q Querier, table string, olderThan time.Duration) (int64, error)

	// EnsureInboxSchema idempotently creates the dedup table (and its
	// processed_at retention index) — CREATE ... IF NOT EXISTS. It is optional
	// and opt-in; msgin never runs DDL implicitly on the production path (D2).
	EnsureInboxSchema(ctx context.Context, q Querier, table string) error

	// SchemaExists reports whether the table exists, via the same portable
	// information_schema probe LeaseDialect uses (no SQL driver import, D2). It is
	// shared with LeaseDialect — a built-in satisfies both with one implementation.
	SchemaExists(ctx context.Context, q Querier, table string) (bool, error)

	// MsgIDUniqueIndexExists reports whether the table's msg_id column
	// participates in a unique or primary-key index — the constraint the dedup
	// relies on (without it MySQL/MariaDB's INSERT IGNORE never conflicts and the
	// dedup silently never works). It is a portable information_schema probe (no
	// SQL driver import, matching SchemaExists) and is used by InboxDeduper.Ready
	// to fail fast on a mis-provisioned schema (ErrInboxNoUniqueConstraint). Added
	// per the Task 10 review (ADR 0010 D10).
	MsgIDUniqueIndexExists(ctx context.Context, q Querier, table string) (bool, error)
}

// identPattern is the only accepted shape for a table (or derived index)
// identifier. It admits no quote, semicolon, or whitespace, so a validated
// name is safe to dialect-quote and interpolate.
var identPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidateIdent returns ErrInvalidTableName unless name matches identPattern
// (^[A-Za-z_][A-Za-z0-9_]*$ — no quote, semicolon, or whitespace). It is part
// of the dialect-author SPI (ADR 0010 D3, ADR 0011): every LeaseDialect/
// InboxDialect method and every reference-DDL builder validates its table (or
// derived index) identifier with ValidateIdent BEFORE dialect-quoting and
// interpolating it into SQL — the identifier cannot be a bound parameter, so
// this is the sole injection guard. A dialect author implementing a new
// LeaseDialect/InboxDialect MUST call ValidateIdent first in every method that
// takes a table name.
func ValidateIdent(name string) error {
	if !identPattern.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrInvalidTableName, name)
	}
	return nil
}

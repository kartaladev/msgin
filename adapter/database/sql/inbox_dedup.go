package sql

import (
	"context"
	stdsql "database/sql"
	"fmt"
	"time"

	msgin "github.com/kartaladev/msgin"
)

// defaultInboxTable is the dedup-inbox table name used when WithInboxTable is
// unset — the sensible, opinionated default (CLAUDE.md), so the common case
// needs no configuration.
const defaultInboxTable = "msgin_inbox"

// inboxConfig accumulates InboxOption settings before NewInboxDeduper builds an
// InboxDeduper.
type inboxConfig struct {
	table string
}

// InboxOption configures an InboxDeduper built by NewInboxDeduper. It is a
// distinct type from the Source's Option (interface-segregation): a deduper
// exposes only the dedup-relevant knobs (the table; the dialect is a required
// positional constructor argument, not an option), never claim/lease or
// transactional-outbox options.
type InboxOption func(*inboxConfig)

// WithInboxTable sets the dedup-inbox table name. Unset, it defaults to
// "msgin_inbox" (a sensible default, CLAUDE.md). An empty or otherwise invalid
// identifier is rejected by NewInboxDeduper with ErrInvalidTableName — it is not
// silently coerced back to the default, so a caller who passes "" learns of the
// mistake rather than unknowingly sharing the default table.
func WithInboxTable(table string) InboxOption {
	return func(c *inboxConfig) { c.table = table }
}

// InboxDeduper is the idempotent-consumer dedup inbox (ADR 0010 D10, durable
// consume strategy 2): the different-DB idempotent consumer. It records each
// processed msgin.id in a dedup table so a redelivery (msgin sources are
// at-least-once) can be recognized and its business effect skipped — turning
// at-least-once delivery into exactly-once effect.
//
// It is deliberately independent of a Source: a handler consuming from ANY
// source (any adapter, any strategy) may adopt it, and it records into whatever
// business database the handler writes to — which need not be the message
// source's database. That is what makes it the cross-DB strategy: when business
// data lives in a different database than the source (so no single transaction
// can span both), dedup-by-id gives exactly-once effect without a shared tx.
//
// The zero-config baseline stays plain at-least-once with the caller responsible
// for idempotency; adopting the deduper is an explicit opt-in (the handler calls
// MarkProcessed inside its business transaction).
type InboxDeduper struct {
	db      *stdsql.DB
	table   string
	dialect InboxDialect
}

// NewInboxDeduper builds an InboxDeduper over businessDB — the database the
// consuming handler writes its business changes to (NOT necessarily the message
// source's database; that different-DB case is the point of this strategy, ADR
// 0010 D10) — using dialect to generate the exact SQL (ADR 0011 — the dialect
// is a required, explicit constructor argument; there is no driver
// auto-detect). Pass postgres.InboxDialect() (adapter/database/sql/postgres),
// sql.MySQLInboxDialect(), or
// your own InboxDialect implementation. The dedup table defaults to
// "msgin_inbox" (WithInboxTable).
//
// A nil businessDB is msgin.ErrNilAdapter; an invalid table identifier is
// ErrInvalidTableName; a nil dialect is ErrNilDialect — all at construction, so
// misuse fails loudly up front rather than on the first MarkProcessed.
func NewInboxDeduper(businessDB *stdsql.DB, dialect InboxDialect, opts ...InboxOption) (*InboxDeduper, error) {
	if businessDB == nil {
		return nil, msgin.ErrNilAdapter
	}

	cfg := inboxConfig{table: defaultInboxTable}
	for _, o := range opts {
		o(&cfg)
	}

	if err := ValidateIdent(cfg.table); err != nil {
		return nil, err
	}
	if dialect == nil {
		return nil, ErrNilDialect
	}

	return &InboxDeduper{db: businessDB, table: cfg.table, dialect: dialect}, nil
}

// MarkProcessed records msgID in the dedup table inside the caller's business
// transaction and reports whether the id was ALREADY processed. It is the heart
// of the idempotent consumer (ADR 0010 D10):
//
//	already, err := deduper.MarkProcessed(ctx, tx, msg.Headers.ID())
//	if err != nil { /* roll back tx; the message is retried */ }
//	if already {
//		// duplicate redelivery: skip the business effect, commit (or roll
//		// back) tx, and return nil so the runtime Acks the message.
//	}
//	// first time: apply the business effect on tx, then commit.
//
// # Why a concrete *sql.Tx (not a Querier)
//
// The parameter is the concrete *sql.Tx BY DESIGN (ADR 0010 D10 HIGH 5): it
// makes passing the auto-committing pool (*sql.DB) a COMPILE error. If the dedup
// row could be written on the pool it would commit independently of the business
// writes, so a crash after the dedup insert but before the business commit would
// mark the id processed while its effect never happened — the message would then
// be deduplicated and silently LOST on redelivery. Recording inside the caller's
// tx makes the dedup row and the business writes commit or roll back atomically,
// so a rolled-back attempt is genuinely retried (a later MarkProcessed reports
// already=false), never falsely deduped.
//
// A nil tx is refused with ErrNilTx before any database call (the
// no-panic-on-caller-input rule); already is false on that error. already==true
// means genuinely a duplicate and nothing else — the verdict is derived
// precisely by the InboxDialect, never from rowsAffected (see InboxDialect).
//
// msgID MUST fit the dedup column (the reference schema uses VARCHAR(255);
// msgin's own ids are far shorter). An over-length id can be silently truncated
// on MySQL/MariaDB, so two distinct ids sharing a 255-char prefix would collide
// (one falsely deduped) — keep ids within the column width.
func (d *InboxDeduper) MarkProcessed(ctx context.Context, tx *stdsql.Tx, msgID string) (already bool, err error) {
	if tx == nil {
		return false, ErrNilTx
	}
	return d.dialect.InsertInboxIfAbsent(ctx, tx, d.table, msgID)
}

// EnsureSchema idempotently creates the dedup table (and its processed_at
// retention index) via the dialect's CREATE ... IF NOT EXISTS. It is optional
// and opt-in (dev/test/opt-in callers); production callers provision the schema
// from the reference InboxDDL instead — msgin never runs DDL implicitly on the
// production path (ADR 0010 D2).
func (d *InboxDeduper) EnsureSchema(ctx context.Context) error {
	return d.dialect.EnsureInboxSchema(ctx, d.db, d.table)
}

// Ready is the fail-fast boot check (ADR 0010 D2, mirroring the Source): it
// returns ErrSchemaNotReady (naming the table) when the dedup table is not
// initialized, then ErrInboxNoUniqueConstraint (naming the table) when the table
// exists but its msg_id column carries no unique/primary-key constraint — the
// constraint the dedup depends on (without it MySQL/MariaDB's INSERT IGNORE never
// conflicts, so the dedup silently never works). Both are boot failures, so a
// forgotten migration or a mis-provisioned table fails the deploy immediately
// rather than after silently double-processing. The reference DDL (InboxDDL)
// provides the constraint. Call it once at startup.
func (d *InboxDeduper) Ready(ctx context.Context) error {
	exists, err := d.dialect.SchemaExists(ctx, d.db, d.table)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%w: table %q not initialized; run EnsureSchema or apply the DDL",
			ErrSchemaNotReady, d.table)
	}
	unique, err := d.dialect.MsgIDUniqueIndexExists(ctx, d.db, d.table)
	if err != nil {
		return err
	}
	if !unique {
		return fmt.Errorf("%w: table %q; apply the reference InboxDDL (its msg_id PRIMARY KEY) or add a UNIQUE(msg_id) constraint",
			ErrInboxNoUniqueConstraint, d.table)
	}
	return nil
}

// Purge deletes dedup rows whose processed_at is older than olderThan (evaluated
// on the DB clock) and returns the number removed. It is manual — no background
// goroutine (D4 precedent) — so the caller schedules it (e.g. a periodic job).
//
// # Retention safety (read before choosing olderThan — ADR 0010 D10 MEDIUM 10)
//
// Purge is ONLY safe when the source has a FINITE msgin.RetryPolicy.MaxAttempts.
// olderThan MUST exceed the source's maximum redelivery window — the longest
// time a still-redeliverable copy of a message can remain in flight (attempts ×
// backoff, plus lease/visibility margins). Purge a dedup row while a copy of
// that id can still be redelivered and the late redelivery reads already=false
// and DOUBLE-PROCESSES the effect — the exact failure the deduper exists to
// prevent. Under the DEFAULT MaxAttempts==0 (retry forever) the redelivery
// window is UNBOUNDED, so there is NO safe olderThan and you must not Purge (or
// set a finite MaxAttempts first).
//
// A non-positive olderThan is REFUSED with ErrInvalidRetention before any DB call
// (a zero/negative cutoff is now()-or-future, which would purge the ENTIRE inbox
// and double-process every id). The library cannot pick a safe POSITIVE value for
// an unknown caller, so a positive olderThan is not further validated — sizing it
// above your source's max redelivery window (above) stays the caller's
// responsibility.
func (d *InboxDeduper) Purge(ctx context.Context, olderThan time.Duration) (int64, error) {
	if olderThan <= 0 {
		return 0, ErrInvalidRetention
	}
	return d.dialect.PurgeInbox(ctx, d.db, d.table, olderThan)
}

// Compile-time assertion that the built-in mysql dialect satisfies the
// segregated InboxDialect SPI, so it may be passed as NewInboxDeduper's
// required dialect argument. The postgres dialect's identical assertion lives
// in its own module (adapter/database/sql/postgres, Plan 006 Task 4).
var _ InboxDialect = mysqlDialect{}

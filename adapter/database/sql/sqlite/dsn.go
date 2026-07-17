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

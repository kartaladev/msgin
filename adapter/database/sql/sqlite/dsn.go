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
//
// A nil element in opts is ignored — DSN returns a string and has no error to
// return, so there is nowhere to report the fault (Spec 015 §3.3). The skip is
// therefore SILENT, and the cost is the caller's to avoid: the resulting DSN
// carries msgin's defaults for whatever the dropped option would have set — WAL
// journal mode, a 5s busy_timeout, and a file-backed database — so a dropped
// [WithJournalMode]("") silently keeps WAL rather than omitting the pragma, a
// dropped [WithBusyTimeout] silently keeps 5s, and a dropped [WithSharedMemory]
// silently targets a different database (a file instead of the shared in-memory
// one). The returned string looks valid and opens fine either way, so build the
// option unconditionally rather than relying on this. Every non-nil element
// still applies, whether it sits before or after the nil one. (A nil opts SLICE
// — DSN(path) or DSN(path, nils...) — is a normal zero-option call, unaffected.)
func DSN(path string, opts ...DSNOption) string {
	cfg := dsnConfig{journalMode: defaultJournalMode, busyTimeout: defaultBusyTimeout}
	for _, o := range opts {
		if o == nil {
			continue // R3: skip; there is no surface to report the fault through.
		}
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

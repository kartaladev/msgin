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

// TestDSN_NilOptionElement proves a nil ELEMENT of opts is SKIPPED rather than a
// panic (Spec 015 §3.1/§3.3, family R3: DSN returns a bare string and has no
// error surface to report the fault through).
//
// The AC-4 pair is the point: the surviving option must still apply whether it
// sits BEFORE or AFTER the nil, so a skip implemented as an early `break` — or
// one that discarded the accumulated config — dies here. The first case records
// the silent cost the godoc names: the dropped option's default survives, and
// the DSN still looks valid.
func TestDSN_NilOptionElement(t *testing.T) {
	cases := []struct {
		name   string
		got    func() string
		assert func(t *testing.T, dsn string)
	}{
		{
			// AC-1: no panic. The dropped option leaves msgin's defaults
			// (WAL + 5s busy_timeout + file-backed) in place — the concrete
			// consequence DSN's godoc is required to name.
			name: "nil element alone keeps msgin's defaults",
			got:  func() string { return sqlite.DSN("/x/y.db", nil) },
			assert: func(t *testing.T, dsn string) {
				const want = "file:/x/y.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
				if dsn != want {
					t.Fatalf("DSN = %q, want %q", dsn, want)
				}
			},
		},
		{
			// AC-4, half 1: the option BEFORE the nil still applied.
			name: "option before the nil still applies",
			got:  func() string { return sqlite.DSN("ignored", sqlite.WithSharedMemory(), nil) },
			assert: func(t *testing.T, dsn string) {
				const want = "file::memory:?cache=shared&_pragma=busy_timeout(5000)"
				if dsn != want {
					t.Fatalf("DSN = %q, want %q", dsn, want)
				}
			},
		},
		{
			// AC-4, half 2: the option AFTER the nil still applied. A skip
			// implemented as `break` passes half 1 and fails exactly here.
			name: "option after the nil still applies",
			got:  func() string { return sqlite.DSN("ignored", nil, sqlite.WithSharedMemory()) },
			assert: func(t *testing.T, dsn string) {
				const want = "file::memory:?cache=shared&_pragma=busy_timeout(5000)"
				if dsn != want {
					t.Fatalf("DSN = %q, want %q", dsn, want)
				}
			},
		},
		{
			// Two nils around a real option: both are skipped, the survivor
			// applies, and the defaults hold for everything else.
			name: "a nil on each side of a real option",
			got: func() string {
				return sqlite.DSN("/x/y.db", nil, sqlite.WithJournalMode("DELETE"), nil)
			},
			assert: func(t *testing.T, dsn string) {
				const want = "file:/x/y.db?_pragma=journal_mode(DELETE)&_pragma=busy_timeout(5000)"
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

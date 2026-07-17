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

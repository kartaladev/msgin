module github.com/kartaladev/msgin/adapter/database/sql/mysql

go 1.25.0

require github.com/kartaladev/msgin v0.0.0

require github.com/jonboulle/clockwork v0.5.0 // indirect

// Dev-time only: no published tag of the root module carries the engine
// changes this dialect depends on yet (ADR 0011 / Plan 006). Swapped for a
// pinned `require` once the root is tagged (spec 002 §8, Plan 006 Task 6).
replace github.com/kartaladev/msgin => ../../../..

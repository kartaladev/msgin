module github.com/kartaladev/msgin/adapter/database/sql/sqlite

go 1.25.0

require github.com/kartaladev/msgin v0.0.0

require github.com/jonboulle/clockwork v0.5.0 // indirect

// Dev-time only: no published tag of the root module carries this engine yet
// (ADR 0011/0012). Swapped for a pinned require once the root is tagged (spec 002 §8).
replace github.com/kartaladev/msgin => ../../../..

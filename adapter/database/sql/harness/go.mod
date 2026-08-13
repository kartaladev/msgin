module github.com/kartaladev/msgin/adapter/database/sql/harness

go 1.25.0

require (
	github.com/kartaladev/msgin v0.0.0
	github.com/stretchr/testify v1.11.1
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/jonboulle/clockwork v0.5.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// Dev-time only: no published tag of the root module carries the engine
// changes this harness depends on yet (ADR 0011 / Plan 006). Swapped for a
// pinned `require` once the root is tagged (spec 002 §8, Plan 006 Task 6).
replace github.com/kartaladev/msgin => ../../../..

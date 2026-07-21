# ADR 0026 — Rename `HeaderID` to `HeaderMessageID` and its value to `msgin.message-id`

- **Status:** Proposed (2026-07-22) — two adversarial audit rounds have run against
  [Plan 022](../plans/022-header-message-id-rename.md); round 2 returned NOT READY and its findings are folded in. A
  third round gates implementation.
- **Requested by:** the user, mid-planning of the (since-split) producer-retry increment.
- **Spec:** none of its own — this is a naming/format correction to the core envelope defined by
  [Spec 001 — Messaging core](../specs/001-messaging-core.md). · **Plan:** [022](../plans/022-header-message-id-rename.md)
- **Relates to:** [ADR 0001 — Message payload typing](0001-message-payload-typing.md) (the envelope this header belongs
  to), [ADR 0023 — HTTP channel adapter](0023-http-channel-adapter.md) (the reserved-namespace strip that keys off the
  `msgin.` prefix), [ADR 0021 — SQL group store](0021-sql-group-store.md) (the persisted envelope framing this value
  appears in).

## Context

The core message envelope reserves a `msgin.` header namespace. Its identity key is currently declared as:

```go
HeaderID = "msgin.id"
```

Two problems, one in the Go surface and one on the wire:

1. **`HeaderID` is ambiguous at the call site.** The envelope carries several ids — `HeaderCorrelationID`,
   `HeaderSequenceNumber`, and adapter-level ids — so a bare `HeaderID` does not say *which* id it is. It reads
   especially poorly next to `HeaderCorrelationID` in the same expression, which is exactly where it most often appears
   (the HTTP adapter's correlation resolution, the gateway's save/restore, the splitter's group stamping).
2. **`"msgin.id"` is equally ambiguous on the wire**, where the reader has no Go identifier to disambiguate it — only
   the string. A persisted envelope row or a forwarded HTTP header showing `msgin.id` alongside `msgin.correlation-id`
   invites exactly the confusion the rename removes.

The constant is referenced **36 times across 4 Go packages** — `msgin` (`message.go`, `message_test.go`,
`splitter.go`, `splitter_test.go`), `msghttp` (`adapter/http/encode.go`), `msginsql`, and the **separate
`adapter/database/sql/harness` module** — spread over **13 files** under `adapter/database/sql` (7 in the adapter,
6 under `harness/`). Of the 36, **28 are code references** a `gopls` rename resolves and **8 are comment prose** it
correctly leaves alone.

The string literal `"msgin.id"` appears **exactly once**, at the declaration — every functional use goes through the
constant. **But that is the literal radius, not the prose radius:** a further **22 doc-comment mentions** of the value
are spread across 10 Go files, two of which are load-bearing public contract text — `adapter/database/sql/framing.go`,
inside the godoc block explicitly headed *"On-wire header format (stability contract)"*, and
`adapter/http/options.go`'s `WithResponseHeaders` security CAUTION enumerating leakable reserved keys. Leaving those
stale would make the published godoc describe a key that no longer exists, so they are in scope for the change.

> These counts were corrected on 2026-07-22 after the round-2 audit; the first draft claimed "5 packages", "twelve
> files", omitted the two `_test.go` files, and treated the single string literal as the whole value-change radius.

**The timing is what makes this cheap.** The repository has **zero git tags**: nothing is released, no consumer has
imported the symbol, and no long-lived production store holds the value. The same change after `v0.1.0` would be a
major-version break plus a data migration for every deployed outbox. Doing it now costs a mechanical rename; doing it
later costs a deprecation cycle.

## Decision

### 1. Rename the Go identifier: `HeaderID` → `HeaderMessageID`

Mechanically, via `gopls`' Rename refactor across the workspace (all modules in `go.work`), so no reference is missed
and no unrelated identifier is caught by a text substitution.

### 2. Change the value: `"msgin.id"` → `"msgin.message-id"`

Chosen over keeping `"msgin.id"` **deliberately**, with the data-format break understood and accepted (§ Consequences).
The value stays inside the reserved `msgin.` namespace, so every mechanism keyed on `reservedHeaderPrefix` — the HTTP
adapter's case-insensitive client-forgery strip, the reply-header strip that [Plan 024](../plans/024-http-outbound.md)'s O2 will add — keeps covering it with
no change. It also adopts the hyphenated multi-word form the rest of the namespace already uses
(`msgin.correlation-id`, `msgin.delivery-count`, `msgin.sequence-number`), which `msgin.id` was the sole exception to.

### 3. No deprecation alias

No `const HeaderID = HeaderMessageID` shim is kept. With zero tags there is no consumer to deprecate *for*, and a shim
would leave the ambiguous name in the public surface indefinitely — the exact thing this ADR removes. A caller
upgrading across this change gets a compile error naming the symbol, which is the clearest possible migration signal.

### 4. It ships as its own increment, on its own branch

**Revised 2026-07-22, after the round-1 adversarial audit.** The rename was initially folded into the in-flight
producer-retry/HTTP increment as its "Task 0". Three independent auditors each flagged the combined review surface as a
real risk: a large mechanical rename diff degrades exactly the whole-branch `/code-review` + `/security-review` gate
that, on Plan 021, caught two proven vulnerabilities every per-task review had cleared. Since the rename has **zero
coupling** to the retry work, it now ships alone as [Plan 022](../plans/022-header-message-id-rename.md), ahead of
[Plan 023](../plans/023-producer-outbound-retry.md) (core retry) and
[Plan 024](../plans/024-http-outbound.md) (HTTP outbound).

Its commit is therefore a pure-rename diff by construction rather than by discipline, and the plan verifies that
mechanically (Step 8) rather than trusting it.

## Consequences

**Positive**

- Call sites disambiguate themselves: `msg.Header(msgin.HeaderMessageID)` next to
  `msg.Header(msgin.HeaderCorrelationID)` now reads as two different things, because it names two different things.
- The wire value is self-describing to an operator reading a stored envelope or an HTTP header dump, with no Go source
  in hand.
- The `msgin.` namespace becomes internally consistent — every multi-word key is hyphenated.
- Costs one mechanical commit **now**; it would cost a major bump plus a data migration after the first tag.

**Negative / accepted**

- **This is a breaking change to an exported symbol.** Under CLAUDE.md's SemVer gate that implies a major bump — moot
  here only because the repo is pre-`v0.1.0` and untagged. It must NOT be repeated after the first tag without a major
  version.
- **This is also a DATA-FORMAT break, which the identifier rename alone would not have been.** The `database/sql`
  adapter persists the header map by key into its stored envelope, so rows written before this change carry
  `msgin.id` and rows written after carry `msgin.message-id`. A consumer with an existing outbox/queue table would see
  neither side's id across the upgrade, and a mixed-version deployment (old and new binaries against one table) would
  produce both keys concurrently. **No migration is shipped**, because there is no released version to migrate *from* —
  and that is the whole justification for taking the break now. Any consumer already running an unreleased build off
  `main` with a populated table must either drain it before upgrading or run
  `UPDATE <table> SET headers = headers - 'msgin.id' || jsonb_build_object('msgin.message-id', headers->'msgin.id') WHERE headers ? 'msgin.id';`
  (PostgreSQL form; adapt per dialect) themselves. This is stated so the break is a recorded decision, not a discovered
  surprise.
- **`gorelease` cannot verify any of this** — the repo has zero tags, so it reports "inferred base version: none"
  rather than a compatibility diff. Compatibility is established by inspection, as in every prior increment. This ADR is
  a further argument for cutting `v0.1.0` soon: it is the last moment such a change is free.
- **~~The Plan 022 review gate widens.~~ RESOLVED by the §4 revision.** Folding the rename into a branch that already
  carried a core change, a new adapter, and a new SPI implementation would have compounded the risk Spec 013 §7 already
  accepted. The round-1 audit judged "land it first, in its own commit" insufficient mitigation, and the rename was
  split to its own branch. The remaining cost is one extra merge cycle, which is cheap relative to a degraded security
  review on the increment that introduces an outbound network client.

## Traceability

- **Plan:** [022](../plans/022-header-message-id-rename.md).
- **Affects:** `message.go` (declaration), `message_test.go`, `splitter.go`, `splitter_test.go`,
  `adapter/http/encode.go`, `adapter/http/options.go`, `adapter/database/sql/**` **including the separate `harness`
  module** (which `go build ./...` from the repo root does NOT reach — it must be built standalone with `GOWORK=off`,
  as CI does), and the 13 markdown files listed in Plan 022 Step 5.
- **Does not supersede any ADR.** It amends the header-key vocabulary introduced with the core envelope
  ([Spec 001](../specs/001-messaging-core.md)); ADRs citing `HeaderID` by name are updated in place by Plan 022 Task 0
  Step 5, since they describe the same header under its new name rather than recording a superseded decision.

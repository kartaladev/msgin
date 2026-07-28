# Plan 027 — Adversarial design audit, ROUND 2 (2026-07-27)

> **STATUS: `NEEDS-REVISION`. DO NOT IMPLEMENT.** Three independent Opus auditors were handed the complete
> revised bundle (Spec 014 + ADR 0027 + ADR 0028 + ADR 0029 + Plan 027) on distinct lenses, each under an
> explicit **evidence-or-discarded** contract. **All three returned `NEEDS-REVISION`** — the same verdict as
> round 1, on a materially better bundle.

| Lens | Verdict | Findings |
|---|---|---|
| Design & API correctness of the target state | `NEEDS-REVISION` | 5 HIGH, 8 MED, 3 LOW |
| Plan-level execution (with a full task-by-task **simulation**) | `NEEDS-REVISION` | 5 HIGH, 6 MED, 3 LOW |
| Cross-document consistency & factual accuracy (86-claim verification table) | `NEEDS-REVISION` | 5 HIGH, 7 MED, 3 LOW |

**Convergent** marks a finding reached independently by two or three auditors.

---

## 0. The one sentence that matters

**The round-1 revision fixed the named instances and reproduced the defect class.** Round 1's five false claims
were all *"a total asserted from a partial grep"*. The revision corrected those five — and then asserted six new
totals from partial greps, two of which two separate auditors called out **by that name**. The bundle is not
failing on judgment; it is failing on **method**. §5 proposes the method change.

## A. Convergent findings — highest confidence

### A1 — `poller.go` constructs `ExponentialBackoff`, so the `resilience` leaf claim is false · **HIGH · CONVERGENT (3/3)**

```
poller.go:131-137
func (c *consumer[T]) pollErrorBackoff(n int) time.Duration {
	return ExponentialBackoff{Initial: c.pollInterval, Max: maxPollErrorBackoff, Mult: 2}.Delay(n - 1)
}
```

Real code, not godoc. This falsifies four normative statements: Spec 014 §3 (*"`resilience` is imported by no
other package in the module … verified: only godoc mentions"*), ADR 0027 §2 (*"no subpackage imports another"*),
ADR 0027 Consequences (*"a single fan-in to root — no inter-subpackage edges at all"*), and ADR 0027 §5
(*"makes `resilience` a true leaf"*). The "verification" grepped `NewCircuitBreaker`/`NewTokenBucket` only —
**the identical partial-grep method that produced round-1 finding E5**.

Two distinct edges exist, and the three auditors found different halves:

- **`endpoint → resilience`** — at the Task 7 boundary `poller.go` is still in root, so *root* would import
  `resilience`; after Task 8 it is `endpoint → resilience`. The simulation's only residual inter-package error
  was `endpoint/poller.go:133:9: undefined: ExponentialBackoff`.
- **`adapter/database/sql → resilience`** — `source.go:175,235` and `harness/source.go:112`. These are packages
  of the root module, so "imported by no other package in the module" is false a second way.

Plan Tasks 4–8 instruct the implementer to **halt** on exactly this: *"Any proposed edge from root into a
subpackage, or between two subpackages, is a design error — stop and escalate."*

**Options (an ADR-level decision, not an edit):** (a) accept one `endpoint → resilience` edge and rewrite the
acyclicity argument; (b) **keep `ExponentialBackoff` in root** beside `BackoffStrategy` — it is a value type
with one method, consistent with §5's "governor interfaces stay in root", and this also deletes the two
`adapter/database/sql` change sites; (c) inline the two-line computation in `endpoint`, as §H2 does for
`delayFor`. **(b) is the recommendation** — it makes the leaf claim true instead of retracting it.

### A2 — The adapter blast radius is understated by ~70–120 sites, and no task migrates the adapter tree · **HIGH · CONVERGENT (3/3)**

Plan Task 0: *"**The known non-test adapter code changes are exactly:** `adapter/database/sql/source.go:175,235`
and `adapter/database/sql/harness/source.go:112`."* ADR 0027 Consequences: *"adapters compile against root
unchanged **except** for the two enumerated `ExponentialBackoff` construction sites."*

Measured by AST (comments excluded), non-test code referencing symbols that move:

```
  28 adapter/database/sql/harness/lock.go
  18 adapter/database/sql/harness/outbound.go
  17 adapter/database/sql/harness/source.go
   6 adapter/database/sql/harness/groupstore.go
   2 adapter/database/sql/source.go
   1 adapter/database/sql/harness/harness.go
   1 adapter/memory/memory.go        (var _ msgin.StreamingSource = (*Broker)(nil))
   1 adapter/cron/source.go          (same)
   1 adapter/http/sseclient.go       (same)
```

Including tests: **121 sites across 28 files**. `harness` is a **separate module in the CI matrix**, and 69 of
the 72 non-test sites are in it. Root's `go build ./...` — the plan's own "authoritative reference-finder" —
**cannot see it**.

Breakage starts at **Task 2**, not Task 8: `harness/groupstore.go:400,406` are
`require.NoError(t, out.Subscribe(...))`, which stop compiling the moment `Subscribe` returns two values.

`grep -n 'adapter/' docs/plans/027-core-package-layout.md` finds only Task 0's ledger note, Task 1's tidy loop,
and Task 10's CI note. **No task contains a work item for `adapter/`.**

**Fix:** replace Task 0's "exactly" list with the measured per-module inventory, and add an explicit *"update
the adapter tree"* step to Tasks 2, 4, 6, 7, 8 — each naming the modules it breaks and running the `GOWORK=off`
loop over them before its commit.

### A3 — `Subscription` has no destination file; "exactly 14" is unsatisfiable · **HIGH · CONVERGENT (3/3)**

§3.2 says root keeps `Subscription` (`pubsub.go:37`) but names **no destination file** — unlike the
`exchange.go` row, which says *"→ folded into `spi.go`"*. §3.1's closed 14-file list contains neither
`pubsub.go` nor `subscription.go`. The simulation, following §3.2 literally, produced **15** root files, while
Plan Task 11 asserts *"exactly 14"*.

**Fix:** fold `Subscription` into root's `channel.go` beside `SubscribableChannel` (keeps 14, puts the return
type next to the interface returning it) **or** create `subscription.go` and change every "14" to 15.

### A4 — `TopicPublisher`/`TopicSubscriber` are root SPI silently relocated into `channel` · **HIGH · CONVERGENT (2/3)**

```
pubsub_registry.go:11  type TopicPublisher interface { Publish(...) error }
pubsub_registry.go:19  type TopicSubscriber interface { Subscribe(...) (Subscription, error) }
```

`TopicPublisher`'s own godoc: *"Native-topic broker adapters (Kafka, NATS, Redis) implement this using their own
topics, so topic support is handled generically through one SPI."* ADR 0014 and Spec 004 §105 both file it as
**Layer 1 — SPI (adapters implement this)**.

§3.1 moves `pubsub_registry.go` **whole** to `channel/`. Neither name appears in **any** of the five bundle
documents or any RFC. This is `ProbeGate`/A5 recurring: a file classified by its implementation half while
carrying root-contract interfaces. It compiles either way, which is why nobody would notice — but a future
NATS/Redis topic adapter would import `msgin/channel` to implement the seam, inverting the "adapters implement
root SPI" rule with no decision recorded.

**Fix:** `pubsub_registry.go` becomes a **sixth split** — `TopicPublisher`/`TopicSubscriber` fold into `spi.go`;
`PubSub`, `NewPubSub`, `topicSubscription` → `channel`. Add both to §4's SPI list, ADR 0027 §4's table, Task 6.

### A5 — "All 89 error sentinels" — there are 42 · **HIGH · CONVERGENT (2/3)**

```
$ grep -c "errors.New(" errors.go
42
$ go doc -all . | awk '/^var \(/,/^\)/' | grep -oE "^\s+Err[A-Za-z0-9]+" | sort -u | wc -l
42
```

89 matches no reading (root = 42; whole root module = 92). The figure appears twice in normative text —
Spec §3.2 and **§4's closed root contract**, which §9.1 makes an acceptance criterion Task 11 checks
symbol-by-symbol. Round-1 E1 was this exact defect on a different number. *(Origin: `grep -c 'Err[A-Z]'
errors.go` counts godoc lines too.)*

### A6 — The `MessageChannel` call-site census omits the two Aggregator options · **MED · CONVERGENT (2/3)**

*"Four of the five call sites use only `Send`"* is repeated in Spec §1.2 and §5, ADR 0028 Context/§4/§5, and
RFC-0002 §3/§7.3. The real census:

```
aggregator.go:47   func WithOutputChannel(ch MessageChannel) AggregatorOption
aggregator.go:114  func WithExpiredGroupChannel(ch MessageChannel) AggregatorOption
```

Both send-only (`aggregator.go:333`, `:472`), both live adapter API (`harness/groupstore.go:418-419`). So it is
**six of seven**, not four of five. Consequence: §9.4's capability test omits the two positions where the
widening matters most — a durable `QueueChannel` as an Aggregator **output** or **expired-group** sink is
exactly what §10 argues the narrowing enables for a multi-instance deployment.

### A7 — `retry_test.go` does not need splitting; §3.4's split accounting is inverted · **MED · CONVERGENT (2/3)**

`retry_test.go` is **43 lines with one test function** (`TestRetryPolicy_Validate`). Its single
`ExponentialBackoff` mention (`:26`) is a *field value* in a valid-policy fixture — there are no delay
computation cases to hand to `resilience`. §3.4 calls it *"the only one"* that splits; §K row A4 marks round-1
finding A4 **SUPERSEDED** on that basis.

Meanwhile **two files genuinely do cross** — see B2. The accounting is wrong in both directions.

### A8 — Root-staying declarations lose 100% of their coverage · **HIGH · CONVERGENT (2/3)**

Measured, baseline vs. only the ten root-assigned test files from §3.4:

| symbol | baseline | root-tests-only |
|---|---|---|
| `flowcontrol.go:94 OverflowPolicy.String` | 100.0% | **0.0%** |
| `reliability.go:13 (*permanentError).Error` | 100.0% | **0.0%** |

`String()` is a four-arm typed switch covered by exactly one site — `flowcontrol_test.go:121` — and §3.4 sends
`flowcontrol_test.go` to `endpoint` while §3.2 keeps `OverflowPolicy` in root. Coverage is attributed **per test
binary**, so root scores 0%. Worse, Global constraint 4 actively **misdiagnoses** it: *"a pure move that loses
coverage means tests were dropped"* — nothing was dropped, and the worker is sent hunting a non-existent bug.
Under CLAUDE.md an uncovered typed branch is a delivery blocker.

---

## B. Proven by execution — findings the auditors reproduced

### B1 — Task 8 does not compile: `endpoint` reads `Message`'s unexported fields · **HIGH**

The plan-execution auditor applied **every** §3.3 resolution and still got six hard errors. Independently
re-verified during this session:

```
producer.go:416  return Message[any]{payload: msg.payload, headers: msg.headers}, nil
producer.go:418  b, err := p.codec.Encode(msg.payload)
producer.go:422  return Message[any]{payload: any(b), headers: msg.headers}, nil
consumer.go:693  msg := Message[T]{payload: payload, headers: d.Msg.headers}
consumer.go:827  v, ok := m.payload.(T)
consumer.go:834  b, ok := m.payload.([]byte)
```
against `message.go:106-109  type Message[T any] struct { payload T; headers Headers }`.

§3.3 claims *"Verified by grepping every declaration and use site"* and *"After Task 8, no unexported helper
crosses any package boundary"*. **A grep over declarations cannot see struct-field access.** This is round-1
A3's exact defect class, recurring one level down.

It is also a **behavior trap**: a subagent reaching for `New[T](payload)` instead of `NewMessage[T]` would
silently re-stamp `msgin.message-id` and `msgin.timestamp` on every consumed message — and no assertion would
change, so the plan's behavior-preservation guard would not catch it. (`NewMessage` is a bare struct literal,
`message.go:184-186`; `New` is not.)

### B2 — Task 1 leaves the root test binary RED: test-only helpers cross packages · **HIGH**

The consistency auditor copied the tree, ran Task 1 verbatim, and got:

```
./gateway_test.go:273:12: undefined: collector
FAIL	github.com/kartaladev/msgin [build failed]
```

`expr_test.go:29` declares `collector`, used 9× in `gateway_test.go` (`:142,154,172,195,221,248,263,273`).
`codec_test.go:11` declares `order`, used **281×** in `consumer_test.go` and 67× in `poller_test.go` — and
§3.4 sends those to `endpoint` while `codec_test.go` stays in root.

**§3.4's governing rule is wrong for test-only helpers.** *"A `package X_test` binary may import any package"*
is true, but **test packages are not importable**: a `package endpoint_test` binary cannot reach an unexported
type declared in `package msgin_test` by any mechanism. The rule holds for *production* packages and fails for
test fixtures — and §3.4 relies on it to conclude "exactly one file splits", which §K then uses to close
round-1 finding A4.

`go build ./...` does not compile tests, so Task 1's Verify would not catch this.

### B3 — §3.2's "declaration-level and normative" split tables are themselves incomplete · **HIGH**

AST enumeration of the split files against §3.2's "Moves" column:

- **`pubsub.go`** also declares `pubSubConfig` (:39), `defaultPubSubConfig` (:44), `withConfig` (:69) — none
  listed, all required by `channel` (`pubsub.go:49,89,96`; `pubsub_registry.go:29,39,69`).
- **`exchange.go`** also declares `exchangeConfig` (:140) and `newReplyCorrelator` (:49) — neither listed.
  `ExchangeOption` (listed as moving) is literally `func(*exchangeConfig)`.

§3.2 opens *"A subagent follows the table, so each split is enumerated at declaration level"*, and Task 0 makes
the transcribed list the thing every diff is read against — *"a symbol that moves without appearing here is a
finding"*. A worker following it literally leaves five declarations in root, `channel` and `endpoint` fail to
compile, and the worker is simultaneously told that moving an unlisted symbol is a finding.

### B4 — `optErr` stays permanently unreachable; §H5 rescues only one of the two branches · **HIGH**

```
$ grep -rn "optErr" *.go | grep -v _test.go
aggregator.go:19,26      field + doc
aggregator.go:238-239    if cfg.optErr != nil { return nil, cfg.optErr }
expr.go:308,325,326,364,394,395   ← the ONLY writers
```

`expr.go` is deleted by Task 1. `WithReleaseStrategy`, `WithCompletionSize` and the proposed `WithRelease` all
assign `c.release`, never `c.optErr`. So `aggregator.go:238` becomes dead code no public API can reach.

ADR 0029 §3 names both blocks explicitly and claims *"the fallible named type fixes all three"*; §H5 says it
*"resolves B3 … so no coverage is lost"*; Task 9 verifies *"100% on `NewAggregator` and `Handle`"*. Only the
`Handle` release-decision branch is actually rescued. `NewAggregator` stays at ~93.8% with an uncoverable
typed-error branch — the precise failure round-1 B3 predicted and §K marked FIXED.

**Fix:** Task 1 must additionally delete the `optErr` field and the `NewAggregator` guard (they exist solely to
serve the deleted `*Expr` options), and ADR 0029 §3 / §H5 / Task 9 must name only the `Handle` branch.

---

## C. Tooling and process reality

### C1 — Four tooling assertions are wrong; two block Task 0 · **MED**

Verified this session:

| tool | on `PATH` | in `$(go env GOPATH)/bin` | plan says |
|---|---|---|---|
| `gofumpt` | no | **yes** | *"`gofumpt` is not installed"* — **false** |
| `goimports` | no | yes | used **bare** in the header's move procedure step 3 |
| `apidiff` | no | **no** | required by Tasks 0, 3.5, 9, 11 |
| `gorelease` | no | **no** | required by Tasks 0, 11 |
| `gopls` | no | yes | correctly documented |

The header flags the not-on-PATH hazard for `gopls` but not for `goimports`, the tool **every file move depends
on**. And Task 0's very first artifact — the apidiff baseline — is unproducible: the tool is not installed at
all. *(The false `gofumpt` claim was inherited from `docs/HANDOVER.md` and carried forward unverified.)*

**Fix:** use `$(go env GOPATH)/bin/goimports`; correct the gofumpt line; add
`go install golang.org/x/exp/cmd/apidiff@latest golang.org/x/exp/cmd/gorelease@latest` to Task 0.

### C2 — The `expr` module cannot build standalone · **MED**

`git tag | wc -l` → **0**. Every satellite module carries `require github.com/kartaladev/msgin v0.0.0` +
`replace github.com/kartaladev/msgin => ../../../..`. Task 10 says only *"New module `expr` with its own
`go.mod`; add to `go.work`"*, then demands *"all eight modules green standalone under `GOWORK=off`"*. Without a
`replace` and with no published tag, `expr` cannot resolve the root module. Both the plan's gate and CI's
`module` job (which sets `GOWORK: off`) fail.

### C3 — "The ledger" is load-bearing in 8 places and never defined · **MED**

`grep -c 'ledger' docs/plans/027-core-package-layout.md` → **8**. There is no `## Ledger` section, no file path,
no template. Under one-fresh-subagent-per-task this breaks a real handoff: Task 1 writes the deleted `*Expr`
test cases into "the ledger" and **Task 10 reads them back as its acceptance bar**; Task 2 writes its RED
transcript there; Task 11 reads Task 0's move-list and §4 allow-list.

### C4 — Task 3's verification step is unsatisfiable · **MED**

*"`grep -rn 'StreamingSource' .` returns nothing outside `MIGRATION.md`"*. Actual: **120 `.md` hits across ~25
files**, including shipped ADRs (0002, 0010, 0017, 0023) and specs that CLAUDE.md forbids rewriting
(*"supersede rather than rewrite old ADRs"*). Scope it to `--include='*.go'`.

### C5 — Task 8 is ~3× any "M" task and partitions cleanly · **MED**

Task 8's inputs: **2,518 source lines + 7,372 test lines = ~9,890 lines across 24 files**, in one commit —
against Task 4 (`routing`, labelled M) at ~3,146. It also carries two file splits, three helper inlines, two
symbol relocations, B1's field rewrite, and ~70 `harness` sites. Round 1 cut the old Task 11 partly for being
*"the largest task in the plan"*; Task 8 is now materially larger.

The simulation showed it splits cleanly: moving `{consumer, poller, producer, credit, flowcontrol, attempts}`
out left `go build ./endpoint` (`activator`, `gateway`, `exchange`) **green**.

**Fix:** 8a = `exchange` split + `gateway` + `activator`; 8b = `consumer`, `poller`, `producer`, `credit`,
`flowcontrol` option half, `attempts.go`.

---

## D. Smaller corrections

- **D1 · MED —** `WithReleaseStrategy` does not accept `ReleaseStrategy`. §6 keeps
  `WithReleaseStrategy(func(MessageGroup) bool)` while naming the type `ReleaseStrategy` — so
  `agg.WithReleaseStrategy(myReleaseStrategy)` **does not compile**, and `expr.Release`'s return value must go
  to the differently-named `WithRelease`. `WithCorrelationStrategy` (`aggregator.go:56`) is typed with its named
  type. Suggested: `WithReleaseStrategy(ReleaseStrategy)` + rename the sugar to `WithReleaseWhen`.
- **D2 · MED —** `expr.RouteFunc` cannot be expressed in the stated uniform `(string) → (T, error)` provider
  shape: `RouterExpr` (`expr.go:115`) also takes `routes map[string]MessageChannel` and has two construction
  validations. Task 10's branch list omits both.
- **D3 · MED —** Spec §10's mandatory multi-instance review omits `PubSub` (`pubsub_registry.go:26-30`, an
  in-memory topic registry) and `attemptTracker` (`retry.go:95-100`, per-instance attempt counts, so
  `RetryPolicy.MaxAttempts` is per-instance across N nodes).
- **D4 · MED —** ADR 0028 §6.2 rejects a runtime exclusivity guard by rebutting a *global registry*, never
  considering a **channel-local** opt-in (`WithSingleSubscriber()` on `PublishSubscribeChannel`, reusing
  `ErrChannelSubscribed`) that needs no cross-exchange visibility. CLAUDE.md's sensible-defaults rule prefers an
  explicit opt-in with a typed error over documentation alone.
- **D5 · MED —** `ChannelExchange.Close` **already exists** (`exchange.go:356`). Spec §5.2 says *"§5.1 creates
  both"* and ADR 0028 says it *"gains a real `Close`"* — both false; the premise was inherited from round 1
  unverified. Also unrecorded: after `Cancel()`, a post-`Close` reply hits `ErrNoSubscriber` instead of routing
  to `WithUnmatchedReplySink` — a real behavior change in a "behavior-preserving" increment.
- **D6 · MED —** Task 2 drops the `OutboundAdapter`-as-route-destination capability test that §9.4 and §H3
  mandate. `grep 'OutboundAdapter\|memory.Broker'` over the plan finds only a godoc bullet, so §9.4 has **no
  implementing task**.
- **D7 · MED —** Plan Task 7's `toHalfOpen` note claims it *"drops to 87.5% once `resilience` is split out"*.
  Measured: it is **already 87.5% today** and stays 87.5% after the `*Expr` experiment. It is a pre-existing
  gap the extraction surfaces, not a regression — and as written, a worker comparing baselines sees "no drop"
  and skips the case.
- **D8 · MED —** The "9" attribution is wrong. Spec §3.1 and ADR 0027 say it came from RFC-0001 **Appendix A**,
  which is marked SUPERSEDED; Appendix A says **32→21**. The 9 lives in RFC-0001 **§5 Success Metrics** and
  **§7.4**, neither superseded — and §5 *also* still carries the A6-killed "no root file declares a constructor"
  criterion and *"use `gopls` move/rename"*.
- **D9 · MED —** D3 is only partially fixed: `docs/specs/011-http-adapter.md:630`'s phasing table still reads
  `| **5** | 027 | adapter/http/gin …`.
- **D10 · MED —** The crossing arithmetic does not add up. The round-1 A3 table lists **20** identifiers, not
  18; ADR 0027 §6's "8 genuine + 4 + 6 + 2 = 18" sums to **20**, and mixes units (8 counts *table rows*, which
  cover 9 symbols).
- **D11 · LOW —** Task 3.5 says `delayFor` has *"two call sites"*; there are **three** (`consumer.go:730,779`,
  `producer.go:485`). Its *"apidiff shows exactly three additions and zero removals"* also cannot hold against
  the Task 0 baseline, since Task 1 already removed six exported `*Expr` constructors.
- **D12 · LOW —** §3.4's pasted evidence is **wrong**: it claims `grep 'settle{\|\*settle'` returns 0 in
  `aggregator_test.go`; it returns **2** (`settleErrStore` at `:88,92`). The conclusion survives — the type is
  unrelated and `settlement_doubles_test.go` genuinely needs no split — but a fabricated command output in a
  document whose revision theme was *"run the command, paste the output"* is corrosive.
- **D13 · LOW —** §3.3 lists `attemptEntry` as used from `consumer.go`; it is used only in `retry.go:82,99,103`.
- **D14 · LOW —** The `EventDrivenSource` rename edits a user-visible string: `errors.go:22`'s
  `ErrUnsupportedSource` message contains "StreamingSource". Not in any `MIGRATION.md` checklist.
- **D15 · LOW —** `resilience` has no EIP chapter and no Spring counterpart, so §3.5's *"each subpackage doc
  names its EIP chapter and its Spring counterpart"* is unsatisfiable for it. (`channel` does have both: EIP
  ch.3, `org.springframework.integration.channel`.)
- **D16 · LOW —** `IsPermanent` is a **policy classifier**, not a marker inspector: it returns true for
  `ErrPayloadType`/`ErrPayloadDecode`/`ErrPayloadTooLarge`, which never passed through `Permanent`. §4.1 frames
  it as *"the natural public twin of `Permanent`"*, understating what exporting it freezes into the contract.
- **D17 · LOW —** `RetryAfterOf`'s godoc rationale (*"the only caller never passes nil"*) is void on export;
  the nil case is untested public surface.

---

## E. Verified sound — what the revision got right

Recorded so the next pass does not re-open settled ground. Each was independently checked:

- **The eight helper crossings in §3.3 are complete for non-test *declarations*.** Two auditors ran independent
  AST sweeps and found nothing beyond the table (B1's field access is a different category).
- **`breaker` and `jitter` are correctly discarded** — re-verified by all three auditors.
- **The `nilFuncStep`/`boxMessage` ordering wrinkle is correct and complete.** Attacked specifically; holds.
- **ADR 0029 §3's coverage measurement reproduced to the decimal**: `NewAggregator` 100→93.8%,
  `Handle` 100→94.7%. The bundle's strongest measured claim.
- **`toHalfOpen` = 87.5%** — the number is right (its framing as a *drop* is not; D7).
- **The three cycle-forced splits are genuinely forced**; the `exchange.go` fifth split found during the
  revision is real.
- **`isPermanent`/`retryAfterOf` are genuinely forced exports** (`errors.As` over unexported types).
- **`StreamingSource` scope**: exactly 30 occurrences, 12 files, all root-module.
- **"87 `msgin.*` symbols"**, **"32 source + 45 test files"**, **7.1 MB / 144 KB**, **zero tags**,
  `.golangci.yml` claims, the CI matrix gap, the seven-module `expr-lang` tidy loop, `spi.go:42`,
  `flowcontrol.go:71`, `pubsub.go:37`, `backoff.go:12`, `retry.go:43`, `channel.go:24`,
  `adapter/memory/queuestore.go:74` — **all verified**.
- **Task 9's source-compatibility claim verified**: bare closures still infer against named generic func types
  on Go 1.25.
- **`settlement_doubles_test.go` → endpoint whole, no split** — correct conclusion (despite D12's bad evidence).
- **§9.1's `go list -deps .` check is a valid mechanical test** and would pass on the simulated tree.
- **No import cycle** exists after the resolutions.
- **All round-1 traceability repairs landed**: ADR 0013's amendment note and fixed link, ADR 0029→0002 citation,
  ADR 0019 status, ADR 0003 annotation, RFC-0004/0005 promotion lines, RFC index annotations, the
  `SettleMembers` removal (complete and consistent everywhere), status banners on all five bundle documents.

---

## F. The method change this audit calls for

Two rounds have now failed the same way. The recurring defect is not carelessness about any one fact — it is
**deriving a normative move-list by hand and then asserting it was "verified"**. Round 1 produced five false
totals; round 2's revision corrected those and produced six more, plus three defects (B1, B2, B3) that **only a
compiler could have found**.

The plan-execution auditor demonstrated the alternative: it **simulated the entire migration**, and its findings
are compiler output, not argument.

**Recommendation for round 3 — invert the order of work:**

1. **Perform the migration mechanically first**, on a throwaway branch or worktree: move the files, let
   `go build ./...` and the per-module `GOWORK=off` loop drive every fix until the whole workspace is green and
   the tests pass.
2. **Generate the move-list from the result** — file table, declaration-level splits, symbol crossings, test
   placement, root file count, adapter change sites — all emitted by script from the verified tree.
3. **Then write the documents** from that generated artifact, so every table in Spec 014 §3 is a
   *transcription of something that compiled* rather than a prediction.
4. **Keep the design decisions where they are** — §H's six, plus this round's A1(b), A4, B4, D1, D4 — since
   those are genuine judgment calls the code cannot settle.
5. **Re-audit** the regenerated bundle. The consistency lens should then have almost nothing to find, because
   the numbers will not be hand-typed.

This is not a bigger effort than another hand-revision — the simulation has already been done once, by an
auditor, in a single agent run. It is the same work, ordered so the compiler answers the questions the compiler
can answer, and the humans answer the rest.

---

## G. Execution playbook for the derivation run

Written 2026-07-27 for a fresh session. **This run produces FACTS, not a deliverable.** Its output is a
generated move-list that Spec 014 §3 and Plan 027 are then rewritten from; the shipping implementation still
goes through the regenerated plan → round-3 audit → SDD, per CLAUDE.md.

*(If the derivation tree comes out clean, green and reviewable, promoting it directly instead of redoing the
work under SDD is a reasonable call — but it is **the user's call**, not an assumption this playbook makes.)*

### G.0 Where to work — read this first

The revised bundle and this audit record are **uncommitted in the primary working tree**. A `git worktree`
checks out a *commit*, so it would carry the **stale `ab233d9` docs** and a fresh session reading them would
drift straight back into the errors round 2 just catalogued.

```bash
git worktree add ../msgin-derive -b refactor/mechanical-derivation   # code is identical; no code has changed
```

> **RULE for the derivation session: read every design document from the PRIMARY tree**
> `/Users/zakyalvan/Documents/RND/msgin/docs/`. The worktree's `docs/` is stale — **do not read it, do not edit
> it**. Leave it untouched so `git diff --stat` stays clean for §G.4's inventory generation.

Alternative if worktrees are inconvenient: `cp -R` the repo into the scratchpad. That loses `git mv` history and
`git diff --stat`, which §G.4 uses — workable, but the worktree is better.

### G.1 Settle these before moving files

Round 2 left judgment calls the compiler cannot make. **Bold = recommended default.**

| # | Decision | Recommendation | Whose call |
|---|---|---|---|
| D-A | `ExponentialBackoff` placement (round-2 §A1) | **SETTLED 2026-07-27 (user).** Contract in root, implementation in `resilience` — see D-A1 below. | **settled** |
| D-B | `TopicPublisher`/`TopicSubscriber` (§A4) | **SETTLED 2026-07-27 (user): root** (`spi.go`), sixth declaration-level split | **settled** |
| D-C | `Subscription`'s home (§A3) | **SETTLED 2026-07-27: fold into root `channel.go`** beside `SubscribableChannel`; root stays 14 | **settled** |
| D-D | `cfg.optErr` (§B4) | **SETTLED 2026-07-27: delete the field and the `NewAggregator` guard in Task 1** — only `expr.go` writes it | **settled** |
| D-E | `WithReleaseStrategy` (§D1) | **SETTLED 2026-07-27 (user): `WithReleaseStrategy(ReleaseStrategy)`**; the bool-only sugar becomes **`WithReleaseWhen`** | **settled** |
| D-F | Reply-channel exclusivity (§D4) | **SETTLED 2026-07-27 (user): add `channel.WithSingleSubscriber()`** reusing `ErrChannelSubscribed` — an opt-in typed error beats godoc, per CLAUDE.md sensible-defaults | **settled** |
| D-G | Task 8 sizing (§C5) | **SETTLED 2026-07-27: split 8a** (`exchange` split, `gateway`, `activator`) **/ 8b** (`consumer`, `poller`, `producer`, `credit`, `flowcontrol` options, `attempts.go`) | **settled** |
| D-H | `Message` field access (§B1) | **Forced, not a choice**: rewrite over `NewMessage[T](payload, m.Headers())` + `m.Payload()`. **Never `New[T]`** — it re-stamps `msgin.message-id`/`msgin.timestamp` and no assertion would catch it | forced |

**All eight are settled as of 2026-07-27** — D-A/D-B/D-E/D-F by the user, D-C/D-D/D-G on the recommended
default, D-H by the type system. Round 1's six §H decisions stand unchanged. Consequences to fold into the
documents in §G.5:

- **D-B** — `pubsub_registry.go` is the **sixth split**: `TopicPublisher`/`TopicSubscriber` → root `spi.go`;
  `PubSub`, `NewPubSub`, `topicSubscription` → `channel`. Add both interfaces to Spec §4's SPI list, ADR 0027
  §4's table, and Task 6. A future NATS/Redis topic adapter then implements a **root** seam, as ADR 0014 and
  Spec 004 §105 file it.
- **D-E** — `WithReleaseStrategy(ReleaseStrategy)` (fallible, symmetric with `WithCorrelationStrategy`) and
  `WithReleaseWhen(func(MessageGroup) bool)` (bool-only sugar, wraps to `(bool, nil)`). This **renames** today's
  `WithReleaseStrategy` and **removes** the spec's proposed `WithRelease`; both are breaking, which is free
  (zero tags, zero consumers) but must appear in `MIGRATION.md` and the §4.1 apidiff expectations.
  `expr.Release` returns `routing.ReleaseStrategy`, so `WithReleaseStrategy(expr.Release(...))` now compiles.
- **D-F** — `channel.WithSingleSubscriber()` on `PublishSubscribeChannel`, **off by default** (no behavior
  change), returning the existing `ErrChannelSubscribed` on the second `Subscribe`. Rewrite ADR 0028 §6.2: its
  rebuttal targets a *global registry* and does not reach a channel-local opt-in. New exported surface → §4.1,
  and it needs its own hot-path branch tests (subscribe-first-ok / subscribe-second-error / option-off).

#### D-A1 — `ExponentialBackoff` → `resilience`; the edge is removed, not accepted (SETTLED, user, 2026-07-27)

**Decision: `BackoffStrategy` (the contract) stays in root; `ExponentialBackoff` + `jitter` (the implementation)
move to `resilience`.** The organising principle applies without exception, and future backoff strategies land
beside the first one rather than splitting the family across two packages post-v1.

**The `endpoint → resilience` edge that round-2 §A1 found is removed, not accepted.** It existed for exactly one
internal convenience call. `endpoint` declares its own bounded default instead:

```go
// endpoint/poller.go
//
// Deliberately NOT resilience.ExponentialBackoff: that type is public and
// unconstrained, so it carries float/NaN/overflow guards this call site cannot
// need. pollInterval is validated > 0 (ErrInvalidPollInterval, consumer.go:179)
// and the result is hard-capped at maxPollErrorBackoff (30s), so the doubling is
// bounded to at most six iterations. Keeping it local keeps endpoint free of any
// subpackage import.
func (c *consumer[T]) pollErrorBackoff(n int) time.Duration {
	d := c.pollInterval
	for i := 1; i < n && d < maxPollErrorBackoff; i++ {
		d *= 2
	}
	return min(d, maxPollErrorBackoff)
}
```

**Behavior-identical** to `ExponentialBackoff{Initial: pollInterval, Max: 30s, Mult: 2}.Delay(n-1)` on every
arm — `n<=0` → `pollInterval` (matching `Delay`'s `attempt<0` clamp); `n=1` → `pollInterval`; `n=2,3` → 2×, 4×;
large `n` → capped. `RandomizationFactor` is zero here, so no jitter path is involved. This **removes** float
arithmetic from the poll loop rather than duplicating it.

**A correction this decision rests on.** Round-2 §A1 and §C6 both fault the sentence *"`resilience` is imported
by no other package in the module"*. That sentence conflated two different claims:

- **"No core subpackage imports another core subpackage"** — the acyclicity invariant that actually matters, and
  which the local default above **preserves exactly**.
- **"Nothing at all imports `resilience`"** — over-broad. It swept in `adapter/database/sql` and
  `adapter/database/sql/harness`, which are *consumers* of the library, not peers of the core packages.
  `adapter/http` already imports `msgin`; an adapter importing `msgin/resilience` violates nothing.

So the three adapter change sites (`sql/source.go:175,235`, `harness/source.go:112`) are **expected mechanical
churn, not an architectural cost** — the earlier framing mispriced them.

**Consequences to fold in:**

- ADR 0027 §2's dependency claim becomes: *no core package (`endpoint`/`routing`/`transform`/`channel`/
  `resilience`) imports another; adapters and callers import `resilience` directly.* Scriptable, with **no
  allowlist entry**: `go list -deps ./endpoint ./routing ./transform ./channel ./resilience | grep msgin/` empty.
- `backoff.go` **remains a split** (five splits stand): `BackoffStrategy` root, `ExponentialBackoff`+`jitter` →
  `resilience`.
- Task 0's adapter inventory must list the three sites; `harness` is a separate module, so its `GOWORK=off`
  build is part of Task 7's gate.
- Callers write two imports for a retry policy —
  `msgin.RetryPolicy{Backoff: resilience.ExponentialBackoff{...}}`. Accepted; record it in `MIGRATION.md`.

**Filed as a follow-up, deliberately NOT in this window:** `pollErrorBackoff` is a hard-coded policy with no
`WithPollErrorBackoff` override, which CLAUDE.md's *Sensible defaults (opinionated, but overridable)* rule makes
a latent gap. It is **additive and non-breaking**, so it does not need the breaking window, and this increment
is declared behavior-preserving. Add it as its own increment rather than smuggling it in here.

### G.2 Install what is missing, then baseline

```bash
export GOTOOLCHAIN=go1.25.12
go install golang.org/x/exp/cmd/apidiff@latest golang.org/x/exp/cmd/gorelease@latest
export PATH="$(go env GOPATH)/bin:$PATH"      # gofumpt, goimports, gopls, govulncheck all live here

# Baselines — capture BEFORE any move
apidiff -w /tmp/msgin-root.api .
for d in . adapter/database/sql/{harness,postgres,mysql,sqlite,dbtest} adapter/cron/crontest; do
  (cd "$d" && GOWORK=off go test ./... -coverprofile=/tmp/cov-$(echo $d | tr / _).out) 
done
```

### G.3 The green gate — run this after every step, not per task

`go build ./...` is **not sufficient** and is how round-2 §B2 survived: it does not compile tests, and it cannot
see the six satellite modules.

```bash
go build ./...            # fast inner loop
go vet ./...              # compiles TEST binaries too — catches the `collector`/`order` class
for d in . adapter/database/sql/{harness,postgres,mysql,sqlite,dbtest} adapter/cron/crontest; do
  (cd "$d" && GOWORK=off go build ./... && GOWORK=off go vet ./...) || echo "RED: $d"
done
```

Full `-race` test run at each task boundary; Docker required for `dbtest`/`crontest`.

**Order:** follow Plan 027's task order (it is sound — only its *tables* are wrong), but let the compiler drive
each step rather than the table. Expect the adapter tree to break from Task 2; fix it in the same step.

### G.4 Generate the artifacts — do not hand-type any of these

At green, emit each table Spec 014 §3 needs:

```bash
# root file list + count            → §3.1
ls *.go | grep -v _test.go | tee /tmp/root-files.txt | wc -l

# per-package file tables           → §3.1
for p in endpoint routing transform channel resilience; do echo "== $p"; ls $p/*.go; done

# declaration-level split tables    → §3.2  (per original split file)
#   parse the ORIGINAL file's top-level decls, locate each in the new tree
grep -nE '^(func|type|const|var) ' <original>   # then: grep -rn "<decl>" . --include='*.go'

# unexported crossings              → §3.3  — the compiler already proved this set; record what you had to
#   inline/move/export, and note that FIELD ACCESS is part of it (§B1), not just declarations

# test-file placement + helpers     → §3.4  — `go vet ./...` proved it; record where each of the 45 landed
#   and every package-level identifier declared in one _test.go and used from another destination

# adapter change inventory          → Task 0
git diff --stat -- adapter/                                   # per-file counts, real
git diff --numstat -- adapter/ | awk '{print $3}' | sed 's|/[^/]*$||' | sort | uniq -c   # per module

# exported surface diff             → §4 closed contract + §4.1 additions
apidiff /tmp/msgin-root.api .

# coverage per package, before/after → §9.7
go test ./... -coverprofile=/tmp/after.out && go tool cover -func=/tmp/after.out
```

**Every number that lands in a document must come from one of these commands, pasted with its output.** That is
the entire point of the method change: two rounds failed on hand-typed totals.

### G.5 Rewrite the documents, then round 3

1. Rewrite **Spec 014 §3** (§3.1–§3.5) and **Plan 027** wholesale from §G.4's output.
2. Fold §G.1's decisions into ADR 0027 (D-A, D-B), ADR 0028 (D-F), ADR 0029 (D-D, D-E).
3. Fix the round-2 leftovers not covered by regeneration: §A5 (42 sentinels), §A6 (six-of-seven census),
   §C1–C4 (tooling, `expr` `replace`, the ledger definition, Task 3's grep scope), §D3 (multi-instance for
   `PubSub`/`attemptTracker`), §D5, §D7–D17, and `docs/specs/011-http-adapter.md:630`.
4. Clear every ROUND-2-FAILED banner only when its listed defects are actually gone.
5. **Round-3 audit**, same three lenses. The consistency lens should have little to find — the numbers will be
   generated, not typed.

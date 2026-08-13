# Derivation-run findings (round-2 §G)

Every entry is produced by a command whose output is pasted. Facts only; design calls are §G.1's D-A…D-H.

## Baseline (before any change)

```
$ GOWORK=off go test ./... -coverprofile=… && go tool cover -func | tail -1
total:                                          (statements)    91.9%
  root package github.com/kartaladev/msgin                      99.3%
  adapter/cron                                                  50.8%
  adapter/database/sql                                          93.7%
  adapter/http                                                  100.0%
  adapter/http/stdlib                                           100.0%
  adapter/memory                                                71.3%

$ ls *.go | grep -v _test.go | wc -l      → 32   (non-test root files)
$ ls *_test.go | wc -l                    → 45   (root test files — matches §3.4)
```

Per-module `GOWORK=off go build ./... && go vet ./...`: all **seven** modules GREEN at baseline.
`apidiff` baseline written to `/tmp/msgin-derive/root.api`.

### F0 — root top-level declaration census (AST-derived, non-test)

```
$ decls . | grep -v _test.go | awk -F'\t' '{print $5}' | sort | uniq -c
 245 exported
 138 unexported
$ … | awk -F'\t' '{print $3}' | sort | uniq -c
  30 const   108 func   119 method   84 type   42 var
```

### F1 — **42 error sentinels, not 89** · confirms round-2 §A5

```
$ decls . | awk -F'\t' '$3=="var" && $4 ~ /^Err/ {print $1}' | sort | uniq -c
  42 errors.go
```

All 42 are in `errors.go`; **no `Err*` var is declared in any other root file**. Spec 014 §3.2's
"All 89 error sentinels" is wrong in the count *and* implies a distribution that does not exist.

---

## Task 1 — delete `expr.go`, `expr_test.go`, `doc_composition.go`

Result: **green** (`go vet ./...` clean, `GOWORK=off go test ./... -race` all 6 root-module packages ok).
Root package coverage 99.3% → **99.2%**; workspace total 91.9% → **91.5%**.

### F2 — Task 1 orphans a test helper across a package boundary · **new, round-2 §B2's defect class**

Deleting `expr_test.go` left the **root test binary RED** — caught by `go vet`, invisible to `go build`:

```
$ go vet ./...
vet: ./gateway_test.go:142:34: undefined: collector
```

`collector` (a 6-line recording `MessageChannel`) was declared in `expr_test.go` and is used by
`gateway_test.go`, which §3.4 sends to **`endpoint`**. Verified it is the *only* such crossing:

```
$ for sym in exprOrder exprItem exprBatch collector runStep exprGroupItem sumAmountFn filterExprRun; do …
collector        -> gateway_test.go
```

**Resolution:** `collector` is re-declared in `gateway_test.go`, so it travels to `endpoint` with its only
user. **No plan task mentions this.** §3.4 must list test-only *identifiers*, not only test *files*.

### F3 — Task 1 deletes coverage of five aggregator branches; three are core, not expr · **HIGH, new**

`aggregator_test.go` held 10 references to the `*Expr` API. They split three ways — the plan treats them as
one undifferentiated group and loses the core three:

| Case | Branch | Verdict |
|---|---|---|
| M-1 empty group snapshot | `toGroupEnv` guard (declared in `expr.go`) | genuinely leaves with expr → Task 10 |
| M-6 non-`A` member → `ErrPayloadType` | `toGroupEnv` guard (declared in `expr.go`) | genuinely leaves with expr → Task 10 |
| **H-1 reaper fall-through** | `reapGroup`, **core** | **must survive** |
| **H-2 drain-loop residual release-check error** | `release`, **core** | **must survive** |
| **H-3 drain-loop residual `releaseOnce` failure** | `release`, **core** | **must survive** |

H-1/H-2/H-3 are reachable *only* when the release check can return an error. Before this window the sole
fallible release strategy was `WithReleaseExpr`; the bool-only `WithReleaseStrategy` wrapper never errors.
So **Task 1 removes the only driver for three core branches, and Plan 027 does not reintroduce a fallible
release strategy until Task 9** — a task-ordering defect that would show up as a silent coverage loss.

**Resolution taken:** D-E (`ReleaseStrategy`, fallible) was pulled forward into Task 1, and H-1/H-2/H-3 were
rewritten over a Go-func `requireQtyRelease(min) msgin.ReleaseStrategy` helper. Coverage preserved.

**For the regenerated plan: D-E is a Task 1 prerequisite, not Task 9 work.**

### F4 — `mixedTypeAddStore` / `mixedTypeGroup` become dead code and nothing catches it

Both fixtures existed only to drive M-6. After the deletion nothing references them, and **no tool reports
it**: `.golangci.yml` sets `linters.default: none`, so `unused` is off, and Go does not error on unused
package-level declarations. Removed by hand. `emptyGroupAddStore` **is** still live
(`aggregator_settlement_test.go:246`) — the two are not symmetric, despite looking it.

### F5 — D-D confirmed by the compiler: `optErr` had exactly one writer

After deleting `expr.go`, `grep -n optErr aggregator.go` returned only the two *read* sites
(`:256`, `:257`). Field + `NewAggregator` guard deleted per D-D; the two branches it guarded are
unreachable, not merely untested — round-2 §B4's reading is confirmed.

### F6 — `expr-lang` drops cleanly

`expr.go` was the only non-godoc importer. `go mod tidy` after the deletion removed
`github.com/expr-lang/expr v1.17.8` from `go.mod` with no other change; `clockwork` and `robfig/cron/v3`
remain. Root module dependency count goes 3 → 2.

### F7 — D-H confirmed: endpoint reads Message's unexported fields

The extracted `endpoint` package did not compile. `go build ./...` (root, `/Users/zakyalvan/Documents/RND/msgin-derive`)
before the fix:

```
# github.com/kartaladev/msgin/endpoint
endpoint/consumer.go:694:26: cannot refer to unexported field payload in struct literal of type msgin.Message[T]
endpoint/consumer.go:694:44: cannot refer to unexported field headers in struct literal of type msgin.Message[T]
endpoint/consumer.go:694:59: d.Msg.headers undefined (type msgin.Message[any] has no field or method headers, but does have method Headers)
endpoint/consumer.go:828:14: m.payload undefined (type msgin.Message[any] has no field or method payload, but does have method Payload)
endpoint/consumer.go:835:13: m.payload undefined (type msgin.Message[any] has no field or method payload, but does have method Payload)
endpoint/producer.go:417:29: cannot refer to unexported field payload in struct literal of type msgin.Message[any]
endpoint/producer.go:417:42: msg.payload undefined (type msgin.Message[T] has no field or method payload, but does have method Payload)
endpoint/producer.go:417:51: cannot refer to unexported field headers in struct literal of type msgin.Message[any]
endpoint/producer.go:417:64: msg.headers undefined (type msgin.Message[T] has no field or method headers, but does have method Headers)
endpoint/producer.go:419:31: msg.payload undefined (type msgin.Message[T] has no field or method payload, but does have method Payload)
endpoint/producer.go:419:31: too many errors
```

The compiler truncated at "too many errors". The **full** site list is **6 lines, not 4** — `grep -rn '\.payload\|\.headers' endpoint/`
before the fix:

```
endpoint/producer.go:417:		return msgin.Message[any]{payload: msg.payload, headers: msg.headers}, nil
endpoint/producer.go:419:	b, err := p.codec.Encode(msg.payload)
endpoint/producer.go:423:	return msgin.Message[any]{payload: any(b), headers: msg.headers}, nil
endpoint/consumer.go:694:	msg := msgin.Message[T]{payload: payload, headers: d.Msg.headers}
endpoint/consumer.go:828:		v, ok := m.payload.(T)
endpoint/consumer.go:835:	b, ok := m.payload.([]byte)
```

`producer.go:423` was **never surfaced by the compiler** (suppressed by the 10-error cap) — a plan that
enumerated sites from a single build's stderr would have missed it and Task N+1 would have re-broken the
build. Enumerate with `grep`, then confirm with the compiler; not the reverse.

All 6 rewrote over the public API with no new exported symbol required — `NewMessage[T](payload, Headers)`
is literally `Message[T]{payload: payload, headers: headers}` (message.go:184-186), and `Headers` is a
struct wrapping a map, so passing `m.Headers()` aliases the same map the struct literal did. Identity
(`msgin.message-id` / `msgin.timestamp`) is preserved bit-for-bit. `msgin.New[` appears nowhere in
`endpoint/` (`grep -rn 'msgin\.New\[' endpoint/` → exit 1).

**D-H costs nothing structurally.** The `endpoint→root` edge needed no widening; the exported Message
surface (`Payload()`, `Headers()`, `NewMessage`) was already sufficient. Post-fix: `go build ./...` exit 0,
`gofmt -l endpoint/` empty.

---

## Task: place the root `_test.go` files

Method: derive the symbol→package map with `decls`, census every root test file's references against it,
place each file at its SUT, then rewrite via **AST-derived byte splices** (a `requalify` tool built for this
run) so comments and string literals are provably untouched. Result: **green**.

### F8 — test-file placement, derived

#### F8.0 — the file count is **44, not 45**

```
$ ls *_test.go | wc -l          # before any move
      44
```

Spec 014 §3.4 and this run's own baseline both say **45**. The baseline was correct *at baseline*; **Task 1
deleted `expr_test.go`**, so every downstream table that still says 45 is stale by one. The six placement
buckets total **44**.

#### F8.1 — symbol→destination map (AST-derived, non-test decls only)

```
$ for p in endpoint routing transform channel resilience; do
    decls $p | grep -v '_test\.go' | awk -F'\t' '$5=="exported" && $3!="method" {print $4}' | sort -u | wc -l
  done
endpoint 45   routing 21   transform 1   channel 14   resilience 9      (= 90 moved symbols)
$ decls . | grep -v '_test\.go' | awk -F'\t' '$5=="exported" && $3!="method" {print $4}' | sort -u | wc -l
     100                                                                  (stay `msgin.`)

$ # duplicates across destination packages:
(none)
$ # names exported by BOTH a destination package and root:
(none)
```

**The rewrite is unambiguous** — no exported name is claimed by two packages, so `msgin.X → <dest>.X` is a
total function. This is a load-bearing precondition the plan never states; had it failed, the move would
have needed per-file disambiguation.

#### F8.2 — placement census (from the green tree)

Columns are **reference counts by package**, after placement. "SUT symbols" are the top-3 most-referenced
symbols of the destination package — the evidence that decided the file.

| test file | destination | endpoint | routing | transform | channel | resilience | msgin (root) | SUT symbols that decided placement |
|---|---|--:|--:|--:|--:|--:|--:|---|
| `codec_test.go` | **root** | 0 | 0 | 0 | 0 | 0 | 3 | `msgin.JSONPayloadCodec` `msgin.BytesPayloadCodec` |
| `errors_test.go` | **root** | 0 | 0 | 0 | 0 | 0 | 13 | `msgin.ErrUnsupportedSource` `msgin.ErrUnexpectedCodec` `msgin.ErrPayloadType` |
| `example_composition_test.go` | **root** | 2 | 1 | 1 | 0 | 0 | 11 | `msgin.Message` `msgin.WithPayload` `msgin.New` |
| `groupstore_conformance_test.go` | **root** | 0 | 0 | 0 | 0 | 0 | 19 | `msgin.MessageGroupStore` `msgin.WithID` `msgin.New` |
| `handler_test.go` | **root** | 0 | 0 | 0 | 0 | 0 | 30 | `msgin.Step` `msgin.New` `msgin.MessageHandler` |
| `message_test.go` | **root** | 0 | 0 | 0 | 0 | 0 | 83 | `msgin.Message` `msgin.Headers` `msgin.HeaderTimestamp` |
| `payload_test.go` | **root** | 0 | 0 | 0 | 0 | 0 | 18 | `msgin.PayloadOf` `msgin.New` `msgin.WithID` |
| `reliability_test.go` | **root** | 0 | 0 | 0 | 0 | 0 | 4 | `msgin.ErrPayloadTooLarge` `msgin.RetryAfter` `msgin.Permanent` |
| `retry_test.go` | **root** | 0 | 0 | 0 | 0 | 1 | 9 | `msgin.RetryPolicy` `msgin.Message` `msgin.ErrNoDeadLetter` |
| `spi_test.go` | **root** | 0 | 0 | 0 | 0 | 0 | 7 | `msgin.Delivery` `msgin.StreamingSource` `msgin.OutboundAdapter` |
| `channel_test.go` | channel | 0 | 0 | 0 | 4 | 0 | 15 | `channel.NewDirectChannel` |
| `example_pubsub_test.go` | channel | 0 | 0 | 0 | 1 | 0 | 7 | `channel.NewPubSub` |
| `pubsub_integration_test.go` | channel | 2 | 0 | 0 | 3 | 0 | 14 | `channel.NewPubSub` `channel.NewPublishSubscribeChannel` |
| `pubsub_registry_test.go` | channel | 0 | 0 | 0 | 13 | 0 | 16 | `channel.PubSub` `channel.NewPubSub` |
| `pubsub_test.go` | channel | 0 | 0 | 0 | 18 | 0 | 25 | `channel.NewPublishSubscribeChannel` `channel.WithFanOut` `channel.FanOutBestEffort` |
| `queuechannel_e2e_test.go` | channel | 3 | 0 | 0 | 1 | 0 | 2 | `channel.NewQueueChannel` |
| `queuechannel_test.go` | channel | 0 | 0 | 0 | 8 | 0 | 12 | `channel.NewQueueChannel` |
| `activator_test.go` | endpoint | 4 | 0 | 0 | 0 | 0 | 57 | `endpoint.Consume` `endpoint.Activate` |
| `composition_integration_test.go` | endpoint | 3 | 1 | 1 | 0 | 0 | 8 | `endpoint.WithShutdownTimeout` `endpoint.NewConsumer` `endpoint.Consume` |
| `consumer_governor_panic_test.go` | endpoint | 18 | 0 | 0 | 0 | 0 | 12 | `endpoint.WithLogger` `endpoint.NewConsumer` `endpoint.WithCircuitBreaker` |
| `consumer_probegate_wiring_test.go` | endpoint | 19 | 0 | 0 | 0 | 1 | 3 | `endpoint.ConsumerOption` `endpoint.WithCircuitBreaker` `endpoint.WithLogger` |
| `consumer_test.go` | endpoint | 172 | 0 | 0 | 0 | 22 | 199 | `endpoint.NewConsumer` `endpoint.WithConsumerClock` `endpoint.ConsumerOption` |
| `example_flowcontrol_test.go` | endpoint | 6 | 0 | 0 | 0 | 2 | 2 | `endpoint.WithRateLimit` `endpoint.WithOverflow` `endpoint.WithMaxInFlight` |
| `example_reliability_test.go` | endpoint | 6 | 0 | 0 | 0 | 1 | 10 | `endpoint.WithProducerRetry` `endpoint.WithProducerCodec` `endpoint.WithInvalidMessageSink` |
| `example_scheduled_test.go` | endpoint | 2 | 0 | 0 | 0 | 0 | 6 | `endpoint.NewProducer` |
| `exchange_test.go` | endpoint | 53 | 0 | 0 | 38 | 0 | 148 | `endpoint.NewChannelExchange` `endpoint.WithUnmatchedReplySink` `endpoint.ChannelExchange` |
| `flowcontrol_test.go` | endpoint | 34 | 0 | 0 | 0 | 0 | 16 | `endpoint.ConsumerOption` `endpoint.WithPollMaxBatch` `endpoint.WithPollInterval` |
| `gateway_test.go` | endpoint | 12 | 0 | 0 | 4 | 0 | 109 | `endpoint.NewGateway` `endpoint.OutboundGateway` `endpoint.NewChannelExchange` |
| `poller_test.go` | endpoint | 47 | 0 | 0 | 0 | 0 | 31 | `endpoint.NewConsumer` `endpoint.WithMaxInFlight` `endpoint.WithPollMaxBatch` |
| `producer_retry_test.go` | endpoint | 87 | 0 | 0 | 0 | 10 | 105 | `endpoint.ProducerOption` `endpoint.Producer` `endpoint.WithProducerRetry` |
| `producer_scheduled_test.go` | endpoint | 11 | 0 | 0 | 0 | 0 | 13 | `endpoint.Producer` `endpoint.NewProducer` `endpoint.WithProducerCodec` |
| `producer_test.go` | endpoint | 16 | 0 | 0 | 0 | 0 | 7 | `endpoint.Producer` `endpoint.NewProducer` `endpoint.WithProducerCodec` |
| `settlement_doubles_test.go` | endpoint | 0 | 0 | 0 | 0 | 0 | 38 |  |
| `backoff_test.go` | resilience | 0 | 0 | 0 | 0 | 15 | 1 | `resilience.ExponentialBackoff` |
| `breaker_test.go` | resilience | 0 | 0 | 0 | 0 | 20 | 3 | `resilience.WithBreakerThreshold` `resilience.WithBreakerCooldown` `resilience.WithBreakerClock` |
| `ratelimit_test.go` | resilience | 0 | 0 | 0 | 0 | 7 | 2 | `resilience.NewTokenBucket` `resilience.WithTokenBucketClock` |
| `aggregator_settlement_test.go` | routing | 0 | 30 | 0 | 0 | 0 | 36 | `routing.NewAggregator` `routing.WithOutputChannel` `routing.WithCompletionSize` |
| `aggregator_test.go` | routing | 0 | 132 | 0 | 0 | 0 | 98 | `routing.NewAggregator` `routing.WithOutputChannel` `routing.Aggregator` |
| `example_aggregator_test.go` | routing | 0 | 3 | 0 | 1 | 0 | 9 | `routing.WithOutputChannel` `routing.WithCompletionSize` `routing.NewAggregator` |
| `example_splitter_test.go` | routing | 0 | 1 | 0 | 0 | 0 | 9 | `routing.Split` |
| `filter_test.go` | routing | 0 | 5 | 0 | 1 | 0 | 18 | `routing.Filter` `routing.WithDiscardChannel` `routing.FilterOption` |
| `router_test.go` | routing | 0 | 4 | 0 | 2 | 0 | 33 | `routing.NewRouter` `routing.WithDefaultChannel` `routing.RouterOption` |
| `splitter_test.go` | routing | 0 | 4 | 0 | 0 | 0 | 83 | `routing.Split` |
| `transformer_test.go` | transform | 0 | 0 | 2 | 0 | 0 | 36 | `transform.Transform` |

```
$ ls *_test.go | wc -l ; for d in channel endpoint resilience routing transform; do ls $d/*_test.go | wc -l; done
root 10   channel 7   endpoint 16   resilience 3   routing 7   transform 1     TOTAL 44
```

**Zero files were split.** Every file has exactly one SUT; the multi-package rows (`exchange_test.go`
53 endpoint / 38 channel, `queuechannel_e2e_test.go` 3 endpoint / 1 channel) are the placement rule working
as designed — a `package X_test` binary imports whatever it needs.

#### F8.3 — **§3.4's "`retry_test.go` is the only SPLIT" is a fabrication** · HIGH

`retry_test.go` has never contained an `ExponentialBackoff` delay case. Those cases have always lived in a
*separate file*, `backoff_test.go`:

```
$ git show HEAD:retry_test.go | grep -nE '^func|^type'
13:type nopSink struct{}
15:func (nopSink) Send(context.Context, msgin.Message[any]) error { return nil }
17:func TestRetryPolicy_Validate(t *testing.T)

$ git show HEAD:backoff_test.go | grep -nE '^func|^type'
13:func TestExponentialBackoff_Delay(t *testing.T)
127:func TestExponentialBackoff_JitterLowSideClamp(t *testing.T)
136:func TestExponentialBackoff_SatisfiesInterface(t *testing.T)
```

So the prescribed surgery — "the `ExponentialBackoff` delay cases move into `resilience/backoff_test.go`" —
has no source to cut from. The correct, mechanical outcome is **two whole-file moves and no split at all**:
`backoff_test.go` → `resilience` (SUT `resilience.ExponentialBackoff`, 15 refs) and `retry_test.go` stays in
**root** (SUT `msgin.RetryPolicy.Validate`; its single `resilience.` reference is a *fixture value* on line
27, not a SUT).

> **CORRECTION (coordinator, verified against the spec).** The paragraph originally continued: *"`backoff_test.go`
> does not appear in §3.4 at all as a resilience-bound file … §3.4 both invents a split and omits the file that
> actually moves."* **The omission half is FALSE.** `docs/specs/014-core-package-layout.md:247` reads:
>
> ```
> | **resilience** | `backoff_test.go`, `breaker_test.go`, `ratelimit_test.go` | 3 |
> ```
>
> §3.4 lists `backoff_test.go` correctly. Only the **case-level split** claim (`:252`, `:268-270`) is false.
> Recording this because an unverified assertion inside a findings file is the exact defect this run exists to
> eliminate — the claim was corrected the same way every other number here was reached: by running the command.

**What survives: the case-level split is a fabrication.** Verified against the pre-move tree:

```
$ git show HEAD:retry_test.go | grep -n "ExponentialBackoff"
26:  msgin.RetryPolicy{MaxAttempts: 3, DeadLetter: nopSink{}, Backoff: msgin.ExponentialBackoff{Initial: time.Millisecond}},
$ git show HEAD:backoff_test.go | grep -c "Delay("
4
```

`retry_test.go`'s single reference is a **fixture value** inside a `RetryPolicy` literal driving `Validate`,
not a delay-computation case; all four `Delay()` cases have always lived in `backoff_test.go`. So §3.4's
*"One of the 45 is additionally split at the case level"* has no source to cut from, and the correct outcome
is **zero splits**.

**This CONFIRMS round-2 §A7** (*"`retry_test.go` does not need splitting; §3.4's split accounting is
inverted"* — MED, CONVERGENT 2/3). It is corroboration of a known finding, not a new one.

**On the file count.** 45 is correct as the *baseline inventory* (`ls *_test.go | wc -l` → 45 before any
change, measured in this run's own baseline), and §3.4's table legitimately totals 45 by counting
`expr_test.go` under "deleted → `expr` module". **44** is the number *placed*. Both numbers are right in
their own frame; the regenerated §3.4 should state which frame it means rather than change the total.

#### F8.4 — cross-file test-identifier inventory (Trap 1) · §3.4 lacks this entirely

`go build` cannot see this class of breakage; only `go vet` / `go test` compiles test binaries. Derived at
the **AST level** — `grep -w` over-reports badly, because `settle`, `order`, and `backlog` are ordinary
English words that appear in comments (`grep -w` claimed `settle` was used by `aggregator_test.go`,
`groupstore_conformance_test.go` and `spi_test.go`; the AST says none of the three touch it).

92 package-level identifiers are declared across the root `_test.go` files. 35 are referenced by more than
one file:

| identifier(s) | declared in → dest | users → dest | crossing? | resolution |
|---|---|---|---|---|
| `blockingGroupStore` | `aggregator_settlement_test.go` → routing | `aggregator_test.go` → routing | no | none needed |
| `fakeAggChannel`, `sumFn`, `corrMsg`, `newIntStore` | `aggregator_test.go` → routing | `aggregator_settlement_test.go` → routing | no | none needed |
| `runConsumer` | `consumer_test.go` → endpoint | `poller_test.go` → endpoint | no | none needed |
| `outboundFunc` | `producer_retry_test.go` → endpoint | `example_reliability_test.go` → endpoint | no | none needed |
| `errEncode`, `failingCodec` | `producer_test.go` → endpoint | `producer_scheduled_test.go` → endpoint | no | none needed |
| `lockedBuffer`, `recordingSink`, `settle`, `scriptedSource`, `newSettleDelivery`, `reemittingSource`, `newControllableSource`, `byteStreamSource`, `nativeScriptedSource`, `nativeDLQSource`, `scriptedBreaker`, `newScriptedBreaker`, `countingSource`, `finiteSource`, `panicRateLimiter`, `panicAllowBreaker`, `panicProbeGateBreaker`, `panicRecordBreaker`, `newPanicHalfOpenBreaker`, `newSignalingSource`, `panicDecodeCodec`, `panicSendSink`, `panicSettle`, `hookRec`, `newHookRec` (25) | `settlement_doubles_test.go` → endpoint | `consumer_test.go`, `consumer_governor_panic_test.go`, `consumer_probegate_wiring_test.go`, `poller_test.go` — **all** → endpoint | no | none needed — **this is why §3.4's "`settlement_doubles_test.go` → endpoint, whole, not split" is correct**, and the census proves it rather than asserting it |
| **`order`** | **`codec_test.go` → root** | `consumer_test.go`, `consumer_governor_panic_test.go`, `consumer_probegate_wiring_test.go`, `flowcontrol_test.go`, `poller_test.go`, `producer_scheduled_test.go`, `producer_test.go`, `settlement_doubles_test.go` — **all 8 → endpoint** | **YES** | **duplicated** into `endpoint/settlement_doubles_test.go` (7-line struct + rationale comment). Test doubles; duplication is correct. |

**Exactly one identifier crosses a destination boundary** (`order`), plus **`collector`**, already resolved
in F2. Both were invisible to `go build`. The invariant to encode in the plan is not the list but the check:

```
# no package-level test identifier may have a user in a different destination package
$ awk -F'\t' '…' placement.tsv testhelpers.tsv idents.tsv
=== end. (no lines = all resolved)
```

#### F8.5 — placements that differ from Spec 014 §3.4

All six placements §3.4 pre-settled are **confirmed** by the census (`settlement_doubles_test.go`→endpoint,
`example_composition_test.go`→root, `composition_integration_test.go`→endpoint,
`groupstore_conformance_test.go`→root, `flowcontrol_test.go`→endpoint). The **seventh — the `retry_test.go`
split — is not performable** (F8.3). Beyond that, three placements are worth calling out because they are
the ones a hand-written table gets wrong:

| file | naive guess | derived | why |
|---|---|---|---|
| `handler_test.go` | `endpoint` (`endpoint` exports a `Handler` type) | **root** | Its SUT is `msgin.Chain` / `msgin.To` / `msgin.Step`, all still in root `handler.go`. `endpoint.Handler` is an unrelated generic type; the name collision is a trap. |
| `queuechannel_e2e_test.go` | `endpoint` (3 endpoint refs vs 1 channel ref) | **channel** | Reference *count* is not the SUT. `TestQueueChannel_EndToEnd` drives `channel.NewQueueChannel`; `NewProducer`/`NewConsumer` are the harness. **Counting references would place this file wrong.** |
| `pubsub_integration_test.go` | `endpoint` | **channel** | Same shape: `NewConsumer` is the driver, `channel.PubSub` is the SUT. |

#### F8.6 — verification (in-scope tree)

```
$ gofmt -l . endpoint routing transform channel resilience
(empty)
$ go build ./...
(exit 0)
$ go vet . ./channel ./endpoint ./resilience ./routing ./transform
(exit 0)                      # compiles all six TEST binaries
$ GOWORK=off go test . ./channel ./endpoint ./resilience ./routing ./transform -race -shuffle=on
ok  	github.com/kartaladev/msgin	1.211s
ok  	github.com/kartaladev/msgin/channel	1.351s
ok  	github.com/kartaladev/msgin/endpoint	2.825s
ok  	github.com/kartaladev/msgin/resilience	1.598s
ok  	github.com/kartaladev/msgin/routing	2.016s
ok  	github.com/kartaladev/msgin/transform	1.848s
```

**Behavior-identity proved, not asserted.** 211 `Test*`/`Example*` functions before the move, 211 after,
**identical name sets** (`diff` empty). Diffing every moved file against the pre-move snapshot, normalised
for the package clause, the import block, and the `msgin.X → <pkg>.X` requalification, yields **exactly one
difference across all 44 files** — the deliberate 8-line `order` duplication in F8.4. No assertion, test
name, or case changed.

#### F8.7 — residual: `adapter/` still references moved symbols (out of scope, enumerated for the next task)

`go vet ./...` is **still red**, in `adapter/` only. Per Trap 2 this is enumerated with `grep`+AST, not from
the compiler's truncated stderr — the compiler names only **4** failing packages and **1 line each**:

```
$ go vet ./...
vet: adapter/cron/consumer_integration_test.go:58:18:  undefined: msgin.NewConsumer
vet: adapter/http/classify_test.go:95:21:              undefined: msgin.NewProducer
vet: adapter/database/sql/runner_test.go:16:73:        undefined: msgin.Consumer
vet: adapter/http/stdlib/inbound_test.go:28:14:        undefined: msgin.NewDirectChannel
```

The real set is **28 files**, split by whether the reference is code or godoc:

**14 files with real code references (build-breaking), 130 selectors total** — with the imports each needs:

| file | selectors | needs |
|---|--:|---|
| `adapter/cron/consumer_integration_test.go` | 1 | endpoint |
| `adapter/database/sql/runner_test.go` | 1 | endpoint |
| `adapter/database/sql/harness/harness.go` | 1 | endpoint |
| `adapter/database/sql/harness/groupstore.go` | 6 | channel, routing |
| `adapter/database/sql/harness/lock.go` | 28 | endpoint |
| `adapter/database/sql/harness/outbound.go` | 18 | endpoint |
| `adapter/database/sql/harness/source.go` | 16 | endpoint |
| `adapter/database/sql/postgres/example_sql_groupstore_test.go` | 5 | channel, routing |
| `adapter/http/classify_test.go` | 8 | endpoint |
| `adapter/http/exchange_test.go` | 5 | endpoint |
| `adapter/http/inbound_test.go` | 9 | channel, endpoint |
| `adapter/http/outbound_test.go` | 6 | endpoint |
| `adapter/http/sse_e2e_test.go` | 2 | endpoint |
| `adapter/http/stdlib/inbound_test.go` | 9 | channel, endpoint |

**14 files where the mention is godoc-comment-only** — *not* build-breaking, but stale documentation that no
compiler will ever flag, so a task that stops at "green" leaves them wrong:
`adapter/cron/example_test.go`, `adapter/database/sql/{errors,options,outbound,queuestore,source}.go`,
`adapter/database/sql/harness/queuestore.go`, `adapter/database/sql/outbound_test.go`,
`adapter/http/{doc,exchange,inbound,options,outbound}.go`, `adapter/memory/queuestore.go`.

Satellite-module gate after this task:

```
$ for d in …; do (cd $d && GOWORK=off go build ./... && GOWORK=off go vet ./...); done
RED:   adapter/database/sql/harness       # 5 files above
RED:   adapter/database/sql/postgres      # 1 file above
RED:   adapter/database/sql/dbtest        # transitively, via harness
GREEN: adapter/database/sql/mysql
GREEN: adapter/database/sql/sqlite
GREEN: adapter/cron/crontest
```

Note `adapter/database/sql/harness` is a **non-test module whose non-test files** (`lock.go`,
`outbound.go`, `source.go`, `groupstore.go`, `harness.go`) reference `endpoint`/`routing`/`channel` — the
adapter task is therefore **not** a test-only rewrite, and it adds `harness → endpoint` and
`harness → routing` module edges that the dependency-direction invariant should be checked against.

---

## Task: requalify `adapter/` + the six satellite modules

Method: identical to F8 — derive the change set by **grep against an AST-derived symbol→destination map**,
never from compiler stderr (F7). Rewrite code selectors with the AST-based `requalify`, rewrite
godoc-comment mentions with a new AST-based `adaptscan` (comments and string literals classified
separately, so neither tool can corrupt the other's territory). Result: **all seven modules GREEN**.

### F9 — adapter and satellite-module blast radius, measured

#### F9.0 — the symbol→destination map is still a total function (re-derived, not assumed)

```
$ for p in endpoint routing transform channel resilience; do
    decls $p | grep -v '_test\.go' | awk -F'\t' -v P=$p '$5=="exported" && $3!="method" {print P"\t"$4}'
  done | sort -u -k2,2 > symmap.tsv
$ wc -l < symmap.tsv
      90
$ cut -f1 symmap.tsv | sort | uniq -c
  14 channel   45 endpoint    9 resilience   21 routing    1 transform
$ cut -f2 symmap.tsv | sort | uniq -d          # a name claimed by two destination packages
(none)
$ comm -12 root-exported.txt <(cut -f2 symmap.tsv | sort -u)   # a name exported by root AND a destination
(none)
```

The root module still exports **100** non-method symbols. `msgin.X → <dest>.X` therefore remains
unambiguous over the adapter tree, exactly as F8.1 established for the root test files.

#### F9.1 — the local import name is `msgin` in **every** adapter file (precondition, verified)

A textual `msgin.X` scan is only sound if no file binds the root module to a different identifier:

```
$ grep -rn 'kartaladev/msgin"' --include='*.go' adapter/ \
    | grep -v '^\S*:[0-9]*:\s*\(msgin \)\?"github.com/kartaladev/msgin"$' | head -20
(no output — all 59 import specs are either the bare path or the explicit alias `msgin`)
```

#### F9.2 — **28 files, 154 textual occurrences: 115 CODE + 39 COMMENT, 0 STRING**

`adaptscan` classifies every `msgin.<Sym>` occurrence against the AST: CODE = an `*ast.SelectorExpr`
with `X` = ident `msgin`; COMMENT = inside an `*ast.CommentGroup`; STRING = inside a `token.STRING`
literal; OTHER = neither.

```
$ cut -f1 occ.tsv | sort -u | xargs adaptscan -map symmap-sp.tsv > adapt-classify.tsv
$ cut -f3 adapt-classify.tsv | sort | uniq -c
 115 CODE
  39 COMMENT
$ cut -f1 occ.tsv | sort -u | wc -l
      28
```

**No occurrence lives in a string literal, and none is unclassifiable** — the two rewrite passes are
provably exhaustive and non-overlapping.

**Correction to the prior enumeration.** The residual note in F8.7 reported "**130 selectors**" for the
14 code files. The measured number is **115**; F8.7's own per-file column sums to 115 as well
(`1+1+1+6+28+18+16+5+8+5+9+6+2+9 = 115`), so the headline 130 was a hand-typed total over a correct
table — the same defect class this run exists to eliminate. Everything else in F8.7 (the 28-file set, the
14/14 code/godoc split, the per-file counts, the RED/GREEN satellite verdict) is **confirmed exactly**.

#### F9.3 — per-file inventory (`code refs` vs `godoc-only refs`, and the imports each needs)

| file | CODE selectors | COMMENT mentions | destination packages imported |
|---|--:|--:|---|
| `adapter/cron/consumer_integration_test.go` | 1 | 1 | endpoint(1) |
| `adapter/cron/example_test.go` | 0 | 1 | — |
| `adapter/database/sql/errors.go` | 0 | 1 | — |
| `adapter/database/sql/harness/groupstore.go` | 6 | 1 | routing(4), channel(2) |
| `adapter/database/sql/harness/harness.go` | 1 | 0 | endpoint(1) |
| `adapter/database/sql/harness/lock.go` | 28 | 0 | endpoint(28) |
| `adapter/database/sql/harness/outbound.go` | 18 | 1 | endpoint(18) |
| `adapter/database/sql/harness/queuestore.go` | 0 | 1 | — |
| `adapter/database/sql/harness/source.go` | 16 | 0 | endpoint(16) |
| `adapter/database/sql/options.go` | 0 | 1 | — |
| `adapter/database/sql/outbound_test.go` | 0 | 1 | — |
| `adapter/database/sql/outbound.go` | 0 | 5 | — |
| `adapter/database/sql/postgres/example_sql_groupstore_test.go` | 5 | 0 | channel(1), routing(4) |
| `adapter/database/sql/queuestore.go` | 0 | 1 | — |
| `adapter/database/sql/runner_test.go` | 1 | 0 | endpoint(1) |
| `adapter/database/sql/source.go` | 0 | 1 | — |
| `adapter/http/classify_test.go` | 8 | 1 | endpoint(8) |
| `adapter/http/doc.go` | 0 | 2 | — |
| `adapter/http/exchange_test.go` | 5 | 4 | endpoint(5) |
| `adapter/http/exchange.go` | 0 | 5 | — |
| `adapter/http/inbound_test.go` | 9 | 1 | endpoint(2), channel(7) |
| `adapter/http/inbound.go` | 0 | 3 | — |
| `adapter/http/options.go` | 0 | 2 | — |
| `adapter/http/outbound_test.go` | 6 | 3 | endpoint(6) |
| `adapter/http/outbound.go` | 0 | 2 | — |
| `adapter/http/sse_e2e_test.go` | 2 | 0 | endpoint(2) |
| `adapter/http/stdlib/inbound_test.go` | 9 | 0 | channel(6), endpoint(3) |
| `adapter/memory/queuestore.go` | 0 | 1 | — |

Totals: **115 CODE**, **39 COMMENT**, across **28** files.
**14 files carry code references** (build-breaking); **14 files are godoc-comment-only** (not
build-breaking — no compiler will ever flag them, so a task that stops at "green" leaves them wrong):
`adapter/cron/example_test.go`, `adapter/database/sql/{errors,options,outbound,queuestore,source}.go`,
`adapter/database/sql/outbound_test.go`, `adapter/database/sql/harness/queuestore.go`,
`adapter/http/{doc,exchange,inbound,options,outbound}.go`, `adapter/memory/queuestore.go`.
The remaining 12 comment mentions sit inside files that also have code references.

#### F9.4 — the rewrite is *only* a requalification: proved by normalized diff, not asserted

The whole `adapter/` tree was snapshotted before the pass. Normalizing both sides by (a) deleting an
import line for a destination package and (b) collapsing `<dest>. → msgin.`, the two trees must be
identical if and only if nothing but requalification happened:

```
$ norm() { perl -pe 's{^\s*"github\.com/kartaladev/msgin/(endpoint|routing|channel|transform|resilience)"\s*$}{};
                     s{\b(endpoint|routing|channel|transform|resilience)\.}{msgin.}g' "$1" | grep -v '^[[:space:]]*$'; }
$ for f in $(cut -f1 occ.tsv | sort -u); do diff <(norm adapter-pre/$f) <(norm $f); done
### RESIDUAL DIFF in adapter/database/sql/runner_test.go
6d5
<       msgin "github.com/kartaladev/msgin"
files with non-requalification differences: 1  (of 28 touched)
```

**One** file differs beyond requalification, and correctly so: `runner_test.go`'s only msgin reference was
`msgin.Consumer`, so after the rewrite the root import is unused and `goimports` removed it. **No
assertion, test name, table case, or behavior changed in any of the 28 files.**

#### F9.5 — an article-agreement artifact the mechanical rewrite introduces (7 sites, fixed)

`msgin` begins with a consonant, `endpoint` with a vowel, so every prose comment reading "a msgin.Producer"
became ungrammatical:

```
$ grep -rn --include='*.go' -E '\b[Aa] (endpoint|routing|transform)\.' adapter/
adapter/database/sql/outbound.go:29     adapter/http/outbound_test.go:350,733,814
adapter/http/exchange.go:32             adapter/http/doc.go:75
adapter/http/classify_test.go:28                                        (7 sites)
```

Fixed to `an` in all 7. **This is a class, not seven instances**: any future `msgin.X → endpoint.X`
comment rewrite reintroduces it, so the check belongs in the plan as a grep, not as a list of files.

A second, related check came back clean — **no godoc doc-link form** (`[endpoint.Foo]`) was created, which
would have rendered as a broken link in the 14 files that do not import the destination package:

```
$ grep -rn --include='*.go' -E '\[(endpoint|routing|channel|transform|resilience)\.[A-Z]' adapter/
(no output)
```

#### F9.6 — **no new module-level dependency edge; four new package-level edges out of `harness`**

`harness`'s breakage is in **non-test** files (`lock.go`, `outbound.go`, `source.go`, `groupstore.go`,
`harness.go`), so this was not a test-only rewrite. But every satellite already `require`s **and**
`replace`s the root module, and `endpoint`/`routing`/`channel`/`resilience` are *packages inside that same
module* — so **no `go.mod` needed a single edit**:

```
$ git status --short -- 'adapter/**/go.mod' 'adapter/**/go.sum'
(no output)
$ git diff --stat -- 'adapter/**/go.mod' 'adapter/**/go.sum'
(no output)
```

The **package**-level edges (`go list`, including test imports) that now exist from the adapter tree into
the core subpackages — these are the ones to record, per the task brief:

| adapter package | edge kind | → core subpackage |
|---|---|---|
| `adapter/cron` | XTEST | `endpoint` |
| `adapter/database/sql` | **NONTEST** | `resilience` |
| `adapter/database/sql` | XTEST | `endpoint` |
| **`adapter/database/sql/harness`** | **NONTEST** | `channel`, `endpoint`, `resilience`, `routing` |
| `adapter/database/sql/postgres` | XTEST | `channel`, `routing` |
| `adapter/http` | XTEST | `channel`, `endpoint`, `resilience` |
| `adapter/http/stdlib` | XTEST | `channel`, `endpoint` |

Only **two** packages gain a **non-test** edge: `adapter/database/sql → resilience` (pre-existing from the
`resilience` extraction) and **`adapter/database/sql/harness → {channel, endpoint, resilience, routing}`**
(new here). Per D-A this violates nothing — the invariant is that no *core* package imports another core
package — and that invariant still holds:

```
$ for p in . ./endpoint ./routing ./transform ./channel ./resilience; do
    echo "$(go list -f '{{.ImportPath}}' $p): $(go list -f '{{range .Imports}}{{.}} {{end}}' $p | tr ' ' '\n' | grep '^github.com/kartaladev/msgin' | tr '\n' ' ')"
  done
github.com/kartaladev/msgin:
github.com/kartaladev/msgin/endpoint:   github.com/kartaladev/msgin
github.com/kartaladev/msgin/routing:    github.com/kartaladev/msgin
github.com/kartaladev/msgin/transform:  github.com/kartaladev/msgin
github.com/kartaladev/msgin/channel:    github.com/kartaladev/msgin
github.com/kartaladev/msgin/resilience: github.com/kartaladev/msgin
```

Root imports **nothing** from the module; each of the five subpackages imports **only** root. Fan-out from
root, zero sibling edges.

#### F9.7 — verification: all seven modules GREEN, including both Docker-backed runners

```
$ gofmt -l . adapter endpoint routing transform channel resilience
(empty)
$ go build ./... ; go vet ./...
build exit=0
vet exit=0

$ for d in . adapter/database/sql/harness adapter/database/sql/postgres adapter/database/sql/mysql \
           adapter/database/sql/sqlite adapter/database/sql/dbtest adapter/cron/crontest; do
    (cd "$d" && GOWORK=off go build ./... >/dev/null 2>&1 && GOWORK=off go vet ./... >/dev/null 2>&1 \
      && echo "GREEN: $d") || echo "RED: $d"
  done
GREEN: .
GREEN: adapter/database/sql/harness
GREEN: adapter/database/sql/postgres
GREEN: adapter/database/sql/mysql
GREEN: adapter/database/sql/sqlite
GREEN: adapter/database/sql/dbtest
GREEN: adapter/cron/crontest
```

All three previously-RED modules (`harness`, `postgres`, `dbtest` — F8.7) are now GREEN.

```
$ GOWORK=off go test ./... -race -count=1 -shuffle=on          # root module, 11 packages
ok  github.com/kartaladev/msgin                     1.208s
ok  github.com/kartaladev/msgin/adapter/cron        3.468s
ok  github.com/kartaladev/msgin/adapter/database/sql 1.480s
ok  github.com/kartaladev/msgin/adapter/http        2.189s
ok  github.com/kartaladev/msgin/adapter/http/stdlib 1.787s
ok  github.com/kartaladev/msgin/adapter/memory      2.199s
ok  github.com/kartaladev/msgin/channel             1.932s
ok  github.com/kartaladev/msgin/endpoint            3.941s
ok  github.com/kartaladev/msgin/resilience          2.305s
ok  github.com/kartaladev/msgin/routing             2.436s
ok  github.com/kartaladev/msgin/transform           2.423s
exit=0
```

**Docker WAS running** (`docker info` exit 0, client 29.5.3), so `dbtest` and `crontest` are **verified,
not skipped** — the two testcontainers-backed conformance runners ran for real:

```
$ (cd adapter/database/sql/dbtest  && GOWORK=off go test ./... -race -count=1)
ok  github.com/kartaladev/msgin/adapter/database/sql/dbtest   122.112s   exit=0
$ (cd adapter/cron/crontest        && GOWORK=off go test ./... -race -count=1)
ok  github.com/kartaladev/msgin/adapter/cron/crontest          62.654s   exit=0
$ for d in harness postgres mysql sqlite; do (cd adapter/database/sql/$d && GOWORK=off go test ./... -race -count=1); done
?   github.com/kartaladev/msgin/adapter/database/sql/harness   [no test files]
ok  github.com/kartaladev/msgin/adapter/database/sql/postgres  1.463s
ok  github.com/kartaladev/msgin/adapter/database/sql/mysql     1.431s
ok  github.com/kartaladev/msgin/adapter/database/sql/sqlite    1.440s
```

Note for the plan: **`harness` has zero test files of its own.** It is a library of `Run*(t, …)` conformance
helpers consumed by `dbtest`; `go test` on it proves nothing, and only `go vet` (which compiles it) plus
`dbtest`'s run actually exercise it. A gate that runs `go test` per module and reads "no test files" as a
pass would have missed every one of its 68 rewritten selectors.

#### F9.8 — stale-reference sweep: `adapter/` clean, **2 residual sites inside the core packages**

```
$ while IFS=$'\t' read -r pkg sym; do
    grep -rn --include='*.go' --exclude-dir=docs "msgin\.${sym}\b" .
  done < symmap.tsv | sort -u
codec.go:33://  msgin.NewProducer[[]byte](out, msgin.WithProducerCodec[[]byte](msgin.BytesPayloadCodec{}))
routing/aggregator_test.go:21:// tests instead of a *msgin.DirectChannel + subscriber.

$ …same loop restricted to adapter/…
(no output — adapter/ has ZERO stale references)
```

**`adapter/` is clean.** The two survivors are **godoc comments inside the already-extracted core
packages** — out of this task's scope (the core tree is frozen and its byte-level identity was proved in
F8.6, so it was deliberately not edited):

| site | reads | should read |
|---|---|---|
| `codec.go:33` (root, godoc example on the codec doc block) | `msgin.NewProducer` / `msgin.WithProducerCodec` | `endpoint.NewProducer` / `endpoint.WithProducerCodec` |
| `routing/aggregator_test.go:21` (comment) | `*msgin.DirectChannel` | `*channel.DirectChannel` |

**For the regenerated plan:** the godoc-staleness sweep is *not* an adapter-only task. The core-extraction
tasks leave their own stale mentions behind, and `go build`/`go vet`/`go test`/`gofmt` are all blind to
them. The plan needs one explicit, tree-wide grep gate — the loop above — run **after the last move**, not
a per-task file list.

#### F9.9 — change inventory (the artifact two audits got wrong)

```
$ git diff --numstat -- adapter/
3	2	adapter/cron/consumer_integration_test.go
1	1	adapter/cron/example_test.go
1	1	adapter/database/sql/errors.go
9	7	adapter/database/sql/harness/groupstore.go
2	1	adapter/database/sql/harness/harness.go
29	28	adapter/database/sql/harness/lock.go
20	19	adapter/database/sql/harness/outbound.go
1	1	adapter/database/sql/harness/queuestore.go
19	17	adapter/database/sql/harness/source.go
1	1	adapter/database/sql/options.go
5	5	adapter/database/sql/outbound.go
1	1	adapter/database/sql/outbound_test.go
7	5	adapter/database/sql/postgres/example_sql_groupstore_test.go
1	1	adapter/database/sql/queuestore.go
2	2	adapter/database/sql/runner_test.go
4	3	adapter/database/sql/source.go
13	11	adapter/http/classify_test.go
2	2	adapter/http/doc.go
4	4	adapter/http/exchange.go
10	9	adapter/http/exchange_test.go
3	3	adapter/http/inbound.go
12	10	adapter/http/inbound_test.go
2	2	adapter/http/options.go
2	2	adapter/http/outbound.go
12	10	adapter/http/outbound_test.go
3	2	adapter/http/sse_e2e_test.go
11	9	adapter/http/stdlib/inbound_test.go
1	1	adapter/memory/queuestore.go

$ git diff --stat -- adapter/ | tail -1
 28 files changed, 181 insertions(+), 160 deletions(-)
```

Rolled up per adapter / satellite module:

```
$ git diff --numstat -- adapter/ | awk '…bucket by path…'
adapter/cron                             files=2   +4   -3
adapter/database/sql                     files=7   +15  -14
adapter/database/sql/harness (MODULE)    files=6   +80  -73
adapter/database/sql/postgres (MODULE)   files=1   +7   -5
adapter/http                             files=10  +63  -55
adapter/http/stdlib                      files=1   +11  -9
adapter/memory                           files=1   +1   -1
```

Note the diff spans **28 files vs HEAD**, which includes the 4 files already rewritten by the earlier
`resilience` extraction (`adapter/database/sql/{source.go,harness/source.go}`,
`adapter/http/{classify_test.go,outbound_test.go}` — `msgin.ExponentialBackoff → resilience.ExponentialBackoff`).
The `endpoint`/`routing`/`channel` pass touched the same 28-file set.

**The adapter tree is 28 changed files / ±181 lines — 28 of the repo's 198 non-doc `.go` files, and the entire
`msgin.X → <dest>.X` change is mechanical.** Any plan that sizes the adapter requalification as a
multi-task effort is over-sizing it; the real risk is not volume but the **two silent classes** measured
here: the 39 godoc mentions no compiler sees (F9.3), and `harness`'s **69 non-test selectors in a module
with zero test files of its own** (F9.7).

---

## F10 — channel segregation, D-F, and the rename

Scope: Plan 027 **Task 2** (`MessageChannel` segregation), decision **D-F**
(`channel.WithSingleSubscriber()`), Plan 027 **Task 3** (`StreamingSource` → `EventDrivenSource`).
Every number below is paired with the command that produced it. Toolchain: `GOTOOLCHAIN=go1.25.12`.

### F10.1 — The RED artifact (compile failure, no FAIL line)

The capability test (`capability_test.go`, root, `package msgin_test`) was written and run against
**unmodified** production code. Plan 027 Task 2's audit-F5 note is confirmed: the whole `msgin_test` binary
fails to build, so there is no `FAIL` line — the compiler transcript IS the RED evidence.

```
$ go test -c -o /dev/null .
# github.com/kartaladev/msgin_test [github.com/kartaladev/msgin.test]
./capability_test.go:40:7: cannot use qc (variable of type *channel.QueueChannel) as msgin.MessageChannel value in struct literal: *channel.QueueChannel does not implement msgin.MessageChannel (missing method Subscribe)
./capability_test.go:68:24: cannot use ps (variable of type *channel.PublishSubscribeChannel) as msgin.MessageChannel value in struct literal: *channel.PublishSubscribeChannel does not implement msgin.MessageChannel (wrong type for method Subscribe)
		have Subscribe(msgin.MessageHandler) (msgin.Subscription, error)
		want Subscribe(msgin.MessageHandler) error
./capability_test.go:90:24: cannot use b (variable of type *memory.Broker) as msgin.MessageChannel value in struct literal: *memory.Broker does not implement msgin.MessageChannel (missing method Subscribe)
```

Three distinct failures — one per target kind — matching ADR 0028's three-row satisfaction table plus §5's
`OutboundAdapter` widening. `go vet .` shows only the **first** of the three (it stops after one type error
batch), which is why the transcript above is taken from `go test -c`, not from vet.

GREEN after implementation: `go test ./... -race -shuffle=on` (§F10.7) — 9 subtests
(`TestSendOnlyCallSitesAcceptEveryChannel`, 3 targets × 3 call sites) pass.

### F10.2 — **ADR 0028's call-site census is wrong, and so is round-2's correction of it**

ADR 0028's Context says *"Four of the five call sites use only `Send`"*. Round-2 §D5 corrected that to
*"six of seven (`aggregator.go:47,114` are send-only and unlisted)"*. **Both are wrong.** Mechanically:

```
$ grep -rn "msgin\.MessageChannel\|MessageChannel interface" --include="*.go" . \
    | grep -v "_test.go" | grep -v "^./docs" | grep -v '// '
channel.go:20:type MessageChannel interface {
adapter/http/inbound.go:116:func ServeAsync(w http.ResponseWriter, r *http.Request, target msgin.MessageChannel, cfg *Config) {
adapter/http/stdlib/inbound.go:33:func NewInbound(target msgin.MessageChannel, opts ...msghttp.Option) (http.Handler, error) {
channel/direct.go:28:	_ msgin.MessageChannel      = (*DirectChannel)(nil)
endpoint/exchange.go:192:	request   msgin.MessageChannel
endpoint/exchange.go:223:func NewChannelExchange(request msgin.MessageChannel, reply msgin.SubscribableChannel, …
routing/aggregator.go:14:	output    msgin.MessageChannel
routing/aggregator.go:18:	expired   msgin.MessageChannel
routing/aggregator.go:55:func WithOutputChannel(ch msgin.MessageChannel) AggregatorOption {
routing/aggregator.go:133:func WithExpiredGroupChannel(ch msgin.MessageChannel) AggregatorOption {
routing/filter.go:9:type filterConfig struct{ discard msgin.MessageChannel }
routing/filter.go:18:func WithDiscardChannel(ch msgin.MessageChannel) FilterOption {
routing/router.go:9:type routerConfig struct{ defaultCh msgin.MessageChannel }
routing/router.go:18:func WithDefaultChannel(ch msgin.MessageChannel) RouterOption {
routing/router.go:29:	pick func(ctx context.Context, m msgin.Message[any]) (msgin.MessageChannel, error)
routing/router.go:37:func NewRouter(pick func(…) (msgin.MessageChannel, error), opts ...RouterOption) *Router {
```

The **public** call sites are **nine**, not five and not seven — **eight send-only, one subscribing**:

| # | Send-only site | File |
|---|---|---|
| 1 | `routing.WithDiscardChannel` | `routing/filter.go:18` |
| 2 | `routing.WithDefaultChannel` | `routing/router.go:18` |
| 3 | `routing.NewRouter`'s `pick` return | `routing/router.go:29,37` |
| 4 | `routing.WithOutputChannel` | `routing/aggregator.go:55` |
| 5 | `routing.WithExpiredGroupChannel` | `routing/aggregator.go:133` |
| 6 | `endpoint.NewChannelExchange`'s `request` | `endpoint/exchange.go:223` |
| 7 | **`msghttp.ServeAsync`'s `target`** | `adapter/http/inbound.go:116` |
| 8 | **`stdlib.NewInbound`'s `target`** | `adapter/http/stdlib/inbound.go:33` |
| — | `endpoint.NewChannelExchange`'s `reply` (the only subscriber) | `endpoint/exchange.go` |

Rows 7–8 appear in **neither** the ADR **nor** round-2's correction: both audits searched only the pattern
core and stopped at the module boundary. This is the "fix the class, not the instance" failure again — round 2
fixed the named count instead of asserting the invariant *"enumerate every non-test `msgin.MessageChannel`
declaration in the workspace"*. The consequence is a real, unrecorded capability widening: **the HTTP inbound
handler's `target` now accepts a `QueueChannel`, a `PublishSubscribeChannel`, and any `OutboundAdapter`** —
i.e. an HTTP request can now be parked in a durable queue channel instead of requiring a synchronous
subscriber. That is desirable, but it is user-visible behavior no artifact currently records.

### F10.3 — `ChannelExchange.Close` already existed (round-2 §D5 confirmed), and §6 changes its contract

Confirmed against the code: `Close` was at `exchange.go:338` before this work, so ADR 0028's Consequences
line *"`ChannelExchange` **gains** a real `Close`"* is false — §6 **extends** it. More importantly, §6.1's
`replySub.Cancel()` changes an **observable, previously-documented** behavior that the ADR records only as an
aside ("Unrecorded behavior change" in the status block) and never folds into the Decision:

- **Before:** the reply receiver stayed subscribed forever, so a reply arriving after `Close` was absorbed by
  the receiver and routed to `WithUnmatchedReplySink`.
- **After:** the receiver is unsubscribed, so a post-`Close` reply is the **channel's** problem — a
  `DirectChannel` returns `ErrNoSubscriber` **to the reply sender**, and a `PublishSubscribeChannel` delivers
  to whoever else is subscribed. It never reaches `WithUnmatchedReplySink`.

This is not a leak fix with no downside; it moves a failure from a silent sink to the sender's error return.
It is now documented on `Close`'s godoc and pinned by
`TestChannelExchange_closeCancelsReplySubscription`. **Spec 014 §5.2 and ADR 0028 §6 still need this stated as
a decided consequence, not a status-block footnote.**

### F10.4 — ADR 0029 §1's rename scope is right for `.go` and silently short by 5

ADR 0029 §1 says *"`StreamingSource` appears **30 times across 12 files, all inside the root module**"*.
Measured before the rename:

```
$ for f in $(grep -rl 'StreamingSource' . --exclude-dir=.git --exclude-dir=docs); do
    printf "%4d  %s\n" "$(grep -c 'StreamingSource' $f)" "$f"; done
   2  spi.go                (already renamed at measurement time)
   3  adapter/cron/source.go
   1  adapter/http/doc.go
   1  adapter/http/sse_e2e_test.go
   3  adapter/http/sseclient.go
   1  adapter/memory/memory.go
   5  endpoint/consumer_test.go
   5  endpoint/consumer.go
   2  endpoint/flowcontrol.go
   5  endpoint/settlement_doubles_test.go
   1  errors.go
   1  spi_test.go
   2  CLAUDE.md
   3  MESSAGING.md
```

**`.go` files: 30 occurrences across 12 files — ADR 0029 §1 is EXACTLY right, and audit F7's correction of the
earlier "seven-module" sizing is confirmed.** But the count is `.go`-only: **`CLAUDE.md` (2) and `MESSAGING.md`
(3)** carry five more, and Plan 027 Task 3's verify step is *"`grep -rn 'StreamingSource' .` returns nothing
outside `MIGRATION.md`"* — which those two files fail. ADR 0029's Consequences names `CLAUDE.md`;
**`MESSAGING.md` is named nowhere in the bundle.** Both were renamed here. Total: **35 occurrences / 14 files.**

```
$ grep -rn 'StreamingSource' . --exclude-dir=.git --exclude-dir=docs
(no output, exit 1)
$ grep -rn 'EventDrivenSource' . --exclude-dir=.git --exclude-dir=docs | wc -l
      35
```

The occurrence set was derived **by grep, never from compiler stderr** — as required, since Go caps at 10
errors per package. The compiler-invisible site the brief flagged was renamed with it:

```
$ grep -n 'EventDrivenSource' errors.go
22:	ErrUnsupportedSource = errors.New("msgin: source implements neither PollingSource nor EventDrivenSource")
```

### F10.5 — Two things ADR 0028 §7 does not specify, both of which the implementation must decide

§7's semantics table is complete for the five questions it asks, and all five are implemented as written. Two
further questions are **forced by the design and unanswered by the ADR**:

1. **A stale handle's `Cancel` after a resubscribe.** `Cancel` → `Subscribe` → old-handle `Cancel` again. With
   a naive `sync.Once` + "clear the handler" the second `Cancel` **evicts the new subscriber**, silently
   breaking a live flow. The implementation therefore releases the slot only on **identity**
   (`if c.sub == s`) — the same defence `PublishSubscribeChannel.remove` and `replyCorrelator.deregister`
   already use. Covered by `TestDirectChannel_SubscriptionLifecycle/"a stale handle's cancel does not evict
   the current subscriber"`. **This should be a sixth row in §7.**
2. **`PubSub` registry error handling under D-F.** `pubsub_registry.go:65`'s error branch carried the comment
   *"defensive: `ch.Subscribe` only errors on a nil handler, already guarded above"*. D-F **falsifies** that:
   a second subscribe to an existing topic now returns `ErrChannelSubscribed`, making the branch live. The
   comment is rewritten and the branch is covered by `TestPubSub_SingleSubscriberPropagatesToTopics`. (A guard
   that dropped a "created but rejected" topic was written and then **removed as dead code**: a topic this call
   creates is empty, so the `single` guard cannot reject the subscriber that created it. Keeping it would have
   added an untestable hot-path branch — a CLAUDE.md delivery blocker.)

### F10.6 — Call-form churn in pre-existing tests (no assertion semantics changed)

`DirectChannel.Subscribe` returning `(Subscription, error)` forces a **call-form** edit at every site.
Assertion *semantics* are unchanged everywhere; the edit is one of three shapes:
`require.NoError(t, ch.Subscribe(h))` → `mustSubscribe(t, ch, h)` (a `t.Helper()` that does the same
`require.NoError`), `if err := ch.Subscribe(h)` → `if _, err := ch.Subscribe(h)`, or `_ = ch.Subscribe(h)` →
`_, subErr := …; require.NoError(t, subErr)`.

```
$ git diff --stat -- ':!docs' | tail -1
 32 files changed, 669 insertions(+), 153 deletions(-)
```

Two call sites are in **satellite modules**, which no artifact anticipates —
`adapter/database/sql/harness/groupstore.go:402,408` (a **non-test** file in a module with zero test files of
its own, F9.7's blind spot again) and `adapter/database/sql/postgres/example_sql_groupstore_test.go:54`.
**ADR 0028 and Plan 027 Task 2 both describe this as a root-module change; it is not.**

Five fakes existed only to satisfy the old bundled interface and were deleted, not migrated:
`fakeAggChannel`, `failNthChannel`, `idsAggChannel`, `collector`, `scriptedChannel` — each had a
`Subscribe(msgin.MessageHandler) error` returning `nil`. Their disappearance is itself evidence the
segregation was correct: **five of the six `MessageChannel` implementations in the test suite never wanted
`Subscribe` at all.**

### F10.7 — The green gate: seven modules, `-race -shuffle=on`

```
$ for d in . adapter/database/sql/{harness,postgres,mysql,sqlite,dbtest} adapter/cron/crontest; do
    (cd "$d" && GOWORK=off go build ./... && GOWORK=off go vet ./... && echo "GREEN: $d") || echo "RED: $d"; done
GREEN: .
GREEN: adapter/database/sql/harness
GREEN: adapter/database/sql/postgres
GREEN: adapter/database/sql/mysql
GREEN: adapter/database/sql/sqlite
GREEN: adapter/database/sql/dbtest
GREEN: adapter/cron/crontest

$ GOWORK=off go test ./... -race -shuffle=on -count=1
ok  	github.com/kartaladev/msgin	2.090s
ok  	github.com/kartaladev/msgin/adapter/cron	3.652s
ok  	github.com/kartaladev/msgin/adapter/database/sql	2.348s
ok  	github.com/kartaladev/msgin/adapter/http	5.781s
ok  	github.com/kartaladev/msgin/adapter/http/stdlib	4.393s
ok  	github.com/kartaladev/msgin/adapter/memory	4.948s
ok  	github.com/kartaladev/msgin/channel	3.167s
ok  	github.com/kartaladev/msgin/endpoint	4.858s
ok  	github.com/kartaladev/msgin/resilience	3.966s
ok  	github.com/kartaladev/msgin/routing	5.877s
ok  	github.com/kartaladev/msgin/transform	2.734s

$ (cd <each satellite> && GOWORK=off go test ./... -race -shuffle=on -count=1 | tail -1)
adapter/database/sql/harness    ?   …/harness	[no test files]
adapter/database/sql/postgres   ok  …/postgres	1.415s
adapter/database/sql/mysql      ok  …/mysql	1.420s
adapter/database/sql/sqlite     ok  …/sqlite	1.432s
adapter/cron/crontest           ok  …/crontest	47.120s   (Docker)
adapter/database/sql/dbtest     ok  …/dbtest	106.338s  (Docker)

$ go build ./... && go vet ./... && gofmt -l .
(no output)
```

### F10.8 — Coverage, measured with `-coverpkg=./...` on both sides

Per the brief: a default-profile comparison across the split is **not** like-for-like. Both sides use the same
command; the merged profile is deduplicated block-by-block with `max(count)` (11 test binaries each instrument
all 11 packages, so a naive sum inflates the denominator ~11×).

```
$ go test ./... -count=1 -coverpkg=./... -coverprofile=<f>    # then max-dedup rollup per package
                                              BASELINE          AFTER
github.com/kartaladev/msgin                    95.45% (105/110)  95.45% (105/110)
github.com/kartaladev/msgin/adapter/cron       50.76% (167/329)  50.76% (167/329)
github.com/kartaladev/msgin/adapter/database/sql 93.71% (313/334) 93.71% (313/334)
github.com/kartaladev/msgin/adapter/http      100.00% (797/797) 100.00% (797/797)
github.com/kartaladev/msgin/adapter/http/stdlib 100.00% (15/15)  100.00% (15/15)
github.com/kartaladev/msgin/adapter/memory    100.00% (178/178) 100.00% (178/178)
github.com/kartaladev/msgin/channel            98.13% (105/107) 100.00% (118/118)   ← +1.87
github.com/kartaladev/msgin/endpoint           99.41% (676/680)  99.12% (677/683)   ← see below
github.com/kartaladev/msgin/resilience         99.11% (111/112)  99.11% (111/112)
github.com/kartaladev/msgin/routing           100.00% (204/204) 100.00% (204/204)
github.com/kartaladev/msgin/transform         100.00% (15/15)   100.00% (15/15)
TOTAL                                          93.23% (2686/2881) 93.26% (2700/2895)
```

`channel` reaches **100%** — the `Subscription` lifecycle, the identity guard, and `WithSingleSubscriber`'s
three arms are all covered. `adapter/cron`'s 50.76% is an artifact of `crontest` being a separate module that a
root `./...` cannot see; unchanged on both sides.

**`endpoint`'s 99.41% → 99.12% is NOT a regression** — it is a flaky, timing-dependent branch, and the number
is worth reporting precisely because the baseline's higher figure was luck:

```
$ for i in 1 2 3; do go test ./endpoint/... -count=1 -coverpkg=./... -coverprofile=e$i.cov >/dev/null
    awk '$1 ~ /consumer.go:467.20,469.15/ {print $3}' e$i.cov | sort -u; done
run 1: 0
run 2: 1
run 3: 0
```

`endpoint/consumer.go:467.20,469.15` is the `case <-ctx.Done():` arm of the dispatch select — covered in 1 of 3
runs. It is untouched by this work (the only edit to `consumer.go` was the `EventDrivenSource` rename). The
other five uncovered blocks are byte-identical before and after: `gateway.go:30`, `nativereliability.go:9`,
`poller.go:152`, `poller.go:164`, `resilience/breaker.go:179`. **No new uncovered block was introduced.**

This is the "measure interleaving tests, don't trust them" rule applied to coverage itself: a single
`-coverpkg` run is not a stable measurement of a race-arm branch, and any gate that diffs one run against
another will produce false regressions on this package.

### F10.9 — Hot-path branch → covering test

| Branch (ADR 0028 §7 / D-F) | Test |
|---|---|
| `DirectChannel.Subscribe` second-subscriber rejection → `ErrChannelSubscribed` | `channel: TestDirectChannel_Errors/"subscribe twice is ErrChannelSubscribed"` (also asserts a **nil** handle) |
| `Subscribe` after `Cancel()` succeeds | `TestDirectChannel_SubscriptionLifecycle/"subscribe after cancel succeeds and the new handler receives"` |
| `Send` between `Cancel` and the next `Subscribe` → `ErrNoSubscriber` | `TestDirectChannel_SubscriptionLifecycle/"send between cancel and the next subscribe is ErrNoSubscriber"` |
| `Cancel` twice is idempotent, never panics | `TestDirectChannel_SubscriptionLifecycle/"cancel twice is idempotent and never panics"` |
| `Cancel` racing an in-flight `Send` (in-flight `Handle` completes) | `TestDirectChannel_CancelDuringInFlightSend` — the handler blocks, `Cancel` fires mid-dispatch, the handler is released; asserts completion **and** that the next `Send` is `ErrNoSubscriber` |
| `Subscribe(nil)` → `(nil, ErrNilHandler)` | `TestDirectChannel_Errors/"subscribe nil handler is ErrNilHandler and no handle"` |
| *(F10.5.1, unspecified in §7)* stale handle's `Cancel` must not evict the current subscriber | `TestDirectChannel_SubscriptionLifecycle/"a stale handle's cancel does not evict the current subscriber"` |
| `ChannelExchange.Close` cancels the reply subscription | `endpoint: TestChannelExchange_closeCancelsReplySubscription` — proves the slot is held while open, released after `Close`, and that a post-`Close` reply is `ErrNoSubscriber` |
| Two exchanges over one `PublishSubscribeChannel` (documented fan-out) | `TestChannelExchange_sharedPubSubReplyChannel/"default fan-out…"` — the owner still gets its reply **and** a full copy reaches the other exchange's `WithUnmatchedReplySink` |
| `WithSingleSubscriber` first-ok | `channel: TestPublishSubscribeChannel_SingleSubscriber/"option on: the first subscriber is accepted"` |
| `WithSingleSubscriber` second-error | `…/"option on: a second subscriber is ErrChannelSubscribed"` **and** `endpoint: TestChannelExchange_sharedPubSubReplyChannel/"WithSingleSubscriber: the second exchange is rejected at construction"` |
| `WithSingleSubscriber` option-off | `…/"option off: a second subscriber is accepted (fan-out is the default)"` |
| *(extra)* `WithSingleSubscriber` + `Cancel` frees the slot | `…/"option on: cancelling frees the slot for a new subscriber"` |
| *(extra)* `WithSingleSubscriber` + nil handler still `ErrNilHandler` | `…/"option on: a nil handler is still ErrNilHandler"` |
| *(extra, F10.5.2)* registry error branch under D-F | `TestPubSub_SingleSubscriberPropagatesToTopics` |
| Capability: 3 channel kinds × 3 send-only sites | `root: TestSendOnlyCallSitesAcceptEveryChannel` (9 subtests) |

### F10.10 — Summary of artifact corrections required

1. **ADR 0028 Context + §5** — the call-site census is nine sites (eight send-only), not five/seven. Two are in
   `adapter/http` and `adapter/http/stdlib`, outside the pattern core (F10.2).
2. **ADR 0028 §6 / Spec 014 §5.2** — the post-`Close` behavior change must be a decided consequence, not a
   status-block footnote (F10.3).
3. **ADR 0028 §7** — needs a sixth row for stale-handle `Cancel` (F10.5.1).
4. **ADR 0028 Consequences** — *"`ChannelExchange` gains a real `Close`"* is false; `Close` already existed
   (F10.3, confirming round-2 §D5).
5. **ADR 0029 §1 / Plan 027 Task 3** — the 30-across-12 count is `.go`-only; `MESSAGING.md` is named nowhere
   (F10.4).
6. **ADR 0028 / Plan 027 Task 2** — Task 2 is not a root-module-only change; it edits two satellite modules
   (F10.6).

**Done in this run** (the two `docs/` edits this task authorised): **ADR 0028 §6.2 rewritten** to record
decision D-F (registry rebuttal kept, channel-local opt-in added, off by default, reuses
`ErrChannelSubscribed`, single-process only) and its status-block "Open" clause marked RESOLVED; **Spec 014
§4.1** extended from three to five new exported symbols (`SubscribableChannel`, `channel.WithSingleSubscriber`).
Corrections 1–6 above are **not** applied — they are outside this task's doc authorisation and belong to Task E's
regeneration.

---

## F11 — regeneration notes (Task E, 2026-07-28)

Everything below was derived during the document regeneration. Same contract as F0–F10: a number with no
pasted command is worthless. Toolchain `GOTOOLCHAIN=go1.25.12`, `PATH="$(go env GOPATH)/bin:$PATH"`.

### F11.0 — What state the tree is actually in

The migration commit is `c83dde9`; **F10's channel-segregation / D-F / rename work is still uncommitted**, so
the regenerated documents describe the **working tree**, not `c83dde9`.

```
$ git log --oneline -1
c83dde9 refactor(core)!: extract the flat core into endpoint/routing/transform/channel/resilience
$ git log --oneline -1 c83dde9~1
ab233d9 docs(audit): settle all six round-1 decisions; record the governing criteria   ← the pre-migration baseline
```

### F11.1 — §3.2 declaration-level split tables, generated

Method: parse the ORIGINAL file's top-level declarations from the pre-migration commit, then locate each
`(kind, name)` pair in the current tree's AST-derived declaration dump. **80 declarations across the six split
files; every one located; zero `(GONE)`.**

```
$ for f in channel.go pubsub.go backoff.go exchange.go flowcontrol.go pubsub_registry.go; do
    git show c83dde9~1:$f > /tmp/msgin-derive/orig-splits/$f; done
$ /tmp/msgin-derive/decls /tmp/msgin-derive/orig-splits | wc -l
      80
$ for d in . endpoint routing transform channel resilience; do
    /tmp/msgin-derive/decls $d | grep -v '_test\.go' | awk -F'\t' -v D="$d" '{print D"/"$1"\t"$2"\t"$3"\t"$4"\t"$5}'
  done > /tmp/msgin-derive/new-decls.tsv
$ wc -l < /tmp/msgin-derive/new-decls.tsv
     380
$ /tmp/msgin-derive/locate.sh            # joins the two by (kind,name)
```

The six generated tables are transcribed verbatim into **Spec 014 §3.2**. Highlights the earlier hand-typed
tables got wrong:

- **`pubsub.go` is a 19-declaration split**, not the 8 §3.2 listed. The three round-2 §B3 named
  (`pubSubConfig`, `defaultPubSubConfig`, `withConfig`) are confirmed present and moving, and so are five
  methods and `PublishSubscribeChannel.remove`/`isEmpty` that no document listed at all.
- **`exchange.go` is a 20-declaration split**, not the 9 §3.2 listed. `exchangeConfig` and
  `newReplyCorrelator` (round-2 §B3) confirmed, plus six `replyCorrelator`/`ChannelExchange` methods.
- **`pubsub_registry.go` is the sixth split** (D-B), 9 declarations: `TopicPublisher`/`TopicSubscriber` →
  root `spi.go:91,99`; the other seven → `channel/pubsub_registry.go`.
- **`Subscription` landed in root `channel.go:49`** (D-C), from `pubsub.go:37`. Root stays at 14 files.

### F11.2 — The exported-surface diff (`apidiff`), generated

```
$ apidiff /tmp/msgin-derive/root.api .
Incompatible changes:   95 lines, all "removed"
Compatible changes:
- EventDrivenSource: added
- IsPermanent: added
- NewID: added
- RetryAfterOf: added
- SubscribableChannel: added
$ apidiff /tmp/msgin-derive/root.api . | grep -c ': removed'
      93
```

`/tmp/msgin-derive/root.api` was written by `apidiff -w` at baseline (22:17, before Task 1). The full 93-line
removal list is transcribed into Spec 014 §4's closed contract discussion; the five additions are §4.1.

**95 removals decompose as:** 87 symbols relocated into `endpoint`/`routing`/`channel`/`resilience`; six
`*Expr` constructors deleted outright (`FilterExpr`, `RouterExpr`, `TransformExpr`, `SplitExpr`,
`WithCorrelationExpr`, `WithReleaseExpr`); one rename (`StreamingSource`, whose replacement
`EventDrivenSource` shows on the additions side); and `MessageChannel.Subscribe: removed`, which is the
segregation itself.

**The decomposition is a verified partition, not a grouping**, and it was checked by set arithmetic against
the symbol→destination map rather than by reading the list:

```
$ apidiff root.api . | awk '/^Incompatible/{f=1;next}/^Compatible/{f=0}f' | sed 's/^- //;s/: removed$//' > removed.txt
$ wc -l < removed.txt
      95
$ comm -12 <(sort removed.txt) <(cut -f2 symmap2.tsv | sort -u) | wc -l        # relocated
      87
$ grep -cE '^(FilterExpr|RouterExpr|TransformExpr|SplitExpr|WithCorrelationExpr|WithReleaseExpr)$' removed.txt
       6
$ grep -c '^StreamingSource$' removed.txt ; grep -c '^MessageChannel\.Subscribe$' removed.txt
       1        1
$ comm -23 <(sort removed.txt) <(cut -f2 symmap2.tsv | sort -u) \
    | grep -vE '^(FilterExpr|RouterExpr|TransformExpr|SplitExpr|WithCorrelationExpr|WithReleaseExpr|StreamingSource|MessageChannel\.Subscribe)$'
(no output — nothing unaccounted for)
```

87 + 6 + 1 + 1 = 95, with an empty residual set.

> **CORRECTION, recorded because it is this run's own defect class.** The first draft of this section, and of
> Spec 014 §4.1, stated **93 removals / 86 relocated** — hand-counted from the pasted list rather than from
> `wc -l`. The commands above give **95 / 87**. Corrected in Spec 014 §4.1, Plan 027 Tasks 12 and Risks, and
> here. The lesson is not "count more carefully"; it is that **any number reached by reading output instead of
> piping it is a hand-typed number**, and two audits died on exactly that. `WithReleaseStrategy: removed` appears because D-E changed its parameter type —
`apidiff` reports a signature change on a func value as removal, not as a compatible change.

### F11.3 — Root, per-package, and test-file inventories (regenerated at the current tree)

```
$ ls *.go | grep -v _test.go | wc -l
      14
$ ls *.go | grep -v _test.go | tr '\n' ' '
backoff.go channel.go codec.go doc.go errors.go flowcontrol.go groupstore.go handler.go message.go
payload.go reliability.go retry.go spi.go store.go

$ for p in endpoint routing transform channel resilience; do
    printf "%-12s %2d\n" $p $(ls $p/*.go | grep -v _test.go | wc -l); done
endpoint     11    routing       5    transform     1    channel       4    resilience    3

$ ls *_test.go | wc -l ; for p in endpoint routing transform channel resilience; do ls $p/*_test.go | wc -l; done
root 11   endpoint 16   routing 7   transform 1   channel 7   resilience 3     TOTAL 45
```

**Root test files are now 11, not F8.2's 10** — F10 added `capability_test.go`. So the three frames are:
**45 at baseline** (pre-Task-1 inventory), **44 placed** (`expr_test.go` deleted), **45 in the tree today**
(44 placed + 1 new capability test). Every regenerated table states its frame.

### F11.4 — Invariants re-derived (all hold)

```
$ go list -deps . | grep -E 'kartaladev/msgin/(endpoint|routing|transform|channel|resilience)'
(no output — C-full holds)

$ for p in . ./endpoint ./routing ./transform ./channel ./resilience; do
    echo "$(go list -f '{{.ImportPath}}' $p): $(go list -f '{{range .Imports}}{{.}} {{end}}' $p \
      | tr ' ' '\n' | grep '^github.com/kartaladev/msgin' | tr '\n' ' ')"; done
github.com/kartaladev/msgin:
github.com/kartaladev/msgin/endpoint:   github.com/kartaladev/msgin
github.com/kartaladev/msgin/routing:    github.com/kartaladev/msgin
github.com/kartaladev/msgin/transform:  github.com/kartaladev/msgin
github.com/kartaladev/msgin/channel:    github.com/kartaladev/msgin
github.com/kartaladev/msgin/resilience: github.com/kartaladev/msgin

$ decls . | grep -v _test.go | awk -F'\t' '$3=="var" && $4 ~ /^Err/ {print $1}' | sort | uniq -c
  42 errors.go                               # confirms F1 / round-2 §A5 at the current tree

$ decls . | grep -v '_test\.go' | awk -F'\t' '$5=="exported" && $3!="method" {print $4}' | sort -u | wc -l
     101                                     # was 100 in F9.0; +SubscribableChannel

$ # symbol→destination map is still a total function
$ cut -f2 symmap2.tsv | sort | uniq -d ; comm -12 root-exported.txt <(cut -f2 symmap2.tsv | sort -u)
(both empty)
$ wc -l < symmap2.tsv ; cut -f1 symmap2.tsv | sort | uniq -c
      91
  15 channel   45 endpoint    9 resilience   21 routing    1 transform
```

91 moved symbols (was 90 in F9.0; +`channel.WithSingleSubscriber`).

### F11.5 — The `MessageChannel` census, re-derived as an **invariant** rather than a count

Round 1 said four of five. Round 2 said six of seven. F10.2 said nine. **The lesson is that a count is the
wrong artifact.** The check that cannot rot:

```
$ grep -rn "msgin\.MessageChannel\|MessageChannel interface" --include="*.go" . \
    | grep -v "_test.go" | grep -v "^./docs" | grep -v '// ' | wc -l
      16          # 1 declaration + 15 use sites, across the WHOLE workspace, adapters included
```

Re-run at the current tree, the nine public positions are unchanged from F10.2 — eight send-only
(`routing.WithDiscardChannel`, `routing.WithDefaultChannel`, `routing.NewRouter`'s `pick` return,
`routing.WithOutputChannel`, `routing.WithExpiredGroupChannel`, `endpoint.NewChannelExchange`'s `request`,
`msghttp.ServeAsync`'s `target`, `stdlib.NewInbound`'s `target`) plus one subscriber
(`endpoint.NewChannelExchange`'s `reply`, now `msgin.SubscribableChannel`).

**Both audits missed rows 7–8 because both searched only the pattern core.** The regenerated documents
therefore state the *scope rule* — "every non-test `MessageChannel` occurrence in the workspace, adapters
included" — with the command above, and give the enumeration as an illustration, not as the contract.

### F11.6 — **NEW: root's `boxMessage` and `nilFuncStep` are now dead code**

Plan 027 Task 8 says *"then delete root's `boxMessage` and `nilFuncStep`"*. The migration inlined per-package
copies but **did not delete root's originals**, and nothing reports it:

```
$ grep -rn 'boxMessage' *.go | grep -v _test.go
payload.go:28:// boxMessage lifts a typed Message[T] into Message[any], preserving headers
payload.go:30:func boxMessage[T any](m Message[T]) Message[any] {
$ grep -rn 'nilFuncStep' *.go | grep -v _test.go
handler.go:62:// nilFuncStep returns a Step whose handler always fails with ErrNilFunc — the
handler.go:66:func nilFuncStep() Step {
```

Zero users in root, zero users in root's tests. `.golangci.yml` sets `linters.default: none`, so `unused` is
off and Go does not error on unused package-level declarations — the same blind spot as F4's
`mixedTypeAddStore`. **Not fixed here** (this task is doc-only); recorded as a remaining work item in the
regenerated Plan 027.

### F11.7 — **NEW: the godoc-staleness sweep needs a second arm for DELETED symbols**

F9.8's sweep looks for `msgin.<moved-symbol>`. It cannot see a mention of a symbol that was **deleted**, and
seven such mentions survive:

```
$ grep -rn --include='*.go' -E '\b(FilterExpr|RouterExpr|TransformExpr|SplitExpr|WithCorrelationExpr|WithReleaseExpr)\b' .
errors.go:156:  // ErrInvalidExpression is returned by FilterExpr/RouterExpr when an
errors.go:175:  // TransformExpr/SplitExpr when a compiled expression evaluates to a value that
errors.go:176:  // is not the asserted output type — a non-B TransformExpr result, or a
errors.go:177:  // non-slice SplitExpr result / non-B SplitExpr element. It is an EVALUATION
routing/splitter.go:52:  // SplitExpr.
routing/aggregator.go:316:  return err // release-decision error (e.g. WithReleaseExpr eval) → retry/DLQ;
routing/aggregator_test.go:1276:// WithReleaseExpr can itself error at eval (WithReleaseStrategy's bool-only
$ grep -rn -E '\b(FilterExpr|RouterExpr)\b' CLAUDE.md
CLAUDE.md:235:  - **`github.com/expr-lang/expr`** — runtime expression evaluation for `FilterExpr`/`RouterExpr` …
```

And the moved-symbol arm still returns F9.8's two survivors, unchanged:

```
$ while IFS=$'\t' read -r pkg sym; do grep -rn --include='*.go' --exclude-dir=docs "msgin\.${sym}\b" .; done < symmap2.tsv | sort -u
codec.go:33://  msgin.NewProducer[[]byte](out, msgin.WithProducerCodec[[]byte](msgin.BytesPayloadCodec{}))
routing/aggregator_test.go:21:// tests instead of a *msgin.DirectChannel + subscriber.
```

**Related, and a design question the plan must answer, not assume:** `ErrInvalidExpression` (`errors.go:161`)
and `ErrExprResultType` (`errors.go:183`) are **orphaned in root** — zero users after the `*Expr` deletion,
and their godoc names constructors that no longer exist. Task 10 has to decide whether the `expr` module
imports them from root or mints its own; leaving two unreferenced sentinels in a "closed root contract" is
not a neutral default.

### F11.8 — **NEW: no subpackage has a `doc.go`**

Spec 014 §3.5 requires one per package. Root has one; the five new packages have none:

```
$ for p in . endpoint routing transform channel resilience; do
    echo "=== $p"; grep -rn '^// Package ' $p/*.go 2>/dev/null | grep -v _test.go; done
=== .
./doc.go:1:// Package msgin implements the messaging patterns of Gregor Hohpe and Bobby
=== endpoint
=== routing
=== transform
=== channel
=== resilience
```

`.golangci.yml`'s `linters.default: none` means `ST1000` is off, so nothing flags it. Remaining work.

### F11.9 — Tooling reality (round-2 §C1 re-verified)

All six tools are installed in `$(go env GOPATH)/bin` and **none is on the bare `PATH`**:

```
$ for t in gofumpt goimports apidiff gorelease gopls govulncheck; do
    command -v $t >/dev/null && echo "$t: ON PATH" || echo "$t: not on PATH"; done
gofumpt: not on PATH      goimports: not on PATH    apidiff: not on PATH
gorelease: not on PATH    gopls: not on PATH        govulncheck: not on PATH
$ for t in …; do [ -x "$(go env GOPATH)/bin/$t" ] && echo "$t: yes" || echo "$t: NO"; done
gofumpt: yes   goimports: yes   apidiff: yes   gorelease: yes   gopls: yes   govulncheck: yes
$ go env GOPATH
/Users/zakyalvan/go
$ git tag | wc -l
       0
```

So round-2 §C1's *"`apidiff`/`gorelease` are not installed"* is **no longer true** (they were installed
during this derivation run), and the plan's `gofumpt is not installed` line is **false** and always was. The
durable instruction is `export PATH="$(go env GOPATH)/bin:$PATH"`, not a per-tool availability claim.

### F11.10 — Round-2 §C4 quantified

```
$ grep -rn 'StreamingSource' --include='*.md' . | wc -l ; grep -rl 'StreamingSource' --include='*.md' . | wc -l
     129
      29
$ grep -rn 'StreamingSource' . --exclude-dir=.git --exclude-dir=docs
(no output, exit 1)
```

129 hits across 29 `.md` files — shipped ADRs 0002/0006/0008/0009/0010/0017/0018/0023 and shipped specs
among them, which CLAUDE.md forbids rewriting. The gate must be `--include='*.go'` plus the two root
narratives (`CLAUDE.md`, `MESSAGING.md`), and must exclude `docs/`.

### F11.11 — Coverage, re-measured at the current tree

```
$ GOWORK=off go test ./... -count=1 -coverpkg=./... -coverprofile=… && go tool cover -func=… | tail -1
total:  (statements)  93.3%
$ GOWORK=off go test ./... -count=1 -cover
msgin 81.8%   adapter/cron 50.8%   adapter/database/sql 93.7%   adapter/http 100.0%
adapter/http/stdlib 100.0%   adapter/memory 71.3%   channel 100.0%   endpoint 99.1%
resilience 99.1%   routing 100.0%   transform 100.0%
$ go tool cover -func=… | grep toHalfOpen
resilience/breaker.go:176:  toHalfOpen  87.5%
```

Confirms the brief: **default per-package puts root at 81.8%, below CLAUDE.md's 85% gate, purely as a
test-attribution artifact**; `-coverpkg=./...` puts the workspace at 93.3% against a 93.23% `-coverpkg`
baseline (F10.8). `toHalfOpen` at 87.5% **confirms round-2 §D7**: it is 87.5% *today*, post-split, and was
87.5% before — a pre-existing gap the extraction surfaces, not a regression.

### F11.12 — Claims from F0–F10 that did NOT reproduce

Every load-bearing claim was re-run. Two need restating:

1. **F8.2's "root 10" test files** is now **11** — correct when written, stale after F10 added
   `capability_test.go`. Frame stated in F11.3.
2. **F9.0's "90 moved symbols" / "root exports 100"** are now **91** and **101** — correct when written,
   changed by D-F's `WithSingleSubscriber` and the segregation's `SubscribableChannel`. Frame stated in
   F11.4.

Nothing else failed to reproduce. F1 (42), F8.3 (`retry_test.go` does not split), F9.6 (no new module edge),
F9.8 (two moved-symbol survivors), F10.2 (nine call sites), F10.4 (30/12 `.go`, 35/14 with the narratives)
all reproduced exactly.

### F11.13 — Contradictions left unresolved (flagged, not decided)

- **Spec 011 §6's phasing table vs. its own traceability note.** `docs/specs/011-http-adapter.md:630` assigns
  the gin binding to Plan **027**; `:682` says gin is Plan **028**. Resolved in favour of **028** in this
  pass (the note is the later, audited correction, and 027 is demonstrably the core-layout plan). Recording
  it because it is a resolution, not a transcription.
- **Spec 011 §6 Phases 3/4 carry no ✅ DELIVERED marker** although CLAUDE.md states SSE shipped in Plans
  025/026. Not touched — outside this task's authorisation, and marking a phase delivered is a claim that
  needs its own verification pass.

### F11.14 — Spec 014 §4's closed contract, verified to partition exactly

The §4 decomposition is not a hand-grouping — it was checked to be a **partition** of the AST dump, with no
symbol unaccounted for and no symbol counted twice:

```
$ decls . | grep -v '_test\.go' | awk -F'\t' '$5=="exported" && $3!="method" {print $3"\t"$4}' | sort -u
Counter({'var': 42, 'type': 33, 'func': 15, 'const': 11})   TOTAL 101

$ # partition check against §4's six groups
types unaccounted: set()
funcs unaccounted: set()
group sizes: 8 (vocab types) + 11 (consts) + 21 (SPI) + 4 (policy/codec) + 42 (sentinels) + 15 (allow-list) = 101
```

**One correction this surfaced:** round-1 §H4's allow-list included `HandlerFunc`, which is a **type**
(`type HandlerFunc func(…)`), not a func — so listing it under both "SPI/interfaces" and "constructors and
combinators" double-counted it. §4's table counts it once, under SPI, and the arithmetic now closes.

---

## F12 — round-3 code fixes

Six defects raised by the round-3 adversarial audit (3 Opus auditors, all NEEDS-REVISION), each fixed and
independently verified below. Every number is paired with the command that produced it. Uncommitted.

### F12.1 — `expr-lang` never left the six satellite modules (CI BLOCKER)

Task 1 dropped `github.com/expr-lang/expr` from the root module but never re-tidied the satellites, so six
of seven `go.mod`s still carried `github.com/expr-lang/expr v1.17.8 // indirect` and `go mod tidy` was
dirty in all six. CI runs a per-module `go mod tidy` + `git diff --exit-code`, so 5 of 6 matrix jobs were
red on a purely mechanical omission.

**Fix:** `GOWORK=off go mod tidy` in all seven modules.

```
$ for d in . adapter/database/sql/{harness,postgres,mysql,sqlite,dbtest} adapter/cron/crontest; do
    out=$(cd "$d" && GOWORK=off go mod tidy -diff 2>&1)
    [ -z "$out" ] && echo "CLEAN: $d" || { echo "DIRTY: $d"; echo "$out"; }; done
CLEAN: .
CLEAN: adapter/database/sql/harness
CLEAN: adapter/database/sql/postgres
CLEAN: adapter/database/sql/mysql
CLEAN: adapter/database/sql/sqlite
CLEAN: adapter/database/sql/dbtest
CLEAN: adapter/cron/crontest

$ grep -rn 'expr-lang' --include='go.mod' --include='go.sum' .
(no output)
```

`go.sum` was checked explicitly, not just `go.mod` — 12 `go.sum` lines went with the 6 `go.mod` lines:

```
$ git diff --stat -- '*go.mod' '*go.sum'
 adapter/cron/crontest/go.mod         | 1 -
 adapter/cron/crontest/go.sum         | 2 --
 adapter/database/sql/dbtest/go.mod   | 1 -
 adapter/database/sql/dbtest/go.sum   | 2 --
 adapter/database/sql/harness/go.mod  | 1 -
 adapter/database/sql/harness/go.sum  | 2 --
 adapter/database/sql/mysql/go.mod    | 5 +----
 adapter/database/sql/mysql/go.sum    | 2 --
 adapter/database/sql/postgres/go.mod | 5 +----
 adapter/database/sql/postgres/go.sum | 2 --
 adapter/database/sql/sqlite/go.mod   | 5 +----
 adapter/database/sql/sqlite/go.sum   | 2 --
 12 files changed, 3 insertions(+), 27 deletions(-)
```

`postgres`/`mysql`/`sqlite` show `5 +----` rather than `1 -` because dropping `expr-lang` left `clockwork`
as the sole indirect requirement, so the `require ( … )` block collapses to a one-liner — a formatting
consequence of the removal, not a dependency change:

```
$ git diff -- adapter/database/sql/postgres/go.mod
-require (
-	github.com/expr-lang/expr v1.17.8 // indirect
-	github.com/jonboulle/clockwork v0.5.0 // indirect
-)
+require github.com/jonboulle/clockwork v0.5.0 // indirect
```

### F12.2 — `golangci-lint` regressed 0 → 3 (CI BLOCKER)

Commit `b6ce7bb` introduced `TestDirectChannel_SubscriptionLifecycle`, whose table `run` closure returned
`(err error, delivered int)` — error first. `staticcheck` ST1008 fired on all four closure literals'
declaration sites (3 reported).

**Fix:** reorder to `(delivered int, err error)`; the four closure literals and the one call site follow.
**No case's assertion changed** — the `assert` closures are byte-identical.

**One non-obvious hazard the reorder introduces, and how it was avoided.** The naive reorder of

```go
return dc.Send(t.Context(), msgin.New[any]("after-cancel")), delivered
```

is `return delivered, dc.Send(…)` — which is **wrong**. Go orders only *function calls* left-to-right within
a return statement; a plain variable read is unordered relative to them (spec, "Order of evaluation"). Two
of the four cases increment `delivered` from **inside the handler**, i.e. during `dc.Send`, so reading
`delivered` in the first return slot may observe the pre-Send value and silently turn a `1` assertion into
`0`. Both such cases were written as an explicit two-statement sequence instead:

```go
sendErr := dc.Send(t.Context(), msgin.New[any]("after-cancel"))
return delivered, sendErr
```

The two cases that return a literal `0` have no such dependency and were reordered inline.

```
$ golangci-lint run ./...
0 issues.
```

### F12.3 — `IsPermanent` / `RetryAfterOf` / `NewID` had zero tests (DELIVERY BLOCKER)

The audit's grep reproduced exactly:

```
$ grep -rn 'IsPermanent\|RetryAfterOf' --include='*_test.go' .
(no output)
```

Note `reliability_test.go` **did** already exist — it covered the *writer* half (`Permanent`,
`RetryAfter`) and not the *reader* half. Both readers were exported by this branch (Task 3.5) and are
public error-classification surface, so CLAUDE.md's hard rule applies: every typed-error branch needs a
covering test.

**Fix:** appended `TestIsPermanent`, `TestRetryAfterOf`, and `TestNewID` to `reliability_test.go`
(`package msgin_test`, assert-closure table form, `t.Context()` n/a — these are pure functions).

- `TestIsPermanent` — 9 cases: nil · `Permanent(err)` · `Permanent` under an outer wrap · each of
  `ErrPayloadType`, `ErrPayloadDecode`, `ErrPayloadTooLarge` · a **wrapped** sentinel (proves the
  `errors.Is` traversal) · `ErrHandlerPanic` (must be **false** — the doc says a recovered panic is
  retried) · a plain transient error.
- `TestRetryAfterOf` — 6 cases: nil · unmarked · `RetryAfter(cause, 30s)` · a **wrapped** marked error ·
  negative duration · zero duration.
- `TestNewID` — 32 chars, lowercase-hex shape, two calls differ.

**On the negative-duration contract:** the implementation was read before the assertion was written.
`RetryAfter(err, d)` normalizes `d < 0` to `0` at construction (`reliability.go:95-97`) and documents it as
*"normalized to 0 (meaning 'no server-instructed floor') rather than rejected"*. The test therefore asserts
`ok == true, d == 0` — the marker survives, the floor is zero. The pre-existing
`TestRetryAfter/"negative delay is normalized, still wraps"` case **could not observe this** (it has no way
to read the stored delay back); `TestRetryAfterOf` is what actually pins it.

Also note the argument order is `RetryAfter(err error, d time.Duration)`, not `RetryAfter(d, err)` as the
audit brief wrote it.

```
$ go test -run 'TestIsPermanent|TestRetryAfterOf|TestNewID' . -race -v | tail -3
--- PASS: TestIsPermanent (0.00s)
PASS
ok  	github.com/kartaladev/msgin	1.486s
```

`IsPermanent` is now 100%, including the previously-uncovered `err == nil` arm:

```
$ go tool cover -func=/tmp/msgin-cov.out | grep 'msgin/reliability.go'
github.com/kartaladev/msgin/reliability.go:26:	Permanent	100.0%
github.com/kartaladev/msgin/reliability.go:38:	IsPermanent	100.0%
github.com/kartaladev/msgin/reliability.go:91:	RetryAfter	100.0%
github.com/kartaladev/msgin/reliability.go:107:	RetryAfterOf	100.0%

$ # block-level proof for reliability.go:39 specifically (the `if err == nil` body)
$ grep 'reliability.go:39\.' /tmp/msgin-cov.out | sort -u
github.com/kartaladev/msgin/reliability.go:39.16,41.3 1 0
github.com/kartaladev/msgin/reliability.go:39.16,41.3 1 1     <- covered
```

### F12.4 — root's `boxMessage` and `nilFuncStep` deleted (confirms F11.6)

F11.6 reproduced. Every consumer moved to a subpackage and carries its own inlined copy
(`endpoint/helpers.go`, `routing/helpers.go`, `transform/transformer.go` — Spec 014 §3.3), leaving root's
originals with zero users. `.golangci.yml` sets `linters.default: none`, so `unused` is off and nothing
reported them; they were 4 of the 11 uncovered blocks.

**Verified zero root-package users before deleting** — every surviving hit is a subpackage-local
definition or its call:

```
$ grep -rn '\bboxMessage\b' --include='*.go' .
payload.go:30:func boxMessage[T any](m Message[T]) Message[any] {     <- root, definition only
endpoint/gateway.go:71 · endpoint/activator.go:29 · endpoint/helpers.go:12 (def)
routing/splitter.go:57 · routing/aggregator.go:291 · routing/helpers.go:14 (def)
transform/transformer.go:29 · transform/transformer.go:47 (def)

$ grep -rn '\bnilFuncStep\b' --include='*.go' .
handler.go:66:func nilFuncStep() Step {                                <- root, definition only
endpoint/activator.go:17,40 · endpoint/helpers.go:19 (def)
routing/filter.go:29 · routing/splitter.go:32 · routing/helpers.go:21 (def)
transform/transformer.go:17 · transform/transformer.go:36 (def)
```

Deleted `payload.go:28-32` and `handler.go:62-70` (each with its doc comment). `ErrNilFunc` (`errors.go:142`)
is exported public error contract and **stays** — it is what the subpackage copies return.

Root is now **14** non-test files still; `go build ./... && go vet ./...` clean (F12.7).

### F12.5 — five `doc.go` files added (closes F11.8, Spec 014 §3.5)

F11.8 reproduced: none of `endpoint`, `routing`, `transform`, `channel`, `resilience` had a package
comment. `ST1000` is disabled under `linters.default: none`, so nothing reported it.

Each new `doc.go` names its EIP chapter and its Spring counterpart as §3.5 requires:

| package | EIP chapter | Spring counterpart |
|---|---|---|
| `endpoint` | ch.10 Message Endpoint | `org.springframework.integration.endpoint` |
| `routing` | ch.7 Message Routing | `org.springframework.integration.router` |
| `transform` | ch.8 Message Transformation | `org.springframework.integration.transformer` |
| `channel` | ch.3 Point-to-Point + ch.4 Publish-Subscribe | `org.springframework.integration.channel` |
| `resilience` | **none — stated explicitly** | **none — stated explicitly** |

`resilience`'s doc says outright that it has neither, explains why (resilience is not an EIP concern, and
Spring Integration delegates it to Spring Retry / Resilience4j), and cites ADR 0006 instead of inventing a
chapter — per round-2 §D15.

Each doc also discharges CLAUDE.md's **multi-instance awareness** rule for the components it introduces:
`endpoint` states that `ChannelExchange` correlates replies in-process only and names **Return Address** as
the pattern a distributed deployment needs (ADR 0022); `channel` states that every channel here is
in-process only and names broker consumer groups / native topics as the crossing mechanism; `routing`
states that the Aggregator holds no state itself, so its topology is decided entirely by the injected
`msgin.MessageGroupStore` (adapter/memory = in-process, adapter/database/sql = durable + multi-process).

Every structural claim in the five docs was checked against the code before it was written
(`msgin.EventDrivenSource`/`PollingSource`/`Delivery`/`RequestReplyExchange`/`Step`/`Chain` in `spi.go` +
`handler.go`; `MessageGroupStore` — **not** `GroupStore`, which is the *adapter* type name; `Aggregator` is
a `MessageHandler`, **not** a `Step`; `ExponentialBackoff`'s jitter is **optional**
(`RandomizationFactor` defaults to 0), so the doc says so).

```
$ for p in . endpoint routing transform channel resilience; do
    echo "$p: $(grep -l '^// Package ' $p/*.go 2>/dev/null | wc -l)"; done
.: 1
endpoint: 1
routing: 1
transform: 1
channel: 1
resilience: 1
```

**Spec 014 §3.5's last bullet is WRONG and needs an edit.** It asserts *"a duplicate after a merge is a
`go vet` failure"*. `go vet` does **not** check for duplicate package comments — round-3 proved this by
execution, and the count-by-file command above is the only mechanical check available while `ST1000`
stays disabled. Flagged, not fixed: §3.5 is a spec edit, outside this task's scope.

### F12.6 — article agreement, corrected in the right direction (R3-10)

The earlier sweep was wrong in **both** directions. `routing` and `transform` are consonant-initial, so
"a routing.X" / "a transform.X" was already correct English and needed no change; the real surviving
defects were all on the `msgin` side ("an msgin." — `msgin` is consonant-initial too). Three occurrences,
all fixed to "a msgin.":

```
$ git diff --stat -- adapter/http/doc.go adapter/http/stdlib/inbound_test.go
 adapter/http/doc.go                 | 2 +-
 adapter/http/stdlib/inbound_test.go | 4 ++--
 2 files changed, 3 insertions(+), 3 deletions(-)
```

- `adapter/http/doc.go:170` — "an msgin.EventDrivenSource" → "a msgin.EventDrivenSource"
- `adapter/http/stdlib/inbound_test.go:35` — "an msgin.MessageChannel" → "a msgin.MessageChannel"
- `adapter/http/stdlib/inbound_test.go:46` — "an msgin.RequestReplyExchange" → "a msgin.RequestReplyExchange"

The two-directional sweep is now empty:

```
$ grep -rnE '\b[Aa] endpoint\.|\b[Aa]n (msgin|routing|transform|channel|resilience)\.' --include='*.go' .
(no output)
```

### F12.7 — the green gate after all six fixes

```
$ go build ./... && go vet ./... && gofmt -l . && golangci-lint run ./...
(build ok) (vet ok) (gofmt: no files) 0 issues.

$ for d in . adapter/database/sql/{harness,postgres,mysql,sqlite,dbtest} adapter/cron/crontest; do
    (cd "$d" && GOWORK=off go build ./... >/dev/null 2>&1 && GOWORK=off go vet ./... >/dev/null 2>&1 \
      && echo "GREEN: $d") || echo "RED: $d"; done
GREEN: .
GREEN: adapter/database/sql/harness
GREEN: adapter/database/sql/postgres
GREEN: adapter/database/sql/mysql
GREEN: adapter/database/sql/sqlite
GREEN: adapter/database/sql/dbtest
GREEN: adapter/cron/crontest
```

Full `-race -shuffle=on`, all seven modules, Docker-backed runners executed for real:

```
$ GOWORK=off go test ./... -race -shuffle=on
ok  github.com/kartaladev/msgin                      1.397s
ok  github.com/kartaladev/msgin/adapter/cron         3.767s
ok  github.com/kartaladev/msgin/adapter/database/sql 2.071s
ok  github.com/kartaladev/msgin/adapter/http         2.756s
ok  github.com/kartaladev/msgin/adapter/http/stdlib  2.622s
ok  github.com/kartaladev/msgin/adapter/memory       3.167s
ok  github.com/kartaladev/msgin/channel              3.441s
ok  github.com/kartaladev/msgin/endpoint             5.136s
ok  github.com/kartaladev/msgin/resilience           4.612s
ok  github.com/kartaladev/msgin/routing              4.468s
ok  github.com/kartaladev/msgin/transform            5.002s

$ # satellites, standalone, as CI runs them
harness  [no test files]
postgres ok   1.421s
mysql    ok   1.391s
sqlite   ok   1.420s
crontest ok  47.968s   (Docker)
dbtest   ok 111.027s   (Docker)
```

### F12.8 — coverage, `-coverpkg=./...` (like-for-like with F11.11)

```
$ GOWORK=off go test ./... -coverpkg=./... -coverprofile=/tmp/msgin-cov.out
$ go tool cover -func=/tmp/msgin-cov.out | tail -1
total:	(statements)	93.4%
```

**93.4%**, against the F11.11 post-migration figure of **93.2%** and the pre-migration baseline of
**91.9%** — a +0.2pt gain from F12.3's new tests and F12.4's dead-code deletion (4 uncovered blocks
removed). As F11.11 established, a *default*-profile per-package comparison across the package split is
not like-for-like and fails the gate falsely; `-coverpkg=./...` on both sides is the only valid
measurement.

### F12.9 — incidental: two untracked tool files were unformatted

`gofmt -l .` flagged `docs/plans/027-tools/{decls.go,qualify.go}` — the derivation tools copied into the
repo by an earlier task, still untracked. They carry `//go:build ignore` and are in no package
(`go list ./docs/...` matches nothing), so they never broke a build — but CI's `gofmt -l` walks the whole
tree and would have flagged them. Formatted in place; `gofmt -l .` is now empty.

### F12.10 — nothing in the round-3 list turned out to be a non-defect

All six findings reproduced exactly as reported. The only corrections are to the *brief's* framing, not to
the findings: (a) `reliability_test.go` already existed and was extended rather than created; (b)
`RetryAfter`'s signature is `(err, d)`, not `(d, err)`; (c) the ST1008 reorder carries a Go
evaluation-order hazard the brief did not anticipate (F12.2). One **new** artifact defect was surfaced:
Spec 014 §3.5's `go vet` claim (F12.5).

---

## F13 — round-3 doc corrections

The round-3 adversarial audit (3 independent Opus auditors) returned **NEEDS-REVISION 3/3**. The *generated*
tables verified perfectly — all 80 §3.2 declaration rows, the §4.1 apidiff partition, the file counts. **Every
surviving defect was in hand-written prose, or in a command that was pasted but never run.** F12 records the
six code fixes; F13 records the document fixes. Same contract as F0–F12: a number with no pasted command is
worthless. Toolchain `GOTOOLCHAIN=go1.25.12`, `PATH="$(go env GOPATH)/bin:$PATH"`. Doc-only — **no `.go` file
was modified by this pass**.

### F13.0 — the governing rule behind almost every round-3 defect

> **A number or command pinned to an intermediate state — one task's commit, the derivation working tree, the
> root module — and then presented as a property of the finished branch.**

Four instances, each a *true* measurement of the wrong thing:

| Claim | What was actually measured | Scope it was presented as |
|---|---|---|
| "the adapter blast radius is 28 files / ±181" | `git diff --stat -- adapter/` when `HEAD` was `c83dde9` | the whole window |
| "`expr-lang` dropped cleanly / verified clean" | `go mod tidy` in the **root** module | all seven modules |
| coverage "BASELINE 93.23%" | the **post-extraction** tree | pre-refactor |
| "no `Err*` var is declared in any other file in the workspace" | `decls .` — the **root** package | the workspace |

**Adopted, and now Plan 027 Global Constraint 0:** *every pasted command carries its explicit commit range and
module scope.* A `git diff` names its range in the command; a per-module fact is shown in the loop form for
every module; a coverage figure names its tree **and** its profile mode; "verified"/"clean" with no pasted
output is not a claim.

### F13.1 — the acyclicity gate could never pass (published in FOUR places)

`go list -deps` **includes its argument packages**, so the form published in Spec §3, Spec §9.1 AC-1, Plan
Global Constraint 6, and Plan Task 12 prints five lines on a *correct* tree and six on a broken one — it could
neither pass nor distinguish the two:

```
$ go list -deps ./endpoint ./routing ./transform ./channel ./resilience | grep 'kartaladev/msgin/'
github.com/kartaladev/msgin/endpoint
github.com/kartaladev/msgin/routing
github.com/kartaladev/msgin/transform
github.com/kartaladev/msgin/channel
github.com/kartaladev/msgin/resilience
```

**The invariant itself holds.** The corrected gate, run at HEAD, is empty:

```
$ go list -deps ./endpoint ./routing ./transform ./channel ./resilience \
    | grep 'kartaladev/msgin/' \
    | grep -vE '^github.com/kartaladev/msgin/(endpoint|routing|transform|channel|resilience)$'
(no output, exit 1)
$ go list -deps . | grep 'kartaladev/msgin/'
(no output, exit 1)
```

Fixed in all four places.

### F13.2 — `expr-lang`: a root-only measurement stated workspace-wide

F6's *"`expr-lang` drops cleanly"* and Plan Task 1's *"ran `go mod tidy` in all seven modules"* were both
false for six of seven modules until F12.1. Restated with the per-module result:

```
$ for d in . adapter/database/sql/{harness,postgres,mysql,sqlite,dbtest} adapter/cron/crontest; do
    out=$(cd "$d" && GOWORK=off go mod tidy -diff 2>&1)
    [ -z "$out" ] && echo "CLEAN: $d" || { echo "DIRTY: $d"; echo "$out"; }; done
CLEAN: .
CLEAN: adapter/database/sql/harness
CLEAN: adapter/database/sql/postgres
CLEAN: adapter/database/sql/mysql
CLEAN: adapter/database/sql/sqlite
CLEAN: adapter/database/sql/dbtest
CLEAN: adapter/cron/crontest

$ grep -rn 'expr-lang' --include='go.mod' --include='go.sum' .
(no output, exit 1)
```

### F13.3 — the adapter inventory re-derived over the whole window · **SIX of seven rows were stale**

Spec §3.6's table was a `c83dde9`-only snapshot. Re-derived with the range **in** the command:

```
$ git diff --stat c83dde9~1..HEAD -- adapter/ | tail -1
 31 files changed, 239 insertions(+), 191 deletions(-)
```

| module / package | files | +/− | was published as | stale? |
|---|--:|---|---|---|
| `adapter/cron` | 3 | +8 −7 | 2 / +4 −3 | **yes** |
| `adapter/database/sql` | 7 | +15 −14 | 7 / +15 −14 | no |
| `adapter/database/sql/harness` | 6 | +93 −77 | 6 / +80 −73 | **yes** |
| `adapter/database/sql/postgres` | 1 | +8 −6 | 1 / +7 −5 | **yes** |
| `adapter/http` | 11 | +86 −69 | 10 / +63 −55 | **yes** |
| `adapter/http/stdlib` | 1 | +25 −14 | 1 / +11 −9 | **yes** |
| `adapter/memory` | 2 | +4 −4 | 1 / +1 −1 | **yes** |

*(The task brief estimated "5 of 7 stale"; the measurement says **6 of 7** — only `adapter/database/sql` was
unchanged between `c83dde9` and HEAD. Recorded because an estimate corrected by measurement is the point of
this ledger.)*

The three files the snapshot could not see — `adapter/cron/source.go`, `adapter/http/sseclient.go`,
`adapter/memory/memory.go` — all carry the `StreamingSource` → `EventDrivenSource` rename from `b6ce7bb`.
ADR 0027's Context and Consequences repeated the stale trio and are corrected there too.

### F13.4 — the coverage "baseline" was the post-extraction tree

```
$ git archive ab233d9 | tar -x -C <tmp>/ab233d9
$ (cd <tmp>/ab233d9 && GOWORK=off go test ./... -count=1 -coverpkg=./... -coverprofile=ab.cov \
     && go tool cover -func=ab.cov | tail -1)
total:  (statements)  93.5%          <- TRUE pre-refactor baseline

$ git archive b6ce7bb | tar -x -C <tmp>/b6ce7bb
$ (cd <tmp>/b6ce7bb && GOWORK=off go test ./... -count=1 -coverpkg=./... -coverprofile=b6.cov \
     && go tool cover -func=b6.cov | tail -1)
total:  (statements)  93.3%

$ GOWORK=off go test ./... -count=1 -coverpkg=./... -coverprofile=head.cov \
    && go tool cover -func=head.cov | tail -1
total:  (statements)  93.4%          <- HEAD, after the F12 code fixes
```

| Tree | What it is | `-coverpkg=./...` |
|---|---|--:|
| `ab233d9` | **pre-refactor** | **93.5%** |
| `c83dde9` | post-extraction | 93.2% |
| `b6ce7bb` | post-segregation | 93.3% |
| **HEAD** | after F12 | **93.4%** |

**The honest whole-window statement is 93.5% → 93.4%, a −0.1pt movement** — not F12.8's "+0.2pt gain", which
compared a `-coverpkg` number against a *default*-profile 91.9%, and not F10.8's "BASELINE 93.23%", which was
the post-extraction tree wearing the pre-refactor label. The −0.1pt is `endpoint/consumer.go:467`'s race arm
(F13.6), not a regression.

### F13.5 — apidiff re-verified at HEAD: 95 / 5, and the prose said 93 / 86

Spec §4.1's prose read *"the **93** decompose as"* and *"`WithReleaseStrategy` is in the **86**"*, sitting
directly above a table that sums to **95 / 87**; ADR 0027's Consequences repeated the 93. Re-derived at HEAD
against the **committed** baseline:

```
$ apidiff docs/plans/027-root-api-baseline.txt . | grep -c ': removed'
      95
$ apidiff docs/plans/027-root-api-baseline.txt . | awk '/^Compatible/{f=1;next}f'
- EventDrivenSource: added
- IsPermanent: added
- NewID: added
- RetryAfterOf: added
- SubscribableChannel: added
```

**Deleting root's `boxMessage`/`nilFuncStep` (F12.4) did not move the surface — verified, not assumed.** Both
were unexported. The other two Task-12 assertions were re-checked at HEAD as well:

```
$ go run docs/plans/027-tools/decls.go . | grep -v '_test\.go' \
    | awk -F'\t' '$5=="exported" && $3!="method" {print $4}' | sort -u | wc -l
     101
$ go run docs/plans/027-tools/decls.go . | grep -v '_test\.go' \
    | awk -F'\t' '$3=="var" && $4 ~ /^Err/ {print $1}' | sort | uniq -c
  42 errors.go
$ ls *.go | grep -v _test.go | wc -l
      14
```

Partition re-verified against a regenerated `symmap.tsv` (91 symbols):

```
$ comm -12 <(sort removed.txt) <(cut -f2 docs/plans/027-tools/symmap.tsv | sort -u) | wc -l   # 87 relocated
$ grep -cE '^(FilterExpr|RouterExpr|TransformExpr|SplitExpr|WithCorrelationExpr|WithReleaseExpr)$' removed.txt  # 6
$ grep -c '^StreamingSource$' removed.txt ; grep -c '^MessageChannel\.Subscribe$' removed.txt   # 1  1
$ comm -23 <(sort removed.txt) <(cut -f2 docs/plans/027-tools/symmap.tsv | sort -u) \
    | grep -vE '^(FilterExpr|RouterExpr|TransformExpr|SplitExpr|WithCorrelationExpr|WithReleaseExpr|StreamingSource|MessageChannel\.Subscribe)$'
(no output)
```

87 + 6 + 1 + 1 = 95, empty residual.

**All three numbers are CONTINGENT** on Plan Task 9.5's undecided sentinel question (F13.11).

### F13.6 — uncovered blocks: 11 claimed as 6; enumerated properly, 11 → 6

F10.8 named six uncovered blocks. A full max-deduplicated enumeration of the six core packages at `b6ce7bb`
finds **ten stable** plus the flaky `consumer.go:467` arm — eleven in a run where the race arm misses:

```
$ awk 'NR>1 { if ($3+0 > m[$1]) m[$1]=$3+0 } END { for (k in m) if (m[k]==0) print k }' b6ce7bb.cov \
    | grep -E '^github\.com/kartaladev/msgin/(endpoint|routing|transform|channel|resilience)/|^github\.com/kartaladev/msgin/[a-z_]+\.go' | sort
github.com/kartaladev/msgin/endpoint/gateway.go:30.27,32.3
github.com/kartaladev/msgin/endpoint/nativereliability.go:9.52,9.68
github.com/kartaladev/msgin/endpoint/poller.go:152.11,153.80
github.com/kartaladev/msgin/endpoint/poller.go:164.12,166.3
github.com/kartaladev/msgin/handler.go:66.25,67.45
github.com/kartaladev/msgin/handler.go:67.45,68.64
github.com/kartaladev/msgin/handler.go:68.64,68.85
github.com/kartaladev/msgin/payload.go:30.51,32.2
github.com/kartaladev/msgin/reliability.go:39.16,41.3
github.com/kartaladev/msgin/resilience/breaker.go:179.28,181.3
```

**Five were fixed by F12** — the three `nilFuncStep` blocks + one `boxMessage` block (F12.4, confirming its
"4 of the 11" count exactly) and `reliability.go:39`, `IsPermanent`'s nil arm (F12.3). The same command at
HEAD:

```
github.com/kartaladev/msgin/endpoint/consumer.go:467.20,469.15    <- flaky race arm
github.com/kartaladev/msgin/endpoint/gateway.go:30.27,32.3
github.com/kartaladev/msgin/endpoint/nativereliability.go:9.52,9.68
github.com/kartaladev/msgin/endpoint/poller.go:152.11,153.80
github.com/kartaladev/msgin/endpoint/poller.go:164.12,166.3
github.com/kartaladev/msgin/resilience/breaker.go:179.28,181.3
```

**Six remain, and root now has zero.** Spec §9 AC 7 carries the enumeration with a per-block disposition
instead of the two-item summary.

### F13.7 — the five test fakes were **NOT** deleted; their `Subscribe` stubs were

Spec §5.3, ADR 0028 Consequences, Plan Task 2 and F10.6 all claimed *"five test fakes were deleted, not
migrated"*. **All five survive.**

```
$ git grep -n 'func (.*) Subscribe(.*msgin.MessageHandler) error' ab233d9 -- '*_test.go'
ab233d9:aggregator_settlement_test.go:35:func (c *idsAggChannel) Subscribe(msgin.MessageHandler) error { return nil }
ab233d9:aggregator_test.go:37:           func (c *fakeAggChannel) Subscribe(msgin.MessageHandler) error { return nil }
ab233d9:aggregator_test.go:208:          func (c *failNthChannel) Subscribe(msgin.MessageHandler) error { return nil }
ab233d9:exchange_test.go:721:            func (c *scriptedChannel) Subscribe(_ msgin.MessageHandler) error { return nil }
ab233d9:expr_test.go:35:                 func (c *collector) Subscribe(msgin.MessageHandler) error { return nil }

$ grep -rn --include='*_test.go' 'Subscribe(.*MessageHandler) error' .
(no output, exit 1)                                    # all five STUBS gone

$ # all five FAKES still present, moved with their tests:
routing/aggregator_test.go:22             type fakeAggChannel struct {
routing/aggregator_test.go:157            type failNthChannel struct {
routing/aggregator_settlement_test.go:24  type idsAggChannel struct {
endpoint/gateway_test.go:19               type collector struct{ got []msgin.Message[any] }
endpoint/exchange_test.go:811             type scriptedChannel struct {
```

The conclusion — *five of the six `MessageChannel` implementations in the test suite never wanted
`Subscribe`* — is **sound and better evidenced by the stub deletion**, which isolates the unwanted method.
This also resolves a contradiction **inside Spec 014**: §3.4c recorded `collector` as **re-declared** (it is,
at `endpoint/gateway_test.go:19`) while §5.3 called it deleted. §3.4c was right.

### F13.8 — "no `Err*` var is declared in any other file in the workspace" · **there are 51**

Spec §3.2's design rationale (*"one import for the whole error contract beats six"*) rested on this, and the
repository's own precedent argues the other way:

```
$ for d in . endpoint routing transform channel resilience adapter/memory adapter/cron adapter/http \
       adapter/http/stdlib adapter/database/sql adapter/database/sql/{harness,postgres,mysql,sqlite,dbtest} \
       adapter/cron/crontest; do
    go run docs/plans/027-tools/decls.go $d | grep -v '_test\.go' \
      | awk -F'\t' -v D="$d" '$3=="var" && $4 ~ /^Err/ {print D"/"$1}'; done | sort | uniq -c
  42 ./errors.go
  26 adapter/http/errors.go
  15 adapter/database/sql/errors.go
   9 adapter/cron/errors.go
   1 adapter/cron/sqlutil.go
```

The 42 figure was always root-scoped. **Replaced with the rule that is actually true:** *a package owns its
own sentinels when callers import it directly; a package whose only import is root shares root's closed error
contract.* That is exactly why `adapter/http` (26), `adapter/database/sql` (15) and `adapter/cron` (9+1) each
own an `errors.go` while the five core subpackages own none — and the check is scoped to those five, not the
workspace.

### F13.9 — "a duplicate package comment is a `go vet` failure" · **FALSE, proven by execution**

A second `// Package transform` comment was planted in a throwaway file and every tool in the gate passed it:

```
$ cat > transform/zz_dup_doc_probe.go <<'X'
// Package transform is a duplicate package comment planted to test go vet.
package transform
X
$ go build ./transform/      ; echo "exit=$?"          -> exit=0
$ go vet ./transform/        ; echo "exit=$?"          -> exit=0
$ gofmt -l transform/        ; echo "exit=$?"          -> (empty) exit=0
$ golangci-lint run ./transform/                       -> 0 issues.
$ go doc ./transform | head -3                         -> renders, no warning
```

`ST1000` would not help either: it is off under `.golangci.yml`'s `linters.default: none`, and it checks for a
*missing* comment, not a duplicate. **The counting assertion is the only one that fails:**

```
$ for p in . endpoint routing transform channel resilience; do
    n=$(grep -l '^// Package ' $p/*.go 2>/dev/null | wc -l | tr -d ' ')
    [ "$n" = 1 ] || { echo "FAIL $p has $n"; exit 1; }
  done
FAIL transform has 2                                   # with the probe present
(silent, exit 0)                                       # probe removed — all six packages have exactly one
```

The probe was deleted; `gofmt -l .` and `go vet ./transform/` clean again. The false claim appeared in Spec
§3.5, Plan Global Constraint 3, and Plan Task 11's Verify; all three now carry the counting assertion.

### F13.10 — Task 10's provider signatures did not compile

The plan and Spec §7 specified **non-generic** `Correlation`/`Release`/`RouteFunc`. Every deleted original was
`[A any]`:

```
$ git show ab233d9:expr.go | grep -nE '^func '
 35:func compile[A any](expression string, kind exprOutputKind) (func(Message[A]) (any, error), error)
 89:func FilterExpr[A any](expression string, opts ...FilterOption) (Step, error)
115:func RouterExpr[A any](keyExpr string, routes map[string]MessageChannel, opts ...RouterOption) (*Router, error)
167:func TransformExpr[A, B any](expression string) (Step, error)
217:func SplitExpr[A, B any](expression string) (Step, error)
262:func compileGroup[A any](expression string) (func(groupExprEnv[A]) (any, error), error)
277:func toGroupEnv[A any](g MessageGroup) (groupExprEnv[A], error)
321:func WithCorrelationExpr[A any](expression string) AggregatorOption
390:func WithReleaseExpr[A any](expression string) AggregatorOption
```

`A` is load-bearing twice, and both uses are things the bundle elsewhere insists on:

1. **`compile[A]` (`:35`) type-checks `payload.Field` against `A`** — what makes ADR 0019's
   *fail-at-construction* contract real rather than nominal.
2. **`PayloadOf[A]` IS the M-6 `ErrPayloadType` branch** Task 10 mandates —
   `git show ab233d9:expr.go | grep -n 'PayloadOf\[A\]'` → `:129, :224, :284, :331`.

Corrected to `Correlation[A any]`, `Release[A any]`, `RouteFunc[A any]` in **both** Plan Task 10 and Spec §7
(and ADR 0029 §5a), noting that `A` is **not inferable from a `string`**, so callers instantiate explicitly:
`expr.Release[Order]("…")`.

### F13.11 — Task 12's numbers are contingent on an undecided question

`ErrInvalidExpression` (`errors.go:161`) and `ErrExprResultType` (`errors.go:183`) are orphaned with zero
users (F11.7), and Task 9.5 lists deciding their fate as its *third* bullet while Task 12 asserts 101 / 42 /
apidiff-95 as fixed numbers.

| Option | root exported | root sentinels | apidiff removals |
|---|--:|--:|--:|
| **A — keep in root** (recommended) | 101 | 42 | 95 |
| **B — remove from root** | 99 | 40 | 97 |

The decision is moved to **Task 9.5.0, the task's first action**, and Task 12's assertions are marked
contingent on it. **Task 10 consumes whatever is decided**: `RouteFunc`'s two construction validations wrap
`ErrInvalidExpression`, so option B forces Task 10 to declare the replacement sentinel before it can compile.
**Not decided here** — it is a user decision, and this pass records the trade rather than picking one.

### F13.12 — `Predicate.And/Or/Not` had no nil handling

`p.And(nil)` would panic, contradicting CLAUDE.md's *"must not panic on caller input"* and the package's own
settled convention:

```
$ sed -n '27,30p' routing/filter.go
func Filter[A any](pred func(ctx context.Context, m msgin.Message[A]) (bool, error), opts ...FilterOption) msgin.Step {
	if pred == nil {
		return nilFuncStep()          # -> ErrNilFunc at dispatch
$ grep -n -B2 'ErrNilFunc = ' errors.go
140:	// ErrNilFunc is returned by an endpoint (Transform/Filter/Activate/Consume/
141:	// Router) constructed with a nil function, instead of panicking at dispatch.
142:	ErrNilFunc = errors.New("msgin: nil endpoint function")
```

A nil receiver is equally reachable — `var p routing.Predicate[T]` is nil and `p.Or(q)` is legal Go.
**Specified:** a nil receiver *or* a nil argument yields a `Predicate[A]` returning
`(false, msgin.ErrNilFunc)` at evaluation (combinators are pure and return a `Predicate`, not
`(Predicate, error)`), reusing the existing sentinel. **The nil check precedes the short-circuit** — `p.Or(nil)`
must surface `ErrNilFunc` even when `p` is true. Three branches added to Task 9's enumeration.

### F13.13 — the capability test covers 7 of 8 send-only positions; the omitted one is row 3

```
$ grep -nE 'name: "' capability_test.go
125:  name:  "QueueChannel"
133:  name:  "PublishSubscribeChannel"
141:  name:  "OutboundAdapter/memory.Broker"
152:  name: "filter discard channel"
163:  name: "router default channel"
174:  name: "exchange request channel"
```

**3 targets × 3 sites = 9 subtests**, against Spec §9.4's eight positions. Task 9.5's own table listed **four**
missing and its Verify line said *"3 targets × 5 core sites plus the two HTTP sites"* — 7 of 8 — two lines
after asserting all eight were required. **The omitted position is `routing.NewRouter`'s `pick` return**
(`routing/router.go:29,37`), the only one of the eight where the destination is chosen at **message time** by
*caller-supplied* code, i.e. precisely the widening ADR 0028 exists for. Verify becomes **3 × 6 core (in
`capability_test.go`) + 3 × 2 HTTP (in `adapter/http`, `adapter/http/stdlib`) = 24 subtests**.

### F13.14 — Task 10's acceptance material does not exist in the ledger

Plan Task 10 said to reinstate the deleted `*Expr` test cases *"from the ledger, all present today"*. The
ledger holds **two table rows** (M-1, M-6, in F3). **None of the twelve deleted test functions is recorded
anywhere under `docs/`:**

```
$ git show ab233d9:expr_test.go | grep -nE '^func (Test|Example)'
 45:TestFilterExpr          141:TestFilterExpr_Concurrent   207:ExampleFilterExpr
233:ExampleRouterExpr       251:TestTransformExpr           326:ExampleTransformExpr
348:TestSplitExpr           459:ExampleSplitExpr            483:TestRouterExpr
589:TestWithCorrelationExpr 676:TestWithReleaseExpr         843:ExampleWithReleaseExpr
```

`git show ab233d9:expr_test.go` and `git show ab233d9:expr.go` are now named as the parity source of truth in
Task 10, and the false *"all present today"* is struck from both Task 10 and the §Ledger contents list.
*(`docs/plans/014-expr-endpoints.md` and `docs/plans/018-expr-sugar.md` carry partial skeletons; useful as a
cross-check on intent, not as the parity bar — they predate several revisions of the cases.)*

### F13.15 — tooling paths: no gate may depend on `/tmp`

`decls`/`qualify` are committed at `docs/plans/027-tools/` as `//go:build ignore` programs
(`go run docs/plans/027-tools/decls.go <dir>`), with `symmap.tsv` beside them and the `apidiff` baseline at
`docs/plans/027-root-api-baseline.txt`. Every reference updated — Plan Task 0, Plan Task 12, Spec §3.2, §3.5,
§4, §4.1, §8.1, and `027-derivation-brief.md`, which still pointed at `/tmp/msgin-derive/`.

**`symmap.tsv` was stale by one entry** and regenerated:

```
$ wc -l < docs/plans/027-tools/symmap.tsv          # before
      90
$ for p in endpoint routing transform channel resilience; do
    go run docs/plans/027-tools/decls.go $p | grep -v '_test\.go' \
      | awk -F'\t' -v P=$p '$5=="exported" && $3!="method" {print P"\t"$4}'
  done | sort -u -k2,2 > docs/plans/027-tools/symmap.tsv
$ wc -l < docs/plans/027-tools/symmap.tsv ; cut -f1 docs/plans/027-tools/symmap.tsv | sort | uniq -c
      91
  15 channel   45 endpoint    9 resilience   21 routing    1 transform
```

The missing entry was `channel	WithSingleSubscriber` (D-F, added in `b6ce7bb`). **A derived file committed
without a regeneration step goes stale exactly like a hand-typed number**; the regeneration command is now in
Spec §8.1 and the derivation brief, immediately above the sweep that consumes it.

### F13.16 — Spec §8's nine godoc bullets and §10's four obligations had NO OWNING TASK

Audited per bullet against HEAD, after F12.5's five `doc.go` files. **Six of thirteen unmet.**

| Obligation | Status | Command |
|---|---|---|
| §8.1 Correlation Identifier named | ⚠️ HALF — "Return Address" present, "Correlation Identifier" absent | `grep -rn -i 'correlation identifier' --include='*.go' .` → exit 1 |
| §8.2 `DirectChannel` single-subscriber vs Spring, worker pool | ✅ | `channel/direct.go:15,17` |
| §8.3 `RequestReplyExchange` AMQP disclaimer | ❌ | `grep -rn -i 'amqp' --include='*.go' .` → exit 1, workspace-wide |
| §8.4 behavior types name their Spring equivalent, per type | ❌ for the two shipped | `grep -B8 'type CorrelationStrategy' routing/aggregator.go \| grep -i spring` → exit 1 (same for `ReleaseStrategy`) |
| §8.5 `MessageChannel`/`OutboundAdapter` method-identical by design | ✅ | `channel.go:18-19`, `spi.go:48-51` |
| §8.6 root `doc.go` states Pipes-and-Filters | ✅ | `doc.go:5-14` |
| §8.7 `ServeAsync`/`NewInbound` state the widened `target` | ❌ | `adapter/http/inbound.go:110-116`, `adapter/http/stdlib/inbound.go:32-33` |
| §8.8 `Close` states the post-`Close` reply change | ✅ | `endpoint/exchange.go:363-369` |
| §8.9 `IsPermanent` documents its classifier policy | ✅ | `reliability.go:33-37` |
| §10a channels/correlator in-process, Return Address named | ✅ | `channel.go:33`, `channel/doc.go`, `endpoint/doc.go:19` |
| §10b `PubSub` in-process, `TopicPublisher`/`TopicSubscriber` named | ✅ at package level | `channel/doc.go` |
| §10c `WithSingleSubscriber` is a **single-process** guard | ❌ — ADR 0028 §6.2 requires it verbatim; the godoc never mentions the process boundary | `channel/pubsub.go:69-83` |
| §10d `RetryPolicy.MaxAttempts` is per-instance (`N × MaxAttempts`) | ❌ | `retry.go:37-41`; `grep -rn 'N × MaxAttempts' --include='*.go' .` → exit 1 |

All thirteen are now **Plan 027 Task 11b/11c checkboxes**, each with the grep that proves it. Spec §6 and
ADR 0029 §2 both asserted the AMQP line **already exists**; corrected to an outstanding obligation.

### F13.17 — Plan §Progress and ADR 0027's status line were stale

Both said Tasks 2/3 were "DONE, UNCOMMITTED" and Plan §Progress instructed the next session that *"committing
it is the first action of the resumed plan"*:

```
$ git log --oneline -3
0e2dcf0 docs(027): regenerate the bundle from the verified tree; clear all round-2 banners
b6ce7bb refactor(core)!: segregate MessageChannel; add WithSingleSubscriber; rename StreamingSource
c83dde9 refactor(core)!: extract the flat core into endpoint/routing/transform/channel/resilience
```

`b6ce7bb` **is** Tasks 2 and 3. What is genuinely uncommitted is the F12 code-fix pass plus this F13 doc pass.
Both requoted from `git log`.

### F13.18 — the smaller textual corrections

- **ADR 0028's Topology section ended mid-sentence** — *"…is not reachable through it. **The**"*, orphaned by
  the blockquote that followed. Closed.
- **ADR 0027's Consequences claimed the organising principle "holds without exception"** while §5 and §3.5
  both already record `resilience` as having no EIP chapter. Restated to cover both cases honestly: *every EIP
  pattern lives in the package named for its chapter, and every package not named for a chapter states why it
  has none.*
- **ADR 0028's Consequences framed D-F as improving exclusivity.** Stated plainly instead: it is **off by
  default**, nothing in the library turns it on, `NewChannelExchange` does not opt its reply channel in, so
  **the default wiring is unchanged and still silently mis-routes**.
- **ADR 0027 §5's three `ExponentialBackoff` line cites were off by one/two:**
  ```
  $ grep -rn 'ExponentialBackoff' --include='*.go' adapter/ | grep -v _test.go
  adapter/database/sql/source.go:176:         penalty := resilience.ExponentialBackoff{
  adapter/database/sql/source.go:236:      penalty := resilience.ExponentialBackoff{
  adapter/database/sql/harness/source.go:114:            Backoff: resilience.ExponentialBackoff{Initial: 300 * time.Millisecond, Mult: 1},
  ```
  `175,235`/`112` → **`176,236`/`114`**.

### F13.19 — verification of this pass

```
$ git status --short -- '*.go'
 M adapter/http/doc.go
 M adapter/http/stdlib/inbound_test.go
 M channel/channel_test.go
 M handler.go
 M payload.go
 M reliability_test.go
```

**Exactly the six files F12 already modified; this pass modified no `.go` file.** (The untracked
`docs/plans/027-tools/{decls,qualify}.go` carry `//go:build ignore` and are in no package.)

```
$ for d in . adapter/database/sql/{harness,postgres,mysql,sqlite,dbtest} adapter/cron/crontest; do
    (cd "$d" && GOWORK=off go build ./... >/dev/null 2>&1 && GOWORK=off go vet ./... >/dev/null 2>&1 \
      && echo "GREEN: $d") || echo "RED: $d"; done
GREEN: .
GREEN: adapter/database/sql/harness
GREEN: adapter/database/sql/postgres
GREEN: adapter/database/sql/mysql
GREEN: adapter/database/sql/sqlite
GREEN: adapter/database/sql/dbtest
GREEN: adapter/cron/crontest

$ gofmt -l . ; golangci-lint run ./...
(no files) 0 issues.
```

### F13.20 — what was NOT a defect, and what is left open

**Not defects.** Nothing in the round-3 list turned out to be a non-defect; every item reproduced. Two
*estimates inside the brief* were corrected by measurement, and both are recorded above rather than quietly
adjusted: the adapter table is **6 of 7 rows stale, not 5** (F13.3), and the uncovered-block count is **10
stable + 1 flaky = 11**, of which **5** were fixed, not 4 (F13.6).

**Left open, deliberately, because they are decisions and not transcriptions:**

1. **The two orphaned expr sentinels** (F13.11). Option A recommended, B defensible; **not picked here.**
   Three published numbers (101 / 42 / 95) hang on it.
2. **`docs/plans/027-tools/` is untracked.** The README says "Committed because…", and the gates in Spec §8.1
   and Task 12 now depend on it. It must land in the next commit or those gates break on a fresh clone — the
   exact failure the directory was created to prevent.
3. **`PubSub`'s type-level godoc** (`channel/pubsub_registry.go:10`) still says only "in-process topic
   registry". `channel/doc.go` discharges §10b at package level, so this is polish rather than a gap; recorded
   so the next pass decides rather than rediscovers.

---

### F14 — code-review fixes (M1–M5)

An adversarial code review of the Plan 027 branch returned **REQUEST CHANGES** with five findings. All five
reproduced; all five are fixed. No finding turned out to be a non-defect.

#### F14.1 (M1) — the package split silently dropped `goleak` from five of six test binaries

On `main`, one `TestMain` in the root test binary (`consumer_test.go:23`) governed every core test. Task 4
moved that file to `endpoint/`, so `endpoint` kept the check and **`channel`, `routing`, `transform`,
`resilience` and root `msgin` lost it** — invisible to build, vet, lint and the suite, because a missing
`TestMain` is not an error. It mattered most for `channel`: `queuechannel_e2e_test.go` and
`pubsub_integration_test.go` each start a full `endpoint.Consumer` (poller + worker pool + sweep loop),
exactly the goroutines the check existed to watch.

**Fixed** by adding a per-package `main_test.go` carrying
`func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }` to `channel`, `routing`, `transform`, `resilience`
and root — following the `adapter/database/sql/main_test.go` convention, one file per package, each with a
comment naming what that package's check is actually guarding. `endpoint` was left alone (it already has one;
duplicating would not compile). All six pass — **no package had a real leak hiding behind the missing check.**

Two now-false comments corrected: `channel/pubsub_integration_test.go:51` and
`channel/queuechannel_e2e_test.go:19` both attributed the check to the "root TestMain", which no longer
governs that binary.

#### F14.2 (M2) — `endpoint.pollErrorBackoff`'s cap and loop guard had ZERO coverage while `-cover` read 100%

`endpoint/poller.go:138` reimplements the backoff locally (Spec 014 decision D-A). The arithmetic is correct,
but `poller_test.go`'s `TestPoller_ErrorBackoffAndReset` stopped at three consecutive errors (1s, 2s, 4s) —
nothing reached the 30s cap. Neither `min(d, maxPollErrorBackoff)`'s clamp nor the loop's
`d < maxPollErrorBackoff` early-exit guard ever executed. Statement coverage still read 100% because both arms
live inside already-covered statements, so the number could not see the gap. This is the same class as
[Measure interleaving tests, don't trust them]: a passing, line-covered test that never reaches its target arm.

**Fixed** by converting that test to the mandated assert-closure table form (`TestPoller_ErrorBackoff`) with
two cases, and adding the missing one:

| case | `pollInterval` | schedule | arms reached |
|---|--:|---|---|
| doubles below the cap and resets on a successful poll | 1s | 1s, 2s, 4s, reset, 1s | (the pre-existing coverage) |
| clamps at the 30s cap and stays pinned there | 20s | 20s, **30s**, **30s** | clamp **and** loop guard |

20s is chosen so the doubling overshoots on the *second* consecutive error (`20s → 40s`, clamped to 30s) and
the *third* exits the loop early on the guard (`40s < 30s` is false). The steps advance the fake clock by
**exactly** the expected delay, which is what makes the schedule falsifiable: had the implementation waited
longer, its timer would not fire and the next step's poll-count assertion would fail.

**Probe evidence** — the arms are genuinely exercised, not merely nominally covered. Two temporary panics were
added to `pollErrorBackoff` and each confirmed to fire from the new subtest, then reverted:

```
$ go test ./endpoint/ -run TestPoller_ErrorBackoff -v      # PROBE-A armed (clamp)
=== RUN   TestPoller_ErrorBackoff/doubles_below_the_cap_and_resets_on_a_successful_poll
=== RUN   TestPoller_ErrorBackoff/clamps_at_the_30s_cap_and_stays_pinned_there
panic: PROBE-A: clamp arm (min() actually clamped)
    endpoint.(*consumer[...]).pollErrorBackoff(...) poller.go:148

$ go test ./endpoint/ -run TestPoller_ErrorBackoff -v      # PROBE-B armed (loop guard)
=== RUN   TestPoller_ErrorBackoff/clamps_at_the_30s_cap_and_stays_pinned_there
panic: PROBE-B: loop guard arm (d >= cap exited the loop early)
    endpoint.(*consumer[...]).pollErrorBackoff(...) poller.go:145
```

Note that in both runs the FIRST subtest completed without tripping the probe — the new case is what reaches
the arms. The counterfactual makes that decisive: with **both** probes armed and only the new subtest skipped,
all 11 packages pass, i.e. nothing else in the entire suite reaches either arm.

```
$ go test ./... -skip 'TestPoller_ErrorBackoff/clamps_at_the_30s_cap_and_stays_pinned_there'   # probes armed
ok  github.com/kartaladev/msgin            ok  .../channel     ok  .../endpoint
ok  .../adapter/cron                       ok  .../resilience  ok  .../routing
ok  .../adapter/database/sql               ok  .../transform   ok  .../adapter/memory
ok  .../adapter/http                       ok  .../adapter/http/stdlib
```

Both probes reverted; `git diff endpoint/poller.go` is empty.

#### F14.3 (M3) — two orphaned sentinels whose godoc named six deleted functions

`ErrInvalidExpression` and `ErrExprResultType` have zero producers and zero tests, and their godoc referenced
`FilterExpr` / `RouterExpr` / `TransformExpr` / `SplitExpr` / `WithCorrelationExpr` / `WithReleaseExpr` — all
deleted by this window.

**The `[ ] Decide:` at Task 9.5.0 was NOT resolved here.** Removing the sentinels is the irreversible half of
that decision and belongs to it. The reversible half was taken instead: **both sentinels stay, and their godoc
is rewritten** to describe them as the root error contract's construction-time and evaluation-time expression
faults, returned by the forthcoming separate `msgin/expr` provider module, and explicitly noted as having *no
producer in this module*. The godoc no longer names any deleted symbol, and it no longer implies a producer
that does not exist. **9.5.0 remains open.**

Four other stale doc sites corrected:

- `doc.go:78` — "are provided by the separate msgin/expr module" was present tense for a module that does not
  exist; now clearly forward-looking ("will be supplied by … not yet written"), and it says the Go-func forms
  are the only forms until it ships.
- `routing/aggregator.go:316` — `// release-decision error (e.g. WithReleaseExpr eval)` → names the
  `WithRelease` strategy instead.
- `routing/splitter.go:52` — `forwardSplit`'s "Shared by Split and SplitExpr" → "the shared forwarding core
  every Split constructor delegates to".
- `routing/aggregator_test.go:1276` — same `WithReleaseExpr` reference in `TestAggregator_ReleaseErrorDrain`
  `CheckError`'s doc comment (found by grep; not in the review's list, same defect).
- `CLAUDE.md:235` — the Dependency policy still listed `github.com/expr-lang/expr` as one of **three** accepted
  core dependencies "for `FilterExpr`/`RouterExpr` in the pattern core". `go.mod` has no such require. Now
  reads **two** exceptions, with an explicit paragraph recording that expr-lang **left the core in this
  window**, that the future `expr` module owns it, and that only the two sentinels remain behind.

After the sweep, `grep -rn 'FilterExpr|RouterExpr|TransformExpr|SplitExpr|WithCorrelationExpr|WithReleaseExpr|
expr-lang' --include='*.go' .` returns nothing.

#### F14.4 (M4) — `ChannelExchange.Close()` nil-dereferenced on caller input

`NewChannelExchange` stored whatever `reply.Subscribe` returned without checking, and `Close` called
`e.replySub.Cancel()` unconditionally. `SubscribableChannel` is public SPI explicitly intended for third-party
adapters, so an implementation returning `(nil, nil)` is **caller input** — and it panicked, violating
CLAUDE.md's "must not panic on caller input (return errors instead)".

**Fixed at construction, not by making `Close` nil-tolerant** — it fails fast, names the broken contract, and
keeps the invariant "a live `ChannelExchange` owns a usable subscription" true for the whole type rather than
re-checked at each use. No existing sentinel fit (`ErrNilChannel` is about a nil *channel* argument), so a new
one was added:

```go
// ErrNilSubscription is returned when a SubscribableChannel breaks its
// Subscribe contract by returning a nil Subscription together with a nil error.
ErrNilSubscription = errors.New("msgin: channel returned a nil subscription")
```

Per "fix the class, not the instance", the contract is also now stated where implementers read it — a
`CONTRACT FOR IMPLEMENTERS` paragraph on `SubscribableChannel` (`channel.go`) requiring a non-nil/nil or
nil/non-nil pair and never nil-nil. The other two `Subscribe` call sites were audited and are **not** the same
defect: `harness/groupstore.go:24` discards the handle (`_, err :=`), and `channel/pubsub_registry.go:65`
subscribes to a `*PublishSubscribeChannel` it constructed itself, never a caller-supplied one.

Blackbox test: a minimal `nilSubChannel` test double returning `(nil, nil)`, folded into the existing
`TestNewChannelExchange_validation` table. Verified to actually detect the defect — with the guard disabled the
new case fails (`want ErrNilSubscription, got <nil>`) while the other three still pass; guard restored.

**Propagation the coordinator must action:** `ErrNilSubscription` is new public API, so Task 12's contingent
assertions move. Measured with the plan's own command: root exported non-method decls **101 → 102**, root
sentinels **42 → 43** (under option A, i.e. both expr sentinels retained). Task 12 and Spec 014 §4.1 need the
new numbers, and `apidiff` gains one addition. Not edited here, because those same numbers are contingent on
the still-open 9.5.0 decision and should move once, together with it.

#### F14.5 (M5) — five new `doc.go` files were untracked

`endpoint/doc.go`, `routing/doc.go`, `transform/doc.go`, `channel/doc.go` and `resilience/doc.go` were all
untracked. `ST1000` is disabled, so no gate would have caught it, and these files are the only place the new
package boundaries and the mandated multi-instance topology statements are documented. **`git add`ed** (not
committed). `docs/plans/027-tools/` deliberately left untracked — separate open question, see F13.20.

#### F14.6 — verification of this pass

```
$ go build ./... ; go vet ./... ; gofmt -l .
OK  OK  (no files)

$ golangci-lint run ./...
0 issues.

$ for d in . adapter/database/sql/{harness,postgres,mysql,sqlite,dbtest} adapter/cron/crontest; do
    (cd "$d" && GOWORK=off go build ./... && GOWORK=off go vet ./...) ; done
GREEN: .  harness  postgres  mysql  sqlite  dbtest  crontest        # 7/7

$ GOWORK=off go test ./... -race -shuffle=on
ok  github.com/kartaladev/msgin            ok  .../channel     ok  .../endpoint
ok  .../adapter/cron                       ok  .../resilience  ok  .../routing
ok  .../adapter/database/sql               ok  .../transform   ok  .../adapter/memory
ok  .../adapter/http                       ok  .../adapter/http/stdlib             # 11/11, goleak now live in 6

$ GOWORK=off go test ./... -coverpkg=./... -coverprofile=… && go tool cover -func=… | tail -1
total: (statements) 93.4%                                          # unchanged from the 93.4% baseline
```

#### F14.7 — left open

1. **The five new `main_test.go` files are untracked.** The task scoped `git add` to the five `doc.go` files
   only, so they were not staged — but leaving them untracked reproduces exactly the F14.5 failure mode for
   the F14.1 fix. **They must be staged with the commit or M1 does not land.**
2. **Task 12 / Spec 014 §4.1's contingent counts are now stale by one** (F14.4): 102 / 43, not 101 / 42.
3. **Task 9.5.0 is still undecided** (F14.3), deliberately.

---

### F15 — Task 9.7 (D-M / D-N / D-P): execution record

**Execution order actually run:** Task **9.7 FIRST**, before Task 9 — as pinned by the round-7 correction
(D-M2/X-M2) and ADR 0029 §5.0b. Nothing in 9.7 depended on 9/9.5/9.6. Task numbers unchanged.

#### F15.1 — the RED baselines reproduced byte-for-byte at `1c4f73e`

The three published sweeps reproduce exactly (class sweep 12 lines / 43 sentinels; D-M godoc sweep 15 lines;
D-N/D-P behavior-derived sweep 39 lines). Gate transcripts, throwaway `package msgin_test` harness at the
repo root, deleted before the commit:

```
$ go test -run 'TestR7ProducerPath|TestR7SentinelCensus|TestR8ProducerConsequence|TestR8DPGate' -v .
--- GATE 1 (RED) ---
transform.Transform(nil)      [dlq, no invalid sink] OnRetry=2 OnDeadLetter=1 OnInvalidMessage=0 | dlqSink=1 invalidSink=0 discarded=false
transform.Transform(nil)      [dlq + invalid sink]   OnRetry=2 OnDeadLetter=1 OnInvalidMessage=0 | dlqSink=1 invalidSink=0 discarded=false
msgin.To(nil)                 [dlq, no invalid sink] OnRetry=2 OnDeadLetter=1 OnInvalidMessage=0 | dlqSink=1 invalidSink=0 discarded=false
msgin.To(nil)                 [dlq + invalid sink]   OnRetry=2 OnDeadLetter=1 OnInvalidMessage=0 | dlqSink=1 invalidSink=0 discarded=false
--- GATE 2 (census, RED == GREEN by design) ---
IsPermanent(msgin: nil endpoint function              ) = false
IsPermanent(msgin: no route for message               ) = false
IsPermanent(msgin: payload is not of the expected type) = true
IsPermanent(msgin: message has no correlation key     ) = false
IsPermanent(msgin: nil outbound sink                  ) = false
--- GATE 3 (producer, RED) ---
transform.Transform(nil) OnRetry=2 OnDeadLetter=1 | dlqSends=1 | Is(ErrDeadLettered)=true  Is(ErrNilFunc)=true  Is(ErrNilSink)=false IsPermanent=false
msgin.To(nil)            OnRetry=2 OnDeadLetter=1 | dlqSends=1 | Is(ErrDeadLettered)=true  Is(ErrNilFunc)=false Is(ErrNilSink)=true  IsPermanent=false
--- D-P gate (RED; row 1 is the plan's "BEFORE D-N") ---
permanent, dlq OK     : deliveries=1   acks=1  nacks=0   dlqSends=0   OnInvalid=1  OnDeadLetter=0  OnRetry=0
permanent, dlq FAILS  : deliveries=1   acks=1  nacks=0   dlqSends=0   OnInvalid=1  OnDeadLetter=0  OnRetry=0
transient, dlq FAILS  : deliveries=41  acks=0  nacks=41  dlqSends=39  OnInvalid=0  OnDeadLetter=0  OnRetry=41
```

#### F15.2 — the GREEN gates, same harness, after the edit

```
--- GATE 1 (GREEN) ---
transform.Transform(nil)      [dlq, no invalid sink] OnRetry=0 OnDeadLetter=0 OnInvalidMessage=1 | dlqSink=1 invalidSink=0 discarded=false
transform.Transform(nil)      [dlq + invalid sink]   OnRetry=0 OnDeadLetter=0 OnInvalidMessage=1 | dlqSink=0 invalidSink=1 discarded=false
msgin.To(nil)                 [dlq, no invalid sink] OnRetry=0 OnDeadLetter=0 OnInvalidMessage=1 | dlqSink=1 invalidSink=0 discarded=false
msgin.To(nil)                 [dlq + invalid sink]   OnRetry=0 OnDeadLetter=0 OnInvalidMessage=1 | dlqSink=0 invalidSink=1 discarded=false
--- GATE 2 (every row UNCHANGED — IsPermanent's enumeration was not amended) ---
IsPermanent(msgin: nil endpoint function              ) = false
IsPermanent(msgin: no route for message               ) = false
IsPermanent(msgin: payload is not of the expected type) = true
IsPermanent(msgin: message has no correlation key     ) = false
IsPermanent(msgin: nil outbound sink                  ) = false
--- GATE 3 (GREEN — a DOCUMENTED LOSS: dlqSends 1 -> 0, Is(ErrDeadLettered) true -> false) ---
transform.Transform(nil) OnRetry=0 OnDeadLetter=0 | dlqSends=0 | Is(ErrDeadLettered)=false Is(ErrNilFunc)=true  Is(ErrNilSink)=false IsPermanent=true
msgin.To(nil)            OnRetry=0 OnDeadLetter=0 | dlqSends=0 | Is(ErrDeadLettered)=false Is(ErrNilFunc)=false Is(ErrNilSink)=true  IsPermanent=true
--- D-P gate (GREEN) ---
permanent, dlq OK     : deliveries=1   acks=1  nacks=0   dlqSends=1   OnInvalid=1  OnDeadLetter=0  OnRetry=0
permanent, dlq FAILS  : deliveries=1   acks=1  nacks=0   dlqSends=1   OnInvalid=1  OnDeadLetter=0  OnRetry=0
transient, dlq FAILS  : deliveries=41  acks=0  nacks=41  dlqSends=39  OnInvalid=0  OnDeadLetter=0  OnRetry=41   <- D8 UNCHANGED
```

**Gate 1 rows 1 and 3 read `dlqSink=1 discarded=false`, not the plan's `dlqSink=0 discarded=true`** — exactly
as the plan's ROUND-8 gate-minor box predicted: those rows publish a D-M-only intermediate this commit never
leaves behind, because D-N routes them to the dead-letter sink in the same commit.

#### F15.3 — the D-P gate is not vacuous (mutation check)

The Nack arm D-P forbids was temporarily re-inserted into `divertTerminal` and both the gate and the unit
test caught it. Reverted immediately; the tree is clean of it.

```
--- with the forbidden Nack arm restored ---
permanent, dlq FAILS  : deliveries=41  acks=0  nacks=41  dlqSends=41  OnInvalid=0  OnDeadLetter=0  OnRetry=41
--- FAIL: TestDivertInvalidFallback/fallback_sink_Send_FAILS_—_single_shot,_no_redelivery_loop
        the original delivery is Acked, never Nacked / exactly one delivery — no redelivery loop
        ONE attempt at the sink — there is no second / OnRetry must not fire — no retry follows
--- FAIL: TestDivertInvalidFallback/decode_failure_with_a_failing_fallback_sink_is_single_shot_too
```

#### F15.4 — the class sweep after the edit: 5 edit-site lines gone, 7 survivors byte-identical

```
$ sentinels=$(grep -oE '^\s*Err[A-Za-z]+ =' errors.go | tr -d ' \t=' | paste -sd'|' -)
$ grep -rnE "return (msgin\.)?($sentinels)[ })]*(//.*)?$" --include='*.go' . | sed 's,^\./,,' \
    | grep -v '_test\.go' | grep -vE '^[^:]+:[0-9]+:[[:space:]]*//' | grep -v 'Permanent(' | sort
adapter/memory/queuestore.go:146:		return msgin.ErrOverflowDropped // nothing evictable (all in-flight) → drop
adapter/memory/queuestore.go:151:	return msgin.ErrOverflowDropped // OverflowReject
channel/direct.go:87:		return msgin.ErrNoSubscriber
endpoint/producer.go:589:		return msgin.ErrScheduledSendUnsupported
retry.go:48:		return ErrInvalidMaxAttempts
retry.go:51:		return ErrNoDeadLetter
routing/router.go:59:			return msgin.ErrNoRoute
```

Only `routing/router.go`'s ErrNoRoute line NUMBER moved (56 → 59), from the three lines added above it. Text
identical on all seven.

#### F15.5 — D-P's shape: option 1 (split the function), and its one consequence

`divert` was **split**, per the plan's recommendation. The invalid path (`consumer.go` decode arm and
permanent arm) now calls a terminal sibling `divertTerminal(ctx, sink, d, terminalHook, cause)` with **no
`attempt` parameter and no `Nack` arm**; the dead-letter call site keeps `divert(..., attempt)` and ADR 0007
D8's Nack-with-backoff unchanged. The hard-coded `attempt = 1` disappeared with the branch.

**Consequence, decided rather than left silent:** `divert`'s `sink == nil` arm became **unreachable** once the
two invalid call sites stopped using it — `RetryPolicy.Validate` rejects a finite `MaxAttempts` without a
`DeadLetter`, so the only remaining caller's sink is never nil. Leaving it would have created a NEW uncovered
block, which this task's Verify forbids. It was therefore **removed from `divert` and kept on
`divertTerminal`**, the only path that can reach it, with the precondition stated in `divert`'s godoc.

#### F15.6 — the fallback WARN uses a `sync.Once`, not `panicLogged`'s `sync.Map`

The plan offered both (*"if a new field reads clearer, say so"*). A new field
`consumer.invalidFallbackLogged sync.Once` was added: there is exactly **one** such event and no key to
deduplicate by, so `LoadOrStore` on a map keyed by a constant string would be a keyed structure with one key.
Same rationale, same one-line-per-consumer outcome, same "further occurrences suppressed" wording.
`panicLogged` is untouched. The neither-sink WARN at `divertTerminal` stays **one line per message** — there
the id is the only record the message existed.

#### F15.7 — two SHIPPED tests asserted the behavior D-P changes and were retargeted

Not enumerated by the plan; found by running the suite. Both are in `endpoint/consumer_test.go`.

1. `TestConsumer_DivertSendFailure_NacksNotAcks` drove the **invalid** sink and asserted Nack-with-backoff +
   `OnRetry` + no terminal hook. That is precisely what D-P removes from the invalid path. It was
   **retargeted to the DEAD-LETTER path** (transient handler error, `MaxAttempts: 1`, failing `DeadLetter`),
   where the contract is unchanged — so both I6 backoff arms (non-nil → non-zero, nil → 0) keep a covering
   case. Its godoc now says why.
2. `TestConsumer_SafeSend_SinkPanicRetriesInsteadOfLosingMessage` asserted that a **panicking invalid sink**
   Nacks. Renamed `TestConsumer_SafeSend_SinkPanicIsRoutedLikeASendError` and re-pointed at what it actually
   proves — a recovered panic is routed identically to a returned error — with the outcome now the
   single-shot terminal discard.

#### F15.8 — the two godoc sweeps after the edit

**D-M sweep** (`grep -rn 'ErrNilFunc\|ErrNilSink' --include='*.go' . | grep -v '_test\.go' | grep '//' | sort`):
all **15** original lines edited, none dropped; the sweep now returns 31 lines because the replacement godocs
are longer. `errors.go`'s two sentinel godocs carry the round-8 invariant **verbatim as a phrase match** (not
the withdrawn round-7 "deterministic" wording); `routing/aggregator.go` states the `NewAggregator` exclusion
as a decision.

**D-N/D-P behavior-derived sweep** — all **39** original sites still present (verified per file: every file
in the original 39 appears with at least its original hit count), 13 triaged untouched, 3 verdict-only
confirmed, 23 edited. It now returns **57** lines, because D-M's own godoc edits added phrases this class
sweep matches too. Four sites needed a second pass after the first edit **dropped them out of the sweep** —
`reliability.go`'s `permanentError`, `consumer.go`'s `safeHandle` comment (both had lost the matching
phrase), `consumer.go`'s over-long `safeDecode` line, and `divert`'s send-failure comment (unchanged text).

`adapter/database/sql/options.go:186` is the **heading** of the block whose body (`:193`) was corrected; the
heading itself is still accurate and was left as the plan's "edit them together" intends.

**Canonical destination sentence:** `msgin.Permanent`'s godoc now carries the three arms in full, and the
other eleven shorthand sites cross-reference it rather than paraphrasing.

#### F15.9 — Task 9 census note (plan line 722): the number is **16**, unchanged

This task retypes **neither** `Router.pick` (the field) **nor** `NewRouter`'s parameter, so Spec §5.0's census
is untouched by 9.7. Task 9 still owns that decision (16 → 15 → 14).

```
$ grep -rn "msgin\.MessageChannel\|MessageChannel interface" --include="*.go" . | grep -v "_test.go" \
    | grep -v "^./docs" | grep -v '// ' | wc -l
16
```

#### F15.10 — verification of this task

```
$ go test ./... -race -shuffle=on                    # 11/11 packages
ok github.com/kartaladev/msgin  ok .../adapter/cron  ok .../adapter/database/sql  ok .../adapter/http
ok .../adapter/http/stdlib      ok .../adapter/memory ok .../channel  ok .../endpoint
ok .../resilience               ok .../routing        ok .../transform

$ for d in . adapter/database/sql/{harness,postgres,mysql,sqlite,dbtest} adapter/cron/crontest; do
    (cd "$d" && GOWORK=off go test ./... -race -shuffle=on); done
OK: .  harness  postgres  mysql  sqlite  dbtest  crontest            # 7/7 (Docker up)

$ gofmt -l . ; go vet ./... ; golangci-lint run ./... ; govulncheck ./...
(no files)  OK  0 issues.  No vulnerabilities found.

$ go mod tidy && git diff --exit-code -- go.mod go.sum ; go mod verify ; CGO_ENABLED=0 go build ./...
NO CHANGE   all modules verified   OK

$ apidiff -w <HEAD snapshot> {.,./endpoint,./routing,./transform} && apidiff <snapshot> <working tree>
(empty for all four — the change is BEHAVIORAL; verified non-vacuous by diffing the root
 snapshot against ./endpoint, which reports "BackoffStrategy: removed / Chain: removed / …")

$ go test ./... -coverpkg=./... -coverprofile=… && go tool cover -func=… | tail -1
before (HEAD worktree): total 93.4%      after: total 93.5%
uncovered blocks in root+endpoint+routing+transform: 5 before, 5 after.
The one diff is `endpoint/consumer.go:467.20,469.15` -> `:494.20,496.15` — the SAME pre-existing
uncovered block (ingest's ctx.Done arm), shifted 27 lines by the additions. ZERO net-new.
```

---

### F15.11 — Task 9.7 adversarial-review fix pass (uncommitted; D-P scope ruling)

An adversarial review of the uncommitted Task 9.7 tree returned nine findings. **The governing question it
raised was a scope question, and the user ruled on it before any fix was written.**

#### F15.11.0 — THE RULING: D-P's single shot is the WHOLE invalid path, not the D-N fallback

The review observed a mismatch: `divertTerminal` settles a failed `Send` single-shot **regardless of which
sink failed**, while D7's amendment, Spec §2.1 row 7 and `WithInvalidMessageSink`'s godoc all described the
single shot as a property of the **D-N fallback**. Two ways to close it — narrow the code to the fallback, or
widen the documents to the path.

**User ruling: the CODE IS CORRECT; the DOCUMENTS ARE WRONG.** The single shot is a property of the
**message** (permanent by classification), not of which sink refused it. Rationale, to be preserved wherever
this is restated:

- The invalid path deliberately **never consults the attempt tracker** (M8), so a `Nack` there is invisible to
  `MaxAttempts`.
- `IsPermanent` records **healthy** on the breaker (`endpoint/consumer.go:614`), so the breaker cannot see it.
- With the default `nil` `Backoff`, `retryDelay` never escalates — it **hot-spins**.
- Worst of the four: the redelivery **holds its `WithMaxInFlight` credit indefinitely**, so a down invalid
  sink starves **valid** traffic.

A permanent message cannot succeed on redelivery, so the loop buys nothing to pay for any of that.

**The accepted cost is now DISCLOSED, not implicit:** a *transient* outage of a **configured**
`WithInvalidMessageSink` discards that window's invalid messages. Loudly — a WARN per message naming the id,
the classification cause and the sink error, plus `OnInvalidMessage`. The documented remedy is to point the
option at a **durable-on-write** target (a spool, an outbox table), not a remote service.

#### F15.11.1 — disposition of all nine findings

| # | Finding | Disposition | Where |
|---|---|---|---|
| 1 | D7 amendment / D8 / Spec row 7 / `WithInvalidMessageSink` godoc all scope the single shot to the **fallback** | **FIXED** — all four widened to the whole invalid path per F15.11.0 | ADR 0007 D7 + D8; Spec §2.1 row 7; `endpoint/consumer.go` |
| 1b | **D8 was STALE** — *"otherwise it fires the relevant hook and `Nack`s the original (never Ack-and-lose)"* is FALSE for the invalid path since 9.7 split `divert`/`divertTerminal`. Missed because the plan's checkbox only required re-verifying **D7** | **FIXED** — D8 scoped explicitly to `divert` (dead-letter), with `divertTerminal` named as the exception | ADR 0007 D8 |
| 2 | No case anywhere drives a **CONFIGURED** invalid sink whose `Send` fails | **FIXED** — new table case; non-vacuity proven (F15.11.2) | `endpoint/divert_fallback_test.go` |
| 3 | Retargeting the safeSend panic test onto the terminal arm left the **retryable** arm unpinned — no consumer-side test drove a panicking **dead-letter** sink, though `safeSend`'s godoc claims both routings | **FIXED** — new table case; `recordingSink` gained a `panicWith` field so a panic is accounted exactly like a returned error | `endpoint/divert_fallback_test.go`, `endpoint/settlement_doubles_test.go` |
| 4 | `endpoint/permanent_classification_test.go` used a `position string` **want-field** (CLAUDE.md + `table-test` hard rule) | **FIXED** — converted to `assert` closures. The other three new test files were swept: `routing/` and `divert_fallback` already comply; `transform/` is a single non-table case | `endpoint/permanent_classification_test.go` |
| 5 | `"cause", cause` may disclose payload bytes on the decode arm | **FIXED** (not triaged — see F15.11.3) | `endpoint/consumer.go` `causeForLog` |
| 6 | The "proves the scope" dead-letter case never asserted `dlqSends > 0`, and `assert.Positive(delays[last])` could not tell a divert-failure `Nack` from an ordinary transient one | **FIXED** — shared `assertDeadLetterSendFailureNacks` helper; non-vacuity proven (F15.11.2) | `endpoint/divert_fallback_test.go` |
| 7 | `warnInvalidFallback`'s *"The per-message record is the terminal WARN/hook"* is false on `divertTerminal`'s **success** arm | **FIXED** — corrected, and the "exactly one line for the whole lifetime" case stated as intended, not accidental | `endpoint/consumer.go` |
| 8 | `routing/aggregator.go` godoc left a dangling `// WithGroupTimeout without a` mid-paragraph | **FIXED** — clause returned to the first paragraph | `routing/aggregator.go` |
| 9 | Doubled-prefix error string `msgin: permanent: msgin: nil endpoint function: …` | **TRIAGED, NO ACTION** — a property of `Permanent` itself, recorded by the plan (~line 697) as *"recorded, not repaired here"*. Repairing it is a change to `permanentError.Error()`, outside 9.7 | — |

#### F15.11.2 — non-vacuity: four mutations, each reverted after measurement

Every new or strengthened assertion was proven to **fail** against the implementation it exists to forbid.

```
M1  divertTerminal's send-failure arm → Nack (the pre-D-P shape)
    FAIL  fallback sink Send FAILS — single shot, no redelivery loop
    FAIL  CONFIGURED invalid sink Send FAILS — single shot too, not just the fallback   <- finding 2
    FAIL  decode failure with a failing fallback sink is single shot too

M2  dead-letter branch made unreachable (`case false && …`)
    FAIL  dead-letter call site still Nacks with backoff when its sink fails            <- finding 6
    FAIL  dead-letter call site Nacks with backoff when its sink PANICS                 <- finding 3

M3  safeSend's recovered panic → err = nil (panicking sink treated as a successful divert)
    FAIL  dead-letter call site Nacks with backoff when its sink PANICS
          Error: Should be zero, but was 1        (acks)
          Error: "2" is not greater than "2"      (nacks past the transient budget)

M4  causeForLog removed (log the raw cause)
    FAIL  decode failure with a failing fallback sink is single shot too                <- finding 5
          Error: … cause="msgin: payload decode failed: invalid character '}' looking
                 for beginning of value" … should not contain "invalid character"
```

M4 is also the **proof that finding 5 was a real disclosure**, not a theoretical one: the unredacted line
quotes a payload byte, verbatim, from `encoding/json`.

The finding-6 helper is deliberately arithmetic rather than a bare `Positive`: exactly `divertMaxAttempts-1`
`Nack`s precede budget exhaustion, so `nacks-(divertMaxAttempts-1) == dlqSends` asserts that **every** `Nack`
past the budget came from the divert arm, one per `Send` attempt. Only then is a positive delay evidence of
**D8/I6's send-failure backoff** rather than of the ordinary retry backoff.

#### F15.11.3 — finding 5: FIX, not triage. The evidence that decided it

The finding allowed a triage **if `safeDecode` already logged `codecErr`**. It does not:

```
$ sed -n '/func (c \*consumer\[T\]) safeDecode/,/^}/p' endpoint/consumer.go
    ... c.logger.Error("msgin: PayloadCodec.Decode panicked", "id", id, "panic", r)   # panic arm only
    ... v, err = c.codec.Decode(b); if err != nil { return zero, fmt.Errorf(...) }     # NOT logged
```

The returned codec error is wrapped and returned, never logged. So `"cause", cause` at the D-P WARN was a
**new** disclosure surface, and M4 above measured it leaking a payload byte. **FIXED.**

The fix keeps what D-P asks for and drops only what it does not:

```go
func causeForLog(cause error) string {
	if errors.Is(cause, msgin.ErrPayloadDecode) {
		return msgin.ErrPayloadDecode.Error()
	}
	return fmt.Sprintf("%v", cause)
}
```

Three design points, each load-bearing:

1. **It returns a `string`, not an `error`** — the value is for display and must never be mistaken for
   something to `errors.Is` against. This is also what keeps it OUT of the class sweep: `return
   msgin.ErrPayloadDecode` (an `error`) would have become an **8th survivor** in a gate whose contract is
   *"the seven survivors are unchanged"*, and it is not a flow-path return at all — triaging it would have
   meant adding a fifth arm to a ratified invariant. `.Error()` dodges the sweep's `[ })]*(//.*)?$` anchor for
   the right reason, not by accident.
2. **Only the `ErrPayloadDecode` class is redacted.** It is the sole cause msgin builds around
   caller-supplied free text. `ErrPayloadType`, `ErrPayloadTooLarge` and `Permanent(...)` carry no
   msgin-extracted payload and render in full.
3. **`%v`, not `.Error()`, on the default arm** — a nil cause renders `<nil>` rather than panicking. No call
   site passes nil today; this must not be the reason a future one may.

The codec detail is not lost to the caller, only to the log: `OnInvalidMessage` still receives the
**unredacted** cause, so an operator who wants it takes it under their own disclosure policy.

#### F15.11.4 — what the review MISSED (found during the pass)

**(a) A fifth doc site with the same fallback-only scoping — `reliability.go`'s `Permanent` godoc.** The
review named four places. `Permanent`'s canonical three-arm block — the one every other *"routed to the
invalid-message sink"* statement in msgin is shorthand for — attached the SINGLE-SHOT sentence to **arm 2
only**. Left alone, the canonical statement would have contradicted the four widened ones. Fixed the class,
not the instance: swept every `single-shot` godoc site in the tree —

```
$ grep -rn "SINGLE-SHOT\|single-shot\|single shot" --include='*.go' . | grep -v '_test\.go'
reliability.go:31              <- WAS fallback-scoped; FIXED
endpoint/consumer.go:69,95,863,889,903,1060
adapter/database/sql/options.go:195   <- already correct ("on the INVALID-message path")
```

**(b) A failed DEAD-LETTER `Send` emits NO log line at all.** Discovered when a first draft of the
finding-6 helper asserted the sink error appeared in the log and the buffer came back **empty**. `divert`
reports the failure only through `OnRetry`, and `OnRetry` carries the **classification cause**, never the
sink error — that is D8's own contract (no terminal event happened, so no terminal record is made). The
consequence: **an operator with no hooks wired sees nothing when their dead-letter sink is down**, while the
invalid path is loud about exactly the same failure. This is **pre-existing D8 behavior, not introduced by
9.7**, so it is triaged here rather than changed; the helper's godoc records why it asserts nothing about the
log. **Worth a decision in a later increment.**

**(c) The review's own finding-6 wording was slightly off.** It asked to assert `dlqSends > 0`, which is
necessary but not sufficient — `dlqSends > 0` alone still cannot separate a divert-failure `Nack` from a
transient one. The arithmetic relation in F15.11.2 is what actually closes it.

#### F15.11.5 — verification of this fix pass

```
$ go test ./... -race -shuffle=on                    # 11/11 root packages
ok msgin  ok .../adapter/cron  ok .../adapter/database/sql  ok .../adapter/http
ok .../adapter/http/stdlib  ok .../adapter/memory  ok .../channel  ok .../endpoint
ok .../resilience  ok .../routing  ok .../transform

$ for d in . adapter/database/sql/{harness,postgres,mysql,sqlite,dbtest} adapter/cron/crontest; do
    (cd "$d" && GOWORK=off go test ./... -race -shuffle=on); done
OK  .   harness   postgres   mysql   sqlite   dbtest   crontest              # 7/7 (Docker up)

$ gofmt -l . ; go vet ./... ; golangci-lint run ./... ; govulncheck ./...
(no files)   OK   0 issues.   No vulnerabilities found.

$ go mod tidy && git diff --exit-code -- go.mod go.sum ; CGO_ENABLED=0 go build ./...
NO CHANGE   OK
```

**Class sweep — the seven survivors, byte-identical to F15's post-edit state (nothing added, nothing lost):**

```
adapter/memory/queuestore.go:146   adapter/memory/queuestore.go:151   channel/direct.go:87
endpoint/producer.go:589           retry.go:48                        retry.go:51
routing/router.go:66
```

**Sentinel census unchanged** — `IsPermanent`'s enumeration was not touched by this pass
(`git diff reliability.go` shows no change inside `func IsPermanent`); it still reads
`errors.As(*permanentError) || Is(ErrPayloadType) || Is(ErrPayloadDecode) || Is(ErrPayloadTooLarge)`.

**Both godoc sweeps re-run; NO site dropped.**

- *D-M's sweep* (`ErrNilFunc|ErrNilSink`): all **15** original sites still present, every one expanded
  (`endpoint/activator.go` 13→13+16, 37→40+42; `helpers.go` 16→18+28; `errors.go` 150→152+159, 152→162+179;
  `handler.go` 50→53+56+57; `routing/aggregator.go` 239→241+248; `filter.go` 26→26+28+29; `routing/helpers.go`
  18→20, 20→22+31; `router.go` 25→26+28+29, 36→42; `splitter.go` 13→14+17; `transform/transformer.go` 14→16+19,
  35→40+50).
- *D-N/D-P's behavior-derived sweep*: **39 → 61 lines**, growth only. Per-file minimums all met; the two files
  this pass touched are `endpoint/consumer.go` (10 → 19, additions only) and `routing/aggregator.go`
  (**2 → 2**, unchanged — the repaired godoc block is not a sweep site).

**Coverage — ZERO net-new uncovered blocks.**

```
$ go test ./... -coverpkg=./... -coverprofile=… && go tool cover -func=… | tail -1
total: 93.5%          # identical to F15.10's post-Task-9.7 figure

$ awk 'NR>1 {c[$1]+=$NF} END{for(k in c) if(c[k]==0) print k}' … | grep -v '^…/(adapter|channel|resilience)/'
endpoint/consumer.go:515.20,517.15      # was :494.20,496.15 — SAME block, shifted 21 lines by the godoc
endpoint/gateway.go:30.27,32.3
endpoint/nativereliability.go:9.52,9.68
endpoint/poller.go:152.11,153.80
endpoint/poller.go:164.12,166.3
```

Five before, five after — the same five. `causeForLog`'s two branches are both covered: the redacted arm by
the decode case, the pass-through arm by every permanent-handler case.

**Docs-link gate (both arms):** arm 2 clean; arm 1 reports only the **two documented parser false positives**
(`docs/plans/016-aggregator.md -> docs/plans/m`, `docs/specs/006-cron-source.md -> docs/specs/factory(fireTime`)
— both are wrapped Go identifiers, not links, exactly as CLAUDE.md's "Known limitation" records.

**Nothing was committed.** The whole pass is additive on top of the uncommitted Task 9.7 tree.

---

### F15.12 — Task 9.7 `/code-review` pass: five findings, all resolved

`/code-review` over the uncommitted Task 9.7 tree returned five findings. One was a **real defect** (a message
loss); one was a **doc-only correction of an over-claim**; three were **hygiene**. Dispositions and evidence
below. Nothing committed; this pass is additive on the same uncommitted tree.

| # | Finding | Disposition | Where |
|---|---|---|---|
| 1 | `divertTerminal` misreads shutdown cancellation as a sink refusal and loses the message | **FIXED** (code + test) | `endpoint/consumer.go` `divertTerminal`; `endpoint/divert_fallback_test.go` |
| 2 | `invalidTarget` omits the `!NativeDeadLetter()` guard the transient arm applies | **NO CODE CHANGE** — the omission is correct; godoc + test added so a sweep cannot "restore" it | `endpoint/consumer.go` `invalidTarget` |
| 3 | `causeForLog`'s comment implies non-decode causes carry no payload; a caller-composed `Permanent` error is logged verbatim | **BEHAVIOR KEPT, CLAIM CORRECTED** | `causeForLog`, `WithInvalidMessageSink`, `msgin.Permanent` godoc |
| 4 | Stale `consumer.go:NNN` citations in test comments | **FIXED** — re-anchored on branch predicates | `divert_fallback_test.go`, `settlement_doubles_test.go` |
| 5 | Dead `policy` field in a table; the constructor discards its `MaxAttempts` | **FIXED** — narrowed to `backoff` | `consumer_test.go` |

#### F15.12.1 — Finding 1, the defect: shutdown cancellation was Ack-and-lose

`divertTerminal` called `c.safeSend(ctx, …)` with `ctx == settleCtx`. `drainWorkers` calls `cancelSettle()`
when the shutdown deadline expires (D9/C1), so a **healthy** sink's `Send` returns `context.Canceled` — and
that took D-P's discard arm: WARN → `OnInvalidMessage` → `safeAck`. Because `adapter/memory`'s `Ack` is
`func(context.Context) error { return nil }` and ignores its context, the `Ack` **succeeded** and the tracker
evicted. **The message was gone with the sink up.** It is a regression introduced by D-P: before 9.7 this arm
`Nack`ed with requeue and the message survived.

**Fix:** when `ctx.Err() != nil`, `Nack(requeue=true)`, fire **no** terminal hook, return `false` (not settled
— the tracker entry is kept). Recorded normatively in [ADR 0007 D7](../adrs/0007-reliability-settlement-api.md)
(shutdown-exception note), [ADR 0029 §5.0b](../adrs/0029-eip-lexical-alignment.md) and
[Spec 014 §2.1 row 7](../specs/014-core-package-layout.md).

**D-P is NOT weakened**, and the D-P gate below proves it: in normal operation `ctx.Err()` is `nil`, so the
single shot is byte-identical; the `Nack` is reachable only inside a shutdown D9 already bounds.

**Mutation proof — the new test is not vacuous.** The `ctx.Err() != nil` branch was deleted and the test run:

```
--- MUTATION APPLIED: ctx-done arm removed ---
--- FAIL: TestDivertTerminalShutdownCancellationNacks
    the Nack REQUEUES; a non-requeue Nack would drop it just as surely
        expected: []bool{true}   actual: []bool(nil)
    OnInvalidMessage must NOT fire — no terminal event happened
        Should be zero, but was 1
    the WARN says the divert was cut short, not refused
        log contains "discarding invalid message; the sink rejected it and the divert is single-shot"
          … err="context canceled"          <- the defect, verbatim
    the single-shot discard WARN must not claim a refusal the sink never made
FAIL	github.com/kartaladev/msgin/endpoint	0.466s
```

Reverted immediately; the tree is clean of the mutation.

#### F15.12.2 — Finding 2, a deliberate negative that needed a stated verdict

The review asked why `invalidTarget` lacks the `!c.native.NativeDeadLetter()` guard that dispatch's transient
dead-letter arm applies. **The guard would be wrong here.** The discriminator is `Nack`-vs-`Ack`:

- the **transient** path `Nack`s, so a native-DLQ broker dead-letters the message itself and a runtime write
  would **double-write** it — hence the guard there;
- the **invalid** path `Ack`s on *every* arm (`divertTerminal` always settles with `safeAck`), so a native-DLQ
  broker **never sees a `Nack`** for it and never dead-letters it. The fallback write is the message's **only**
  capture; adding the guard would return a `nil` sink and silently restore D7's discard.

No code change. The verdict is now stated in `invalidTarget`'s godoc (the "deliberate negative needs a stated
verdict" pattern already used for `ErrNoRoute`) and pinned by
`TestDivertInvalidFallbackUnderNativeDeadLetter`. **Mutation-proved:**

```
--- MUTATION APPLIED: !NativeDeadLetter() guard "restored" on invalidTarget ---
--- FAIL: TestDivertInvalidFallbackUnderNativeDeadLetter
    the D-N fallback still writes under a native DLQ …
        expected: 1   actual: 0
```

#### F15.12.3 — Finding 3, the over-claim (documentation fixed, behavior kept)

`causeForLog` redacts only the `ErrPayloadDecode` class, while its own comment invoked the *"message id only,
never the payload"* contract — implying every other cause is payload-free. It is not: a handler returning
`msgin.Permanent(fmt.Errorf("invalid email %q", m.Payload().Email))` — an ordinary validation shape — has that
text written to the WARN. This is the first point on the settlement path where a **caller-composed** error's
text reaches the log.

**Behavior kept.** Redacting every cause would erase the classification detail D-P *requires* the WARN to name,
and a caller-authored error is the caller's own text, not payload msgin extracted. The distinction being drawn
is **authorship, not sensitivity** — so the contract is stated on the caller's side instead: `causeForLog`'s
comment is narrowed, and both `WithInvalidMessageSink` and `msgin.Permanent` now say plainly which class **is**
redacted (`ErrPayloadDecode`, and only it), that everything else is logged **verbatim**, and that sensitive
detail belongs behind `OnInvalidMessage`, which always receives the unredacted cause.

#### F15.12.4 — Finding 4, stale citations: fixed as a CLASS

`divert_fallback_test.go` and `settlement_doubles_test.go` cited `consumer.go:688` (decode arm), `:716`
(permanent arm) and `:726` (dead-letter arm). All three had drifted — **`:688` had landed inside
`handlerContext`**, i.e. the citation pointed at unrelated code.

Re-deriving the numbers would have gone stale again *in this very pass*: the `WithInvalidMessageSink` godoc
added for finding 3 shifted `dispatch` down 19 lines. So the citations are **re-anchored on the branch
predicate** (`dispatch`'s `derr != nil` / `msgin.IsPermanent(err)` / finite-exhausted `MaxAttempts` case),
which cannot drift, with a one-time note in `divert_fallback_test.go` recording why. This is *fix the class,
not the instance*.

Sweep of the rest of the working-tree diff for the same class: the only other `consumer.go:NNN` citations added
this session are the two **command transcripts** in this ledger, refreshed below. (`docs/adrs/0029`'s `:716`
/`:726` are **pre-existing and committed**, outside this diff — logged as backlog, not silently absorbed.)

#### F15.12.5 — Finding 5, dead table field

`TestConsumer_DivertSendFailure_NacksNotAcks`'s table declared a full `msgin.RetryPolicy` per case, but the
constructor was rewritten to `msgin.RetryPolicy{MaxAttempts: 1, DeadLetter: sink, Backoff: tc.policy.Backoff}`
— so each case's `MaxAttempts` was silently discarded while the table still presented it as meaningful. The
field is narrowed to `backoff msgin.BackoffStrategy` (the only thing the cases vary) and the godoc now says
`n == 1` comes from the constructor's one-attempt budget, not from the table.

#### F15.12.6 — verification

**Refreshed transcripts** (the two this pass's edits shifted):

```
$ grep -rn "SINGLE-SHOT\|single-shot\|single shot" --include='*.go' . | grep -v '_test\.go'
reliability.go:31
adapter/database/sql/options.go:195
endpoint/consumer.go:69,95,110,903,927,954,968,1137      # was :69,95,863,889,903,1060 — +2 sites, both
                                                          # from the shutdown-exception godoc
```

**Class sweep — the seven survivors, unchanged; 43 sentinels, unchanged:**

```
adapter/memory/queuestore.go:146,151   channel/direct.go:87   endpoint/producer.go:589
retry.go:48,51                         routing/router.go:66
```

`IsPermanent`'s enumeration is untouched (`ErrPayloadType` / `ErrPayloadDecode` / `ErrPayloadTooLarge`).

**D-P gate — re-run, byte-identical to F15.2's GREEN transcript:**

```
permanent, dlq OK   : deliveries=1   acks=1 nacks=0   dlqSends=1   OnInvalid=1 OnDeadLetter=0 OnRetry=0
permanent, dlq FAILS: deliveries=1   acks=1 nacks=0   dlqSends=1   OnInvalid=1 OnDeadLetter=0 OnRetry=0
transient, dlq FAILS: deliveries=41  acks=0 nacks=41  dlqSends=39  OnInvalid=0 OnDeadLetter=0 OnRetry=41
```

The single shot is unchanged in normal operation, which is the whole claim finding 1's fix had to preserve.

**Suites and gates:**

```
$ go test ./... -race -shuffle=on                    # 11/11 root packages ok
$ for d in . adapter/database/sql/{harness,postgres,mysql,sqlite,dbtest} adapter/cron/crontest; do
    (cd $d && GOWORK=off go test ./... -race -shuffle=on); done      # 7/7 OK
$ for d in <same seven>; do (cd $d && go build ./...); done          # 7/7 OK (workspace)
$ gofmt -l .                                         # empty
$ go vet ./...                                       # clean
$ golangci-lint run ./...                            # 0 issues.
$ govulncheck ./...                                  # No vulnerabilities found.
$ go mod tidy && git diff --exit-code -- go.mod go.sum ; go mod verify ; CGO_ENABLED=0 go build ./...
```

**Coverage — ZERO net-new uncovered blocks; total unchanged at 93.5%.**

```
endpoint/consumer.go:534.20,536.15      # was :515.20,517.15 — SAME admit ctx-done block, shifted 19 lines
endpoint/gateway.go:30.27,32.3
endpoint/nativereliability.go:9.52,9.68
endpoint/poller.go:152.11,153.80
endpoint/poller.go:164.12,166.3
```

Five before, five after — the same five. Per-package: root 95.3%, `endpoint` 99.1%, `routing` 100%,
`transform` 100%. `divertTerminal`'s **new** `ctx.Err() != nil` branch is covered (by
`TestDivertTerminalShutdownCancellationNacks`) and so is not among them.

**Nothing was committed.**

---

## F16 — Task 9 (named behavior types + `Predicate` combinators): execution record

**Execution order actually run:** Task **9.7 first** (committed at `64963ad`), then Task **9** — the order the
round-7 correction (D-M2/X-M2) pinned. The tree was therefore already uniform when this task started: the five
shipped producers wrap `msgin.Permanent(sentinel)` with a position, so the combinators copied that convention
rather than inventing one. Run from `64963ad`, clean tree. **Nothing was committed.**

### F16.1 — gates 8.4c–8.4f: RED before, GREEN after

Run via the plan's own extraction command (Task 9 Verify, plan lines 781–784), so the gate text is the §11
canonical block's, not a retyped copy:

```
$ eval "$(sed -n '/^# ==== CANONICAL GATE BLOCK/,/^```$/p' \
            docs/plans/027-core-package-layout.md | grep -v '^```')" | grep '8\.4[cdef]'
--- BEFORE (RED) ---            --- AFTER (GREEN) ---
RED: 8.4c                       GREEN: 8.4c
RED: 8.4d                       GREEN: 8.4d
RED: 8.4e                       GREEN: 8.4e
RED: 8.4f                       GREEN: 8.4f
```

The RED is the plan's predicted shape — the symbols did not exist:

```
$ go doc github.com/kartaladev/msgin/routing.Predicate
doc: no symbol Predicate in package github.com/kartaladev/msgin/routing        (exit 1)
    … identically for routing.RouteFunc, routing.SplitFunc, transform.Transformer
```

GREEN, verified **per type** (ADR 0029 §4's mitigation is load-bearing, so this is not sampled):

| Type | Spring equivalent named in its godoc |
|---|---|
| `routing.Predicate[A]` | `org.springframework.integration.core.MessageSelector` |
| `routing.RouteFunc` | `org.springframework.integration.router.AbstractMessageRouter#determineTargetChannels` |
| `routing.SplitFunc[A,B]` | `org.springframework.integration.splitter.AbstractMessageSplitter#splitMessage` |
| `transform.Transformer[A,B]` | `org.springframework.integration.transformer.Transformer` (typed form `GenericTransformer<S,T>`) |

This discharges **Spec §8 obligation 4 for the four types Task 9 creates**. The two shipped types
(`CorrelationStrategy`, `ReleaseStrategy`, gates **8.4a/8.4b**) are **still RED** — they are Task 11b's, and
this task deliberately did not touch them.

### F16.2 — the census is **15**, NOT 14 — the plan's arithmetic is wrong, and 14 is unreachable

Task 9's Note-for-Task-12 checkbox (plan lines 722–728) offers three outcomes: leave both → 16, retype the
parameter → 15, retype the field as well → **14**. The decision taken was **retype BOTH**. The measured result
is **15**, and the plan's 14 is **not achievable by any choice** in this task:

```
$ grep -rn "msgin\.MessageChannel\|MessageChannel interface" --include="*.go" . \
    | grep -v "_test.go" | grep -v "^./docs" | grep -v '// ' | wc -l
      16        # BEFORE
      15        # AFTER
```

Two census lines were removed and **one was added**, which the plan did not account for:

```
REMOVED  routing/router.go:35  pick func(ctx context.Context, m msgin.Message[any]) (msgin.MessageChannel, error)
REMOVED  routing/router.go:45  func NewRouter(pick func(… ) (msgin.MessageChannel, error), opts ...RouterOption)
ADDED    routing/router.go:39  type RouteFunc func(ctx context.Context, m msgin.Message[any]) (msgin.MessageChannel, error)
```

**The `RouteFunc` declaration is itself a census line**, and necessarily so — the type exists precisely to
return a `msgin.MessageChannel`, so no declaration of it can avoid the token the census greps for. 16 − 2 + 1 =
15. The census counts *occurrences of the token*, not *anonymous signatures*, so naming a type relocates one
occurrence rather than eliminating it. **Task 12 must expect 15.**

*(Second plan defect, same checkbox: it cites the two lines as `routing/router.go:29` and `:37`. They were
`:35` and `:45` at `64963ad` — the numbers drifted when Task 9.7 added the D-M godoc paragraph to `Router`.
The two lines the plan identifies are the right two; only the line numbers are stale.)*

### F16.3 — `apidiff`, `./routing` and `./transform`

Task-local scratch snapshots (not a gate input — the committed root baseline is untouched and root `apidiff`
for this task is legitimately zero, since every symbol retyped here lives in `routing`/`transform` and is
already inside the window's 95 root removals):

```
$ apidiff -w $SP/task9-routing.api ./routing && apidiff -w $SP/task9-transform.api ./transform   # BEFORE
$ apidiff $SP/task9-routing.api ./routing                                                        # AFTER
Incompatible changes:
- Filter: changed from func(func(ctx context.Context, m msgin.Message[A]) (bool, error), ...FilterOption) msgin.Step
                   to func(Predicate[A], ...FilterOption) msgin.Step
- NewRouter: changed from func(func(ctx context.Context, m msgin.Message[any]) (msgin.MessageChannel, error), ...RouterOption) *Router
                      to func(RouteFunc, ...RouterOption) *Router
- Split: changed from func(func(ctx context.Context, m msgin.Message[A]) ([]msgin.Message[B], error)) msgin.Step
                  to func(SplitFunc[A, B]) msgin.Step
Compatible changes:
- Predicate: added
- RouteFunc: added
- SplitFunc: added

$ apidiff $SP/task9-transform.api ./transform                                                    # AFTER
Incompatible changes:
- Transform: changed from func(func(ctx context.Context, m msgin.Message[A]) (msgin.Message[B], error)) msgin.Step
                     to func(Transformer[A, B]) msgin.Step
Compatible changes:
- Transformer: added
```

**Reviewed: four `apidiff`-incompatible parameter-type changes, source-compatible FOR EVERY CALL SHAPE EXCEPT
A CALLER'S OWN NAMED FUNC TYPE.** `apidiff` reports a parameter retyped to a named defined type as
incompatible because it compares type identity, not assignability; Go still infers a bare closure literal
against a named generic func type. Demonstrated, not assumed (F16.5). Four types added, all compatible.
Nothing removed.

> ⛔ **QUALIFIED at review (round 9) — the unqualified "SOURCE-COMPATIBLE" above was over-broad, and so was
> F16.5's "both directions … pinned".** One shape genuinely breaks: a **downstream caller's own named func
> type**. `var p MyPred` no longer compiles against any of the four constructors, and for the three generic
> ones inference fails outright with an opaque message rather than a plain assignability error. Measured, all
> four, one call per package build:
>
> ```
> type MyPred  func(ctx context.Context, m msgin.Message[int]) (bool, error)
> type MyRoute func(ctx context.Context, m msgin.Message[any]) (msgin.MessageChannel, error)
> type MySplit func(ctx context.Context, m msgin.Message[int]) ([]msgin.Message[int], error)
> type MyXform func(ctx context.Context, m msgin.Message[int]) (msgin.Message[int], error)
>
> routing.Filter(p)       vet: in call to routing.Filter, type MyPred of p does not match
>                              routing.Predicate[A] (cannot infer A)
> routing.NewRouter(r)    vet: cannot use r (variable of func type MyRoute) as routing.RouteFunc
>                              value in argument to routing.NewRouter
> routing.Split(s)        vet: in call to routing.Split, type MySplit of s does not match
>                              routing.SplitFunc[A, B] (cannot infer A and B)
> transform.Transform(x)  vet: in call to transform.Transform, type MyXform of x does not match
>                              transform.Transformer[A, B] (cannot infer A and B)
> ```
>
> `NewRouter` — the non-generic one — at least names the two types. The three generic ones report an
> **inference** failure, which reads as a mystery unless you already know the cause.
>
> **Everything else still compiles**, verified in the same throwaway package (removed after measurement):
> bare closure literal · variable of the **unnamed** func type · func **returning** the unnamed type ·
> **method value** · plain top-level func declaration. All five, all four constructors where applicable →
> `ALL FIVE SHAPES COMPILE CLEAN`.
>
> **The design artifacts were already correct** — Spec 014, RFC 0003 and ADR 0029 each scope the claim to
> *"a bare closure remains assignable"*. Only this execution ledger overstated it. **Impact is theoretical:
> msgin is unreleased with zero consumers, so no such named type exists anywhere. Code unchanged —
> documentation only.**

### F16.4 — mutation testing: the three trap cases are non-vacuous, and one caught a defect **in the test**

Each mutant is the naive implementation the plan warns about; each was applied to `routing/predicate.go`, the
suite run, then the file restored from a byte-identical copy.

| # | Mutant | Killed by | Result |
|---|---|---|---|
| 1 | `Not` returns `!ok, err` (inverts instead of propagating) | `Not propagates an error rather than inverting the result` | FAIL — `Should be false` |
| 2 | `And` checks the nil argument **after** the `!ok` short-circuit | `And with a nil argument surfaces even when the left is FALSE` | FAIL |
| 3 | `Or` checks the nil argument **after** the `ok` short-circuit | `Or with a nil argument surfaces even when the left is TRUE`, and `NilPositionsAreDistinct` | FAIL |

**Mutant 1 initially did NOT fail, and that was a defect in the test, not the mutant.** The first draft drove
the `Not` error case with a predicate returning `(true, err)`; the naive `return !ok, err` then yields
`(false, err)` — which is exactly what the case asserted, so the trap case passed against the code it exists to
reject. The fixture was re-oriented to `(false, err)`, where correct yields `false` and naive yields `true`.
Without the mutation run this case would have shipped green and vacuous — the same class as
`measure-interleaving-tests`.

### F16.5 — source compatibility, demonstrated rather than assumed

The claim is that existing call sites passing **bare closures** compile unchanged against the named parameter
types. Evidence: **no pre-existing test was edited.**

```
$ git diff --stat -- '*_test.go'
 transform/transformer_test.go | 19 +++++++++++++++++++       # purely ADDITIVE — a new func
 1 file changed, 19 insertions(+)
$ git diff -- transform/transformer_test.go | grep -cE '^-[^-]'
0                                                            # zero lines deleted or changed
```

The whole suite — including bare-closure call sites in `example_composition_test.go:26-27`,
`endpoint/composition_integration_test.go:31-32`, `routing/filter_test.go:85`,
`routing/example_splitter_test.go:17`, `routing/aggregator_settlement_test.go:194` and
`routing/permanent_classification_test.go:67`, none of which were touched — compiles and passes. Both
directions are additionally pinned by new explicit cases
(`TestBehaviorTypes_SourceCompatibility`, `TestTransformerType_SourceCompatibility`): each constructor is
called once with a bare closure and once with a value of the named type.

> ⛔ **QUALIFIED at review (round 9): "both directions … pinned" means MSGIN'S OWN named type and the bare
> closure — NOT a caller's named func type,** which is the one shape that breaks. See the qualifying block in
> §F16.3 for the four measured error messages and the five shapes that still compile. The tests named here
> pin exactly what they say they pin; the summary sentence was the part that generalized too far.

### F16.6 — suites, gates and coverage

```
$ go test ./... -race -shuffle=on                     # 11/11 root packages ok
$ for d in . adapter/database/sql/{harness,postgres,mysql,sqlite,dbtest} adapter/cron/crontest; do
    (cd $d && GOWORK=off go test ./... -race -shuffle=on); done    # 7/7 ok (dbtest 114s, crontest 78s — real Docker)
$ for d in <same seven>; do (cd $d && go build ./...); done        # 7/7 ok (workspace)
$ gofmt -l .                                          # empty
$ go vet ./...                                        # clean
$ golangci-lint run ./...                             # 0 issues.
$ govulncheck ./...                                   # No vulnerabilities found.
$ go mod tidy && git diff --exit-code -- go.mod go.sum # no diff
$ go mod verify                                       # all modules verified
$ CGO_ENABLED=0 go build ./...                        # ok
```

**Coverage — both packages held at 100%; every new branch has a covering case.**

> ⛔ **TRANSCRIPT CORRECTED at review (round 9).** Task 9's Verify (plan line 808) requires **`-coverpkg=./...`
> on both sides**; this section originally recorded the plain `-cover` form, so the checkbox and the evidence
> did not match. Re-run in the required form below. **The numbers did not change** — nothing was hiding — but
> a transcript that does not match its own gate is not evidence.

**Run per package in ISOLATION**, so neither package's test binary can credit the other's statements
(the merged two-package invocation cross-credits, which would mask a gap — the
`package-split-breaks-coverage-attribution` hazard):

```
$ go test ./routing/   -coverpkg=./... -coverprofile=$SP/routing.cov
ok  github.com/kartaladev/msgin/routing     coverage: 58.2% of statements in ./...
      routing/ functions in profile: 35 ; below 100.0%: 0

$ go test ./transform/ -coverpkg=./... -coverprofile=$SP/transform.cov
ok  github.com/kartaladev/msgin/transform   coverage: 36.4% of statements in ./...
      transform/ functions in profile: 3 ; below 100.0%: 0
```

**Read the headline number correctly.** Under `-coverpkg=./...` the `58.2%` / `36.4%` are *"of statements in
`./...`"* — the whole 11-package root module as measured by that one package's tests — **not** the package's
own coverage. The gate *"`routing` and `transform` stay at 100.0%"* is discharged by the per-function
breakdown, which reports **zero** functions below 100.0% in either package. The plain `-cover` form, which
reports per-package attribution, agrees: `routing` 100.0%, `transform` 100.0%.

```
$ go tool cover -func=$SP/routing.cov | grep predicate.go
routing/predicate.go:43:   nilPredicate  100.0%
routing/predicate.go:65:   And           100.0%
routing/predicate.go:104:  Or            100.0%
routing/predicate.go:137:  Not           100.0%
```

*(Line numbers shifted from `:61`/`:92`/`:121` — the §F16.8 fix added the right-operand error branch and the
godoc paragraph stating the invariant.)*

### F16.7 — what was added

| File | Symbol | Note |
|---|---|---|
| `routing/predicate.go` (new, 148 lines) | `Predicate[A]`, `And`, `Or`, `Not`, unexported `nilPredicate` | `nilPredicate` mirrors `nilFuncStep`'s position-parameter shape; 129 → 148 lines in the §F16.8 fix pass |
| `routing/router.go:39` | `RouteFunc` | `Router.pick` **and** `NewRouter`'s parameter both retyped (F16.2) |
| `routing/splitter.go:10` | `SplitFunc[A,B]` | `Split`'s parameter retyped |
| `routing/filter.go:30` | — | `Filter`'s parameter retyped to `Predicate[A]` |
| `transform/transformer.go:10` | `Transformer[A,B]` | `Transform`'s parameter retyped |
| `routing/predicate_test.go` (new) | 21-case table + 2 focused tests | assert-closure form throughout; 19 → 21 cases in the §F16.8 fix pass |
| `routing/example_predicate_test.go` (new, 68 lines) | `ExamplePredicate_And` | added in the §F16.8 fix pass; runnable, real `// Output:` |
| `transform/transformer_test.go` | `TestTransformerType_SourceCompatibility` | additive only |

**The nil checks live in the combinator body, before the returned closure is built** — which is *why* no
short-circuit can skip them: when an operand is nil the composing closure is never constructed at all, so
there is no evaluation path on which the short-circuit could run first. The error still surfaces at
**evaluation**, since `nilPredicate` returns a `Predicate` and combinators stay pure (`Predicate`, not
`(Predicate, error)`).

**Nothing was committed.**

---

### F16.8 — Task 9 adversarial-review fix pass (uncommitted): four findings, all fixed

Review of the uncommitted Task 9 work returned four findings. **All four fixed — none triaged.** Still
uncommitted; the tree at the end of this pass is the tree §F16 describes plus the deltas below.

| # | Finding | Disposition | Where |
|---|---|---|---|
| 1 | **BLOCKER** — `And`/`Or` leaked the right operand's `true` alongside its error | **FIXED IN CODE** (not in the godoc) + both covering tests de-vacuumed | `routing/predicate.go:80-84` (`And`), `:119-123` (`Or`); `routing/predicate_test.go` |
| 2 | `SOURCE-COMPATIBLE` claim over-broad | **FIXED** — qualifying blocks added, code untouched | §F16.3, §F16.5 |
| 3 | Coverage transcript used plain `-cover`, not the required `-coverpkg=./...` | **FIXED** — re-run and re-recorded | §F16.6 |
| 4 | No runnable example for the three new exported combinators | **DONE** | `routing/example_predicate_test.go` |

#### F16.8.1 — Finding 1, the defect: a bare tail call leaked a truthy decision from a failed evaluation

`And` and `Or` both ended in `return q(ctx, m)`, so **whatever bool the right operand returned alongside its
error passed straight through**. Measured before the fix, with a `badTrue` predicate that computes `true` and
then fails — an ordinary shape for a service-backed predicate that records a decision and then errors:

```
BEFORE                                    AFTER
TRUE.And(badTrue)   ok=true  err=boom     ok=false err=boom
FALSE.Or(badTrue)   ok=true  err=boom     ok=false err=boom
badTrue.Not()       ok=false err=boom     ok=false err=boom   (already correct)
```

**The godoc was right and the code was wrong**, so the code moved. *(Line numbers in this paragraph are the
PRE-fix ones the review cited.)* `And`'s godoc at `:51` already said *"the result is false"* and `Or`'s at
`:82` said the same; `Not` (`:113` — *"never (true, err)"*), `nilPredicate` (`:45`) and the test's own
`nilAssert` (*"a degraded predicate must not report true"*) all already guaranteed the invariant. `And`/`Or`
were the two outliers. `Filter` happens to check `err` first (`routing/filter.go:44-48`), but `Predicate` is
public API and composition happens outside `Filter` — so the leak was reachable by any caller composing
combinators directly.

Fix, applied to both (post-fix `routing/predicate.go:80-84` and `:119-123`), matching the shape the left
operand already used:

```go
ok, err = q(ctx, m)
if err != nil {
    return false, err
}
return ok, nil
```

**The LEFT operand was checked, not assumed** (the review asked for this explicitly). It already did
`if err != nil { return false, err }`, so no code change was needed there — and two new table rows
(`And`/`Or` *"propagates a left-side error that arrived with a true result"*, driven by `badTrue`) now **pin**
that, where before nothing did. Both passed on the very first RED run, which is the evidence that the left
side never had the defect.

**Class sweep.** Every `(bool, error)` producer in this change was re-read for the same shape:
`grep -n '(bool, error)' routing/{predicate,filter,router,splitter}.go transform/transformer.go` → the only
producers are the four in `predicate.go`; the other four files contain **no** `(bool, error)` signature at all.
`Filter` is the only *consumer* in the change and reads `err` before `pass`. **No other instance.**

#### F16.8.2 — Finding 1's tests were vacuous, and the vacuity is proven by mutation

`predicate_test.go:104-110` and `:145-151` *(pre-fix line numbers)* asserted `assert.False(t, got)` while
driving the right operand with the shared `bad` fixture — which returns `(false, errBoom)`. **They asserted False against a value that
was already false, so they passed regardless of what the combinator did with it.** This is the *same class*
§F16.4 caught for `Not` and fixed there as an instance, leaving `And`/`Or` unexamined — so the plan's
correction block has been rewritten to state the **invariant** rather than the `Not` row (plan, Task 9).

A second fixture was added — `badTrue`, returning `(true, errBoom)` — and both right-side rows re-driven by
it. Both orientations are now load-bearing and neither is redundant: swap them and the corresponding case
goes vacuous.

**Mutation proof — the fix restored, the defect re-introduced, the suite run, the file restored
byte-identical each time:**

```
########## MUTANT 4 — And ends in a bare `return q(ctx, m)` (the reviewed defect) ##########
--- FAIL: TestPredicateCombinators/And_propagates_a_right-side_error_without_leaking_its_true_result
        Error:     Should be false
        Messages:  an errored And must report false, not the right operand's result
FAIL    github.com/kartaladev/msgin/routing

########## MUTANT 5 — Or ends in a bare `return q(ctx, m)` ##########
--- FAIL: TestPredicateCombinators/Or_propagates_a_right-side_error_without_leaking_its_true_result
        Error:     Should be false
        Messages:  an errored Or must report false, not the right operand's result
FAIL    github.com/kartaladev/msgin/routing

########## RESTORED — byte-identical? ##########
IDENTICAL
ok      github.com/kartaladev/msgin/routing
```

**And the converse — proof the OLD fixture could not have caught it.** Mutants 4+5 applied *and* both rows
reverted to the old `bad` fixture:

```
--- PASS: .../And_propagates_a_right-side_error_without_leaking_its_true_result
--- PASS: .../Or_propagates_a_right-side_error_without_leaking_its_true_result
ok      github.com/kartaladev/msgin/routing
^^^ the broken code PASSES with the old fixture — that is the vacuity, demonstrated rather than argued.
```

The mutants are numbered 4 and 5 to continue §F16.4's series (1–3).

#### F16.8.3 — the nil contract and the two short-circuit traps survived the finding-1 edit

Finding 1 touched the same code paths, so all five nil positions and both traps were re-measured after the
fix (throwaway harness, removed after the run):

```
ok.And(nil)       bool=false Is=true IsPermanent=true "msgin: permanent: msgin: nil endpoint function: routing.Predicate.And: nil argument"
nilPred.And(ok)   bool=false Is=true IsPermanent=true "msgin: permanent: msgin: nil endpoint function: routing.Predicate.And: nil receiver"
ok.Or(nil)        bool=false Is=true IsPermanent=true "msgin: permanent: msgin: nil endpoint function: routing.Predicate.Or: nil argument"
nilPred.Or(ok)    bool=false Is=true IsPermanent=true "msgin: permanent: msgin: nil endpoint function: routing.Predicate.Or: nil receiver"
nilPred.Not()     bool=false Is=true IsPermanent=true "msgin: permanent: msgin: nil endpoint function: routing.Predicate.Not: nil receiver"
--- the two short-circuit traps ---
TRUE.Or(nil)      bool=false err=true
FALSE.And(nil)    bool=false err=true
```

Five distinct strings, `errors.Is` and `IsPermanent` intact on all five, both traps still surfacing the nil.
Unchanged from §F16 — as expected, since the nil checks run before the returned closure is ever built.

#### F16.8.4 — Finding 4: the example

`routing` shipped `ExampleSplit` and `ExampleAggregator` but nothing for the three new exported combinators,
against CLAUDE.md's *"exported behavior is covered by `Example…` tests"*. Added `ExamplePredicate_And`
(`routing/example_predicate_test.go`, `_test` package, real `// Output:`): it composes all three —
`paid.And(isTest.Not()).Or(priority)` — into one `Filter`, and its last row exercises the error contract, so
the example documents *"(false, err), never (true, err)"* rather than only asserting it in a table.

#### F16.8.5 — verification of this fix pass

```
$ go test ./... -race -shuffle=on                        # 11/11 root packages ok
$ go list ./... | wc -l                                  # 11
$ for d in . adapter/database/sql/{harness,postgres,mysql,sqlite,dbtest} adapter/cron/crontest; do
    (cd $d && GOWORK=off go test ./... -race -shuffle=on); done      # 7/7 OK (dbtest, crontest on real Docker)
$ for d in <same seven>; do (cd $d && go build ./...); done          # 7/7 OK (workspace)
$ go test -run '^Example' ./...                          # 11/11 ok
$ gofmt -l .                                             # empty
$ go vet ./...                                           # clean
$ golangci-lint run ./...                                # 0 issues.
$ govulncheck ./...                                      # No vulnerabilities found.
$ go mod tidy && git diff --exit-code -- go.mod go.sum   # no diff
$ go mod verify                                          # all modules verified
$ CGO_ENABLED=0 go build ./...                           # ok
$ eval "$(sed -n '/^# ==== CANONICAL GATE BLOCK/,/^```$/p' \
            docs/plans/027-core-package-layout.md | grep -v '^```')" | grep '8\.4[cdef]'
GREEN: 8.4c   GREEN: 8.4d   GREEN: 8.4e   GREEN: 8.4f
```

Coverage in the plan's required `-coverpkg=./...` form is recorded in **§F16.6** (re-run for finding 3):
`routing` 35/35 functions and `transform` 3/3 functions at 100.0%, including the two branches finding 1 added.

**Nothing was committed.**

### F16.9 — whole-branch `/code-review` pass: three findings, all fixed (uncommitted)

A whole-branch `/code-review` returned three findings. **All three fixed — none triaged.** Two of them are
**outside Task 9's surface** (pre-existing shipped code) and are scoped to land in their own commit; the third
is Task 9's own and rides in Task 9's commit.

| # | Sev | Finding | Disposition | Commit group |
|---|---|---|---|---|
| 1 | HIGH | `WithLogger[T](nil)` stored the nil, then nil-deref'd on the first settlement that logs — on a WORKER goroutine, where `safeHandle` does NOT recover, so the PROCESS DIES | **FIXED** — nil guard, matching the sibling `WithProducerLogger` | **A** (outside Task 9) |
| 2 | MED | `WithCorrelationStrategy(nil)` / `WithReleaseStrategy(nil)` / `WithReleaseWhen(nil)` were accepted, then panicked in `Handle`; inside a Consumer that panic classifies `ErrHandlerPanic` → **transient**, so a pure misconfiguration retries forever | **FIXED** — rejected at the constructor, bare `ErrNilFunc` naming its position | **A** (outside Task 9) |
| 3 | — | The `source-compatible` claim was still over-broad in the godoc and in the two test names | **FIXED** — precise property stated; tests renamed + extended | **B** (Task 9's own) |

Finding 3's ledger text was already qualified in the previous pass (§F16.3, §F16.5); what this pass fixed is
the **godoc and the tests**, which that pass did not reach.

#### F16.9.1 — the two nil-option defects were an INCONSISTENCY, not a new policy

CLAUDE.md: *"Library code must not call `os.Exit`, `log.Fatal`, or `panic` on caller input (return errors
instead)."* In both cases a **sibling option in the very same file already did the right thing** —
`WithProducerLogger` (`endpoint/producer.go:313`) guards, and `WithConsumerClock` / `WithAggregatorClock`
guard — so both findings closed a gap in an established pattern rather than introducing one.

The two fixes deliberately use **different mechanisms**, because the right answer differs:

- **A logger has a safe default** (the discard logger), so `WithLogger(nil)` is a **no-op** — the option
  pattern already used by `WithProducerLogger`, `WithConsumerClock`, `WithAggregatorClock`.
- **A strategy has no safe substitute** for what the caller meant, so a nil one is **rejected at
  `NewAggregator`**, not silently swapped for the default: silently defaulting would run a DIFFERENT
  aggregation policy than the caller wrote, which is worse than a construction error.

`NewAggregator`'s three `ErrNilFunc` arms are returned **BARE**, preserving the constructor arm of D-M's
invariant (ADR 0029 §5.0b) — construction never reaches a `RetryPolicy`, so a `Permanent` wrap would be
meaningless. New unexported helper `nilFuncAt` (`routing/helpers.go`) is the construction-time counterpart of
`nilFuncStep`, and its doc says explicitly that the missing `Permanent` is a decision, not an omission.

**One deliberate extension beyond the review's literal ask:** the review asked for a position only on the two
NEW arms. The pre-existing nil-`fn` arm was also given one (`routing.NewAggregator: nil fn`), because with
three `ErrNilFunc` sources in one constructor a bare sentinel no longer tells the caller which one is
mis-wired — which is the entire point of the change. `errors.Is` and `IsPermanent` behavior are unchanged, so
this breaks nothing; the existing test's assertions still hold and gained a position check.

#### F16.9.2 — the `WithReleaseWhen(nil)` wrinkle

`WithReleaseWhen(nil)` used to wrap the nil `fn` inside a new `bool -> (bool, error)` closure, so `c.release`
came out **NON-nil** and a constructor nil-check would NOT have caught it — the deref would still have
happened at release time. The fix leaves `c.release` **unset** when `fn == nil`, so the constructor's single
check catches every path uniformly. This is proven independently by mutant 2b below.

#### F16.9.3 — mutation proofs: all three guards are non-vacuous

Each guard was removed, the suite re-run, then the file restored from a byte-identical copy.

| # | Mutant | Result |
|---|---|---|
| 1 | `WithLogger` guard removed (`o.logger = l`) | **FAIL** — `panic: nil pointer dereference` at `endpoint/consumer.go:946` (`divertTerminal`'s `c.logger.Warn`), raised in `startWorkers.func1` — an **unrecovered worker goroutine**, so the test BINARY dies rather than the case merely failing. Exactly the reported defect. |
| 2a | Both `NewAggregator` strategy checks removed | **FAIL** — `Expected error … but got nil` on the nil-`WithCorrelationStrategy` case |
| 2b | `WithReleaseWhen(nil)` rebuilds the wrapper (constructor checks KEPT) | **FAIL** — `Expected error … but got nil` on the nil-`WithReleaseWhen` case only. Proves the wrinkle case is load-bearing on its own: the constructor check alone does NOT cover it. |

#### F16.9.4 — finding 1's class sweep: no other unguarded option exists

Every `With*` option in the workspace taking a **pointer, interface, func, map or channel** was traced to its
config field and to **every read** of that field. **Zero further violations.** Dispositions:

| Disposition | Count | Examples |
|---|---|---|
| GUARDED (`if v != nil` in the option) | 17 | `WithProducerClock`, `WithAggregatorClock`, all four `*Logger`s, `WithRateLimit`, `WithCircuitBreaker`, `WithHTTPClient`, `WithLocation` |
| CONSTRUCTOR-REJECTED / re-defaulted | 8 | `WithOutputChannel` (`ErrNilOutput`), `WithSharedTransaction` (`ErrNilResolver`), both codecs, `WithRetryPolicy` |
| NIL-SAFE AT USE | 9 | `WithHooks` (fired via `safeFire`, which nil-checks AND recovers), `WithInvalidMessageSink`, `WithDiscardChannel`, `WithDefaultChannel`, `WithElector`, `WithLocker` |
| Not nilable (scalar/struct) | 5 | `WithFanOut`, both `WithOverflow`s, `WithStrategy`, `WithSlowClientPolicy` — all `int` |

**One coupling worth recording (NOT a defect today).** `consumer.go` `divert` calls
`c.safeSend(ctx, sink, …)` where `sink` is `c.policy.DeadLetter`, **unconditionally**. It is safe only
transitively: `RetryPolicy.Validate` (`retry.go:50`) rejects `MaxAttempts > 0` with a nil `DeadLetter`, and the
sole call site is itself gated on `MaxAttempts > 0`. The invariant is documented at the call site, but it is a
**cross-file coupling** — the one place in the audited set where a future edit could turn a caller's nil into
a panic. Recorded for a future increment; no change made.

#### F16.9.5 — finding 3: the precise property, measured then documented

The claim *"naming the type is source-compatible"* was stated without qualification on all four behavior
types' godoc and embedded in two test NAMES. The precise property was re-measured from scratch in a throwaway
package (`ztmpcompat`, deleted after measurement — not left in the tree):

**REJECTED — a caller's OWN NAMED func type**, all four, one call per build:

```
routing.Filter(p)       in call to routing.Filter, type MyPred of p does not match
                        routing.Predicate[A] (cannot infer A)
routing.NewRouter(r)    cannot use r (variable of func type MyRoute) as routing.RouteFunc
                        value in argument to routing.NewRouter
routing.Split(s)        in call to routing.Split, type MySplit of s does not match
                        routing.SplitFunc[A, B] (cannot infer A and B)
transform.Transform(x)  in call to transform.Transform, type MyXform of x does not match
                        transform.Transformer[A, B] (cannot infer A and B)
```

**NEW this pass, and not previously measured:** explicit type arguments do **not** rescue it —
`routing.Filter[int](p)` fails too (`cannot use p (variable of func type MyPred) as routing.Predicate[int]
value`), which confirms the cause is **assignability** (Go requires at least one side unnamed), with the
inference failure merely being how a generic call site reports it. The **remedy** — an explicit conversion,
`Filter(Predicate[int](p))` — was verified to compile for all four.

**ACCEPTED — six shapes, verified compiling for all four types:** bare closure literal · variable of the
UNNAMED func type · func returning that unnamed type · method value · plain top-level func declaration ·
msgin's own named type.

Fixes, code unchanged (the named types stay — they carry the EIP/Spring documentation value):

- Each of the four types' godoc gained an `ASSIGNABILITY` paragraph naming the accepted shapes, the one
  rejected shape, why (at least one side must be unnamed), how the rejection READS at a generic vs a
  non-generic call site, that explicit type args do not help, and the conversion remedy.
- `TestBehaviorTypes_SourceCompatibility` → **`TestBehaviorTypes_AcceptedCallShapes`**, and
  `TestTransformerType_SourceCompatibility` → **`TestTransformerType_AcceptedCallShapes`**. Names now match
  what the tests assert. Both were extended from 2 shapes to **all six**, across all four constructors —
  **24 subtests**. Each test's doc records that the rejected shape CANNOT be pinned by a test (a test
  asserting it would not compile) and points at the godoc.

#### F16.9.6 — verification of this pass

```
$ go test ./... -race -shuffle=on                        # 11/11 root packages ok
$ go list ./... | wc -l                                  # 11
$ for d in . adapter/database/sql/{harness,postgres,mysql,sqlite,dbtest} adapter/cron/crontest; do
    (cd $d && GOWORK=off go test ./... -race -shuffle=on); done      # 7/7 OK (real Docker for dbtest/crontest)
$ for d in <same seven>; do (cd $d && go build ./...); done          # 7/7 OK (workspace)
$ go test -run '^Example' ./...                          # 11/11 ok
$ gofmt -l .                                             # empty
$ go vet ./...                                           # clean
$ golangci-lint run ./...                                # 0 issues.
$ govulncheck ./...                                      # No vulnerabilities found.
$ go mod tidy && git diff --exit-code -- go.mod go.sum   # no diff
$ go mod verify                                          # all modules verified
$ CGO_ENABLED=0 go build ./...                           # ok
$ <canonical gate block>                                 # GREEN: 8.4c 8.4d 8.4e 8.4f
```

Coverage, measured **per package in isolation** (note `-coverpkg=./...` reports "of statements in ./…", a
WHOLE-MODULE figure, and is not a per-package number):

```
$ go test ./routing/ ./transform/ ./endpoint/ -cover -count=1
routing    100.0%        (unchanged — the new nilFuncAt helper is covered)
transform  100.0%        (unchanged)
endpoint    99.2%        (UP from 99.1% — the nil-logger case added coverage; nothing lost)
```

Task 9's own properties re-confirmed after the changes: the **five** nil combinator positions are still
distinct, both short-circuit traps (`And` with a FALSE left, `Or` with a TRUE left) still surface the nil, and
neither operand leaks `(true, err)`.

**Nothing was committed.** The tree splits into two commit groups: **A** — `endpoint/consumer.go`,
`endpoint/consumer_test.go`, `routing/aggregator.go`, `routing/aggregator_test.go`, `routing/helpers.go`
(shipped code, outside Task 9); **B** — `routing/predicate.go`, `routing/router.go`, `routing/splitter.go`,
`transform/transformer.go` and their tests (Task 9's own surface).

## F17 — Task 9.5 (D-I sentinel removal, staleness sweep, capability widening): execution record

Executed at `b4d1a1a` (clean tree). Plan 027 §9.5; Spec 014 §3.2/§8.1/§9 AC-4; ADR 0029 §5.0a. Three
workstreams, one commit.

### F17.1 — D-I landed, and all four projected numbers held EXACTLY

The §9.5.0 table was measured at `dadc775`, three commits before execution, so the BEFORE row was
**re-measured first** rather than assumed. It was unchanged — confirming that Tasks 9.7 and 9 added no root
exported symbol (Task 9's four new behavior types live in `routing`/`transform`).

```
                              exported  sentinels  removals  additions
plan table, at dadc775            102        43        95         6
RE-MEASURED at b4d1a1a            102        43        95         6     <- BEFORE, matched
after deleting both sentinels     100        41        97         6     <- AFTER, matched the projection
```

The two new `apidiff` lines are exactly `- ErrExprResultType: removed` and `- ErrInvalidExpression: removed`.
Both `var`s were deleted **with their godoc blocks and neither block copied forward** — root's
`ErrInvalidExpression` text names `ErrExprResultType` (which Spec AC-10 arm 2 requires absent workspace-wide)
and closes with *"exported here, not in the provider"*, the premise D-I reverses; `ErrExprResultType`'s text
has no destination at all under revised D-K.

**Every published line number for the two blocks was stale, in all three documents** (Plan §9.5.0 `:168`/`:180`
and `:193`/`:206`; Spec §3.2 `:168`/`:193`; ADR 0029 `:180`/`:206`). The real positions at `b4d1a1a` were
`:196-208` and `:223-236` — the blocks had drifted ~28 lines. Harmless only because the task text said to
locate by symbol. Same class as F13's "verify structural claims against code".

### F17.2 — both sweep arms cleared, and both probed for vacuity

`symmap.tsv` was regenerated first, as required. It was **stale by four entries, not the one the plan named**:
Task 9 added `routing.Predicate`, `routing.RouteFunc`, `routing.SplitFunc`, `transform.Transformer`, taking it
**91 → 95**.

```
ARM 1  codec.go:33                    msgin.NewProducer / msgin.WithProducerCodec  -> endpoint.*
       routing/aggregator_test.go:21  "*msgin.DirectChannel"                       -> *channel.DirectChannel
ARM 2  routing/aggregator.go:366      "the WithRelease strategy failed"            -> WithReleaseStrategy fn
```

All three survivors were real; arm 2's was at **:366, not the :316** the plan and Spec §8.1 both publish (the
file grew 50 lines since the measurement). After the fixes both arms report empty.

Round 4 established that an arm reporting empty proves nothing until you know it can report non-empty, so both
were probed and both reverted:

```
planted  // Probe: msgin.NewQueueChannel is a moved symbol …          -> ARM 1 reported 1 line
planted  // Probe: WithNonexistentOption and ErrNeverDeclared …       -> ARM 2 reported 2 lines
```

### F17.3 — the capability test now covers all eight positions: 24 subtests

3 targets (`QueueChannel`, `PublishSubscribeChannel`, `*memory.Broker`) × 8 send-only positions.

| Where | Test | Subtests |
|---|---|--:|
| `capability_test.go` | `TestSendOnlyCallSitesAcceptEveryChannel` (6 core sites) | 18 |
| `adapter/http/capability_test.go` | `TestServeAsyncTargetAcceptsEveryChannel` | 3 |
| `adapter/http/stdlib/capability_test.go` | `TestNewInboundTargetAcceptsEveryChannel` | 3 |
| | | **24** |

The two HTTP files each carry their own copy of the three target fixtures. That duplication is forced, not
sloppy: all three test packages are blackbox (`package X_test`) and the repo has no shared test-helper package.
HTTP payloads assert `[]byte(capabilityPayload)` — `DecodeRequest` reads the raw body and never decodes it.

**Row 5 (`WithExpiredGroupChannel`) is the trap, and it was proven handled rather than assumed.** That path
returns **no error**: `Handle` holds the member and returns nil, and the reaper delivers later on `Run`'s
goroutine after a fake-clock tick, so an `assert(err)` in the rows-1–3 shape is vacuous. The load-bearing
assertion is the harness's delivery check. To prove that check is real, all three added core sites were
**mutation-probed** — each re-pointed at a decoy `channel.NewPublishSubscribeChannel()` instead of `target`:

```
9 of 9 subtests FAILED (3 targets x {router pick return, aggregator output, aggregator expired-group})
```

including row 5. Mutation reverted. Row 5's `Run` goroutine is joined by a `t.Cleanup` registered **before**
the tick, so it is joined even when the delivery assertion fails; root's `goleak.VerifyTestMain` is the leak
gate. Row 3 (`NewRouter`'s `pick` return — the message-time position every earlier draft dropped) uses a bare
closure literal, which is assignable to Task 9's named `routing.RouteFunc`.

There is deliberately **no RED** for this workstream (round-7 X-M10): the widening landed in `b6ce7bb`, so
every subtest passes the first time it compiles. These are a regression fence whose failure mode is a future
*narrowing*. The mutation probe above is the substitute evidence that they are not vacuous.

### F17.4 — propagation: verified, not assumed

All four downstream sites already carried D-I correctly and needed **no edit** — Spec 014 §3.2 (L522-531), §4
(L1091), §4.1 (L1177-1191), §7 (L1740-1818); Plan Task 10's `expr/errors.go` checkbox and its `RouteFunc`
two-construction-validations bullet; Plan Task 12's projection table and `apidiff` fifth-class bullet. Spec
§4/§4.1's D-I numbers are left as **projections** on purpose: the end state they describe is post-D-J
(102/42), which Task 9.6 has not landed.

Edited because this task invalidated a published measurement: Spec §8.1's `symmap` count and both survivor
lists, and Spec §9 AC-4's "9 of the required 24".

**`CLAUDE.md:275` — RAISED by the implementer, FIXED by the coordinator, in this commit.** It read *"DECIDED
but NOT YET IMPLEMENTED … they still exist in `errors.go` today"*, which the deletion falsifies. CLAUDE.md is
outside an implementer subagent's remit, so it was flagged rather than edited — the right call — and the
coordinator made the edit: the paragraph now records D-I as landed, with the measured 43→41 / 102→100 / 95→97.

**The same class reached three more documents, and the adversarial review caught that one instance had been
fixed while three were left.** ADR 0029 §5.0a (`:279` `STATUS:` and `:1087`) and `docs/HANDOVER.md:183` all
still asserted the sentinels existed. ADR 0029 is the **normative** record of D-I, so leaving it saying "not
yet implemented" after implementing it is precisely the drift the traceability rule exists to prevent; both
lines are corrected here rather than deferred to Task 12's doc sync. This is the *fix the class, not the
instance* rule applied to documentation: the unit of repair is "every document asserting the pre-D-I state",
not "the one CLAUDE.md line someone noticed".

## F18 — Task 9.5 whole-branch review, second pass: one fix, one triaged class

### F18.1 — FIXED: `endpoint.NewConsumer` guarded `src` but not `h` — 46k retries in 200 ms

**The asymmetry.** `NewConsumer[T](src any, h Handler[T], opts ...ConsumerOption[T])` (`endpoint/consumer.go`)
validated exactly two things — `src == nil` → `msgin.ErrNilAdapter`, and `cfg.policy.Validate()`. Its peer
argument `h` was never checked. Every other exported constructor in the workspace that takes two required
arguments guards **both** (`NewChannelExchange` request+reply, `NewSQLElector`/`NewSQLLocker` db+dialect,
`newAdapterBase`/`newGroupBase` db+table+dialect, `NewInboxDeduper`, `cron.NewSource` spec+factory,
`NewAggregator` store+fn+both strategies). `NewConsumer` was the sole exception.

**Why it is not cosmetic.** The nil is not seen until the first message, where `c.handler(...)` nil-derefs on a
worker goroutine. `safeHandle` recovers that panic into `ErrHandlerPanic` — and `IsPermanent` classifies
`ErrHandlerPanic` **TRANSIENT**, so the message is Nacked and redelivered. Measured on one message over 200 ms:

```
OnRetry = 46106     OnInvalidMessage = 0
```

A pure wiring mistake becomes an unbounded hot retry loop instead of an error at the call the caller can see.
This is the same failure mode D-M's `Permanent` wrap exists to prevent on the Step-returning constructors
(ADR 0029 §5.0b) — reached here by the other arm of the invariant.

**The fix.** `endpoint/consumer.go` — a guard immediately after the `src` check, preserving the established
"nil-arg checks before value validation" order:

```go
if h == nil {
    return nil, nilFuncAt("endpoint.NewConsumer: nil handler")
}
```

`nilFuncAt` is a new package-local helper in `endpoint/helpers.go`, mirroring `routing/helpers.go`'s
`nilFuncAt` **byte-for-byte in shape**. It is copied rather than shared: exporting an internal helper across
package boundaries to save two lines over `fmt.Errorf`/`msgin.ErrNilFunc` is precisely what Spec 014 §3.3's
inlining rule rejects — the same rule that already gives each endpoint package its own `boxMessage` and
`nilFuncStep`.

**The sentinel is BARE, deliberately — this is the `NewAggregator` arm of `ErrNilFunc`'s invariant.** The error
is handed straight back to the caller and never carried through a handler, so it never reaches a
`RetryPolicy` and a retry classification would be meaningless on it. The test asserts
`msgin.IsPermanent(err) == false` so a later "finish the job" sweep that wraps it fails loudly.

**Non-vacuity, by mutation.** The guard was removed and the new subtest re-run:

```
--- FAIL: TestNewConsumer_Validation/nil_handler_is_a_BARE,_non-permanent_ErrNilFunc_(construction-time)
        Error: Expected error with "msgin: nil endpoint function" in chain but got nil.
```

Guard restored; green. The case lives in the existing `TestNewConsumer_Validation` table
(`endpoint/consumer_test.go`), which gained a `handler func() endpoint.Handler[order]` modifier field — nil
means "use the loop's valid default", and the nil-handler case sets it to a func *returning* nil, so the table
can express "the argument is nil" without conflating it with "the field is unset".

### F18.2 — the `NewProducer`-family sweep: NO other member of this class exists

Every exported constructor in all seven modules was enumerated mechanically and each required (non-variadic)
argument traced to a guard. **Result: `NewConsumer` was the only "guarded sibling, unguarded peer".**

| Constructor | Required args | Verdict |
|---|---|---|
| `endpoint.NewProducer` | `out` | Guarded (`ErrNilAdapter`). No peer — not in the family. |
| `endpoint.NewGateway` | `x` | Guarded (`ErrNilExchange`). No peer. |
| `endpoint.NewChannelExchange` | `request`, `reply` | **Both** guarded (`ErrNilChannel`), plus `ErrNilSubscription`. |
| `channel.NewQueueChannel` | `store` | Guarded (`ErrNilStore`). |
| `routing.NewAggregator` | `store`, `fn`, strategies | All guarded, all bare `ErrNilFunc`. |
| `routing.NewRouter`, `routing.Filter`, `routing.Split`, `transform.Transform`, `endpoint.Activate`/`Consume`/`OutboundGateway` | 1 func/iface | Return a `Step`/value, not an error: nil degrades to `Permanent(ErrNilFunc)`/`ErrNilExchange` at evaluation (D-M). Correct by design. |
| `cron.NewSource` | `spec`, `factory` | Both guarded (`ErrInvalidSchedule`, `ErrNilFactory`). |
| `cron.NewSQLElector`, `cron.NewSQLLocker` | `db`, `dialect` | Both guarded. |
| `sql.NewPollingSource`, `NewOutboundAdapter`, `NewQueueStore`, `NewGroupStore`, `NewInboxDeduper` | `db`, `table`, `dialect` | All guarded via `newAdapterBase`/`newGroupBase`. |
| `stdlib.NewInbound`, `stdlib.NewInboundGateway` | `target` / `exchange` | Guarded (`ErrNilTarget` / `ErrNilExchange`). |

**One adjacent observation, NOT fixed** (no guarded sibling, so outside this family): `msghttp.NewSSEParser(r
io.Reader, opts ...Option)` validates `opts` via `NewConfig` but never checks `r`. A nil reader is not a
sibling-asymmetry — it is a wholly unguarded single argument that surfaces on the first `Next`. Recorded here
so a future sweep re-derives it rather than rediscovering it.

### F18.3 — TRIAGED TO BACKLOG, NOT FIXED: 24 variadic-option loops call a nil element

**The finding.** 25 sites iterate a variadic slice and invoke each element with no nil check. `msgin.Chain` was
the 25th and **is fixed** (a nil `Step` is replaced in place by a handler returning
`Permanent(ErrNilFunc)` naming its index). The remaining **24** are all the same shape —
`for _, opt := range opts { opt(&cfg) }` in a constructor:

| # | Site | Enclosing constructor |
|--:|---|---|
| 1 | `adapter/cron/source.go:180` | `NewSource[T]` |
| 2 | `adapter/cron/sqlelector.go:100` | `NewSQLElector` |
| 3 | `adapter/cron/sqllock.go:59` | `NewSQLLocker` |
| 4 | `adapter/database/sql/groupstore.go:206` | `NewGroupStore` |
| 5 | `adapter/database/sql/inbox_dedup.go:80` | `NewInboxDeduper` |
| 6 | `adapter/database/sql/outbound.go:56` | `NewOutboundAdapter` |
| 7 | `adapter/database/sql/source.go:89` | `NewPollingSource` |
| 8 | `adapter/database/sql/sqlite/dsn.go:50` | `DSN` |
| 9 | `adapter/http/options.go:1109` | `NewConfig` |
| 10 | `adapter/memory/groupstore.go:80` | `NewGroupStore` |
| 11 | `adapter/memory/memory.go:42` | `New` |
| 12 | `adapter/memory/queuestore.go:86` | `NewQueueStore` |
| 13 | `channel/pubsub.go:121` | `NewPublishSubscribeChannel` |
| 14 | `channel/pubsub_registry.go:28` | `NewPubSub` |
| 15 | `endpoint/consumer.go:253` | `NewConsumer[T]` |
| 16 | `endpoint/exchange.go:235` | `NewChannelExchange` |
| 17 | `endpoint/gateway.go:31` | `NewGateway[Req, Rep]` |
| 18 | `endpoint/producer.go:345` | `NewProducer[T]` |
| 19 | `message.go:159` | `New[T]` |
| 20 | `resilience/breaker.go:85` | `NewCircuitBreaker` |
| 21 | `resilience/ratelimit.go:48` | `NewTokenBucket` |
| 22 | `routing/aggregator.go:297` | `NewAggregator[A, B]` |
| 23 | `routing/filter.go:36` | `Filter[A]` |
| 24 | `routing/router.go:76` | `NewRouter` |

**Reproduction** (`endpoint`, blackbox; the same shape reproduces at all 24):

```go
h := func(context.Context, msgin.Message[order]) error { return nil }
_, _ = endpoint.NewConsumer[order](memory.New(), h, nil)
// recovered: runtime error: invalid memory address or nil pointer dereference
```

**Why these are deliberately NOT fixed in this window** — the three-point rationale, per CLAUDE.md's
"explicitly triaged to a backlog with a written rationale":

1. **It is a uniform 24-site class needing ONE uniform answer.** The decision (skip the nil element vs. reject
   at construction) applies identically to all 24; there is no site-specific judgement. Fixing a subset now
   would repeat exactly the partial-sweep pattern that produced these findings in the first place — `b4d1a1a`
   swept `With*` **options** only, which is precisely why `OutboundGateway` and `Chain` slipped through. *Fix
   the class, not the instance* means fixing all 24 under one decision, or none.
2. **Severity is genuinely lower than the message-time cases.** These panic in the **caller's own goroutine at
   wiring time** — fail-fast, on the caller's stack, at the line that made the mistake. That is the opposite
   of F18.1's failure mode (a silent infinite retry on a worker goroutine where `safeHandle` converts the
   panic into a misclassified transient error). A construction-time panic is visible; a 46k/200ms retry loop
   is not.
3. **No realistic code path produces a nil option.** Options are written as literal calls
   (`WithConcurrency(4)`); none of the workspace's `With*` constructors can return nil. Contrast a nil `Step`,
   which conditional step-building (`steps = append(steps, maybeStep())`) produces naturally — that is why
   `Chain` was fixed and these are not.

**The two candidate fixes.**

| Option | Shape | Pros | Cons |
|---|---|---|---|
| **A — skip the nil element** | `if opt == nil { continue }` at all 24 | 1 line/site, no API change, no new sentinel, cannot break a caller | Silently accepts a wiring bug; diverges from `Chain`, which deliberately does NOT skip |
| **B — reject at construction** (RECOMMENDED) | `return nil, nilFuncAt("<pkg>.<Ctor>: nil option at index N")` | Consistent with `Chain`'s "a nil element is a wiring bug, not a no-op", with F18.1, and with `NewAggregator`'s bare-`ErrNilFunc` constructor arm; names the offending index | **8 of the 24 return no error** and cannot adopt it without a signature change: `sqlite.DSN`, `memory.New`, `channel.NewPublishSubscribeChannel`, `channel.NewPubSub`, `msgin.New`, `resilience.NewCircuitBreaker`, `routing.Filter` (returns a `Step`), `routing.NewRouter` |

**Recommendation: B, with A as the fallback at the eight error-less constructors** — and the split itself
recorded as the decision, so the next worker does not re-litigate it. Whoever takes this must settle B-vs-A
**once**, then apply it to all 24 in a single commit with a class-sweep assertion (not an enumeration) proving
no site was missed.

**Regenerate the site list — do not trust the table above.** It is a measurement with a date on it; line
numbers move. Re-derive with:

```bash
git ls-files '*.go' | grep -v '_test\.go$' | while read -r f; do
  awk -v F="$f" '
    /for[ \t]+_,[ \t]*[A-Za-z_]+[ \t]*:=[ \t]*range[ \t]+(opts|options)[ \t]*\{/ { getline
      if ($0 ~ /^[ \t]*(opt|o|option)\(/) print F ":" NR }
  ' "$f"
done
# 24 today (2026-08-09). Chain is NOT in this set: it ranges `steps`, and is already fixed.
```

### F18.4 — two adjacent nil-argument notes, also backlogged

Both were raised in the same pass and are recorded here for the same reason as F18.3 — they belong to a
"nil argument at wiring time" decision, not to this window's fix.

1. **`stdlib.Register(mux *http.ServeMux, pattern string, h http.Handler)`** (`adapter/http/stdlib/inbound.go`)
   calls `mux.Handle(pattern, h)` with **no guard on `mux`**. A nil `*http.ServeMux` panics on the caller's
   goroutine at wiring time — same fail-fast profile as F18.3, and the same reason it is not urgent. Note it
   is a thin pass-through helper: `http.ServeMux.Handle` would panic on a nil receiver anyway, so guarding it
   is about the **error message**, not about preventing a crash.
2. **`msghttp.ServeAsync(w, r, target msgin.MessageChannel, cfg)`** and
   **`msghttp.ServeGateway(w, r, exchange msgin.RequestReplyExchange, cfg)`** (`adapter/http/inbound.go`) do
   not guard `target`/`exchange`. Unlike (1) these run **per request**, so a nil would deref on a server
   goroutine — but the blast radius is contained: `recoverHandler` catches it and returns a 500. The two
   supported entry points (`stdlib.NewInbound`, `stdlib.NewInboundGateway`) both guard the argument at
   construction, so reaching these with a nil requires calling the low-level function directly. Recorded, not
   fixed.

### F18.5 — coverage: `endpoint` is NOT 99.4% deterministically; it oscillates 99.16 ↔ 99.44

Verifying "coverage no worse than 99.4% on `endpoint`" surfaced a measurement defect, not a regression. The
package's number is **not stable across runs**. Eight consecutive `go test ./endpoint -coverprofile` runs on
the same tree:

| Run | Coverage | `admit` ctx-done arm hits |
|--:|--:|--:|
| 1 | 99.443% | 1 |
| 2 | 99.443% | 1 |
| 3 | 99.443% | 1 |
| 4 | 99.164% | 0 |
| 5 | 99.164% | 0 |
| 6 | 99.443% | 1 |
| 7 | 99.164% | 0 |
| 8 | 99.164% | 0 |

**4 of 8.** The entire 0.28pp swing is one 2-statement block — `consumer.go` `admit`'s final
`select { case out <- md: … case <-ctx.Done(): … }`, the cancellation arm. Whether it is reached depends on
which arm the scheduler picks during a shutdown test; nothing in the tree changed between runs.

**The fix in F18.1 is coverage-neutral-to-positive, proven arithmetically rather than by comparing two
single runs.** Physically reverting the guard, the helper and the test case gives a baseline of
**711/715 = 99.441%**. The change adds **3** statements (cover counts the `if h == nil` statement itself in
the enclosing block, plus the guarded `return`, plus `nilFuncAt`'s `return`) and **all 3 are covered** —
`go tool cover -func` reports `NewConsumer 100.0%` and `nilFuncAt 100.0%`. So with-change is
714/718 = **99.443%** on the runs where the flaky arm lands, i.e. marginally *above* baseline.

**Two traps for the next worker, both hit during this verification:**

1. **`go tool cover -func` resolves line numbers against the CURRENT source.** Running it on a profile
   captured from a different revision of the file silently mis-attributes every block — it reported
   `nilFuncAt 0.0%` for a baseline profile taken when `nilFuncAt` did not exist. Compare **raw profiles**
   (`awk '$3==0'`), never `-func` output across revisions.
2. **A single coverage run is not a measurement here.** Quoting "endpoint 99.4%" as a gate invites a future
   worker to attribute a scheduler coin-flip to their own diff. State the *range* and the flaky block, or
   pin the arm with a deterministic test. Backlogged: `admit`'s ctx-done arm has no test that forces it —
   the coverage it gets today is incidental. This is the *measure interleaving tests, don't trust them* rule
   applied to coverage itself.

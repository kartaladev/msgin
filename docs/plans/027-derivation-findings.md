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

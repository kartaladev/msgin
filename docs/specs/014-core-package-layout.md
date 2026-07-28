# Spec 014 — Core package layout, channel segregation, and behavior types

- **Status:** **REGENERATED FROM A GREEN TREE (2026-07-28); ROUND-3 AUDIT RETURNED `NEEDS-REVISION` 3/3 —
  its findings are folded in below (F12 code fixes, F13 document fixes).**
  > ### The round-3 governing rule — read before quoting any number in this document
  >
  > The *generated* tables verified perfectly: all 80 §3.2 declaration rows, the §4.1 apidiff partition, the
  > file counts. **Every surviving defect was in hand-written prose, or in a command that was pasted but never
  > run**, and they shared one signature:
  >
  > > **A number or command pinned to an intermediate state — one task's commit, the derivation working tree,
  > > the root module — and then presented as a property of the finished branch.**
  >
  > `git diff --stat -- adapter/` (implicitly `HEAD`-relative when it ran) became "the adapter blast radius".
  > `go mod tidy` (implicitly root-only) became "`expr-lang` dropped cleanly" across seven modules. A
  > post-extraction coverage figure became "BASELINE". A root-scoped sentinel census became "the workspace".
  >
  > **Therefore: every pasted command carries its explicit commit range and module scope.** The rule is stated
  > normatively in [Plan 027 Global Constraint 0](../plans/027-core-package-layout.md#global-constraints) so
  > round 4 inherits it. Where a claim below has been re-derived under it, the correction is marked in place.
  §3 is no longer a *prediction*: the migration was performed mechanically, the whole workspace was driven to
  green (all seven modules, `-race`, Docker-backed runners included), and every table in §3–§4 below is a
  **transcription of the resulting tree** with the generating command recorded in
  [`027-derivation-findings.md`](../plans/027-derivation-findings.md) (F0–F11). No number in this document was
  hand-typed.
  > *Round-2 banner cleared 2026-07-28.* Every defect it named is gone and each is now evidenced:
  > `endpoint` no longer reads `Message`'s unexported fields (D-H; F7 — the six sites rewrote over
  > `NewMessage[T](m.Payload(), m.Headers())` and the package compiles); §3.2's split tables are generated at
  > declaration level and complete (F11.1 — 80 declarations across **six** split files, zero unlocated);
  > §3.4's placement leaves no test binary red (F2, F8.4 — the two crossing test identifiers `collector` and
  > `order` are resolved and the whole-workspace `go vet` is clean); "89 error sentinels" is **42** (F1,
  > F11.4); "four of five call sites" is **nine** (F10.2, F11.5); root is **14** files with `Subscription` in
  > `channel.go` (D-C; F11.1, F11.3); and the `resilience` claim is restated as the invariant that is actually
  > true and mechanically checked (§3, F11.4).
  > *(History: rounds 1 and 2 both returned `NEEDS-REVISION` from all three auditors, on hand-typed tables.
  > See [audit round 1](../plans/027-audit-round-1.md) §K and [audit round 2](../plans/027-audit-round-2.md)
  > §F, which called for exactly this regeneration. Round 1's six §H decisions and round 2's eight §G.1
  > decisions D-A…D-H all stand.)*
- **Promoted from:** [RFC-0001](../rfcs/0001-core-package-restructure.md) (package restructure),
  [RFC-0002](../rfcs/0002-eip-alignment.md) (lexical alignment + channel segregation),
  [RFC-0003](../rfcs/0003-endpoint-behavior-types.md) (behavior types + expr provider). All three were accepted
  2026-07-27 with every open question settled; read their §7 *Decisions* before this spec.
- **Governing decisions:** [ADR 0027](../adrs/0027-core-package-restructure.md) (layout, C-full, clean break,
  shared-helper resolution, D-A, D-B), [ADR 0028](../adrs/0028-channel-interface-segregation.md) (channel
  interfaces, Pipe vs Channel Adapter, exchange exclusivity, D-F),
  [ADR 0029](../adrs/0029-eip-lexical-alignment.md) (renames, behavior-type naming, expr provider, D-D, D-E,
  **D-I** §5.0a — the expr sentinels leave root),
  [ADR 0030](../adrs/0030-reply-channel-exclusivity-probe.md) (**D-J** — reply-channel exclusivity is probed
  at construction; **amends ADR 0028 §6.2's default posture**, §5.1 here).
- **Amends:** [ADR 0019](../adrs/0019-runtime-expression-evaluation.md) (`expr-lang` as a core dependency —
  reversed). **Also amends** [ADR 0013](../adrs/0013-composition-endpoints.md), whose F2 `To`/`OutboundAdapter`
  rationale this spec voids (§5.3).
  **Does NOT amend [ADR 0003](../adrs/0003-multi-module-repository-layout.md)** — see §1.4.
- **Implementation:** [Plan 027](../plans/027-core-package-layout.md).
- **Derivation evidence:** [`027-derivation-brief.md`](../plans/027-derivation-brief.md) (method) and
  [`027-derivation-findings.md`](../plans/027-derivation-findings.md) (F0–F11 — every number with its command).
- **Deferred to later increments:** [RFC-0004](../rfcs/0004-trigger-scheduling.md) (Trigger/Poller — increment
  2), [RFC-0005](../rfcs/0005-eip-gap-components.md) (the five EIP components, **including
  `MessageGroupStore.SettleMembers`**, which was cut from this window — audit §H1).

## 1. Problem

Three defects share one cause — the core is a single flat package, so nothing forces a boundary.

1. **No navigable structure.** The root was `package msgin` with **32 source + 45 test files** and no grouping
   (F0 baseline: `ls *.go | grep -v _test.go | wc -l` → 32; `ls *_test.go | wc -l` → 45). A reader looking for
   the Splitter, the Consumer, and the circuit breaker searched the same undifferentiated list.
2. **`MessageChannel` is satisfied by exactly one type.** The only `var _ MessageChannel` assertion in the
   repository outside a test fake was `DirectChannel`. `QueueChannel` has `Send`+`Poll` (it is a
   `PollingSource`); `PublishSubscribeChannel` has `Send`+`Subscribe`→`(Subscription, error)`. The interface
   bundled `Send`+`Subscribe`, but **eight of its nine call sites use only `Send`** — see §5 for the
   enumeration and, more importantly, for the *scope rule* that produces it. **Consequence before the change:
   a `QueueChannel` or `PublishSubscribeChannel` could not be used as a discard target, a default route, a
   router destination, an exchange request channel, or an HTTP inbound target.** This is a capability defect,
   not a naming preference.

   > *Corrected twice.* An early draft claimed "no call site subscribes through the interface" (audit E3);
   > round 1 said **four of five**; round 2 said **six of seven**. Both were produced by grepping the pattern
   > core only. The measured figure is **nine**, and the two sites both audits missed are in the *adapter*
   > tree (F10.2, F11.5). §5 therefore states the check, not the count.
3. **The core was forced to carry `expr-lang`.** Six `*Expr` constructors in `expr.go` were the only reason
   `go list -deps .` pulled in a **7.1 MB** dependency tree, which propagated to all seven modules in the
   workspace — including a consumer using nothing but the SQL adapter. Confirmed by removal: deleting
   `expr.go` and running `go mod tidy` dropped `github.com/expr-lang/expr v1.17.8` from `go.mod` with no other
   change (F6).

**The timing is what makes this affordable.** The repository has **zero tags** (`git tag | wc -l` → 0,
F11.9): nothing is released and no consumer has imported any symbol, so every break in this spec costs a
mechanical rename today and a major version plus a deprecation cycle later.

### 1.4 What the flat core actually is: an undocumented status quo, not a recorded decision

An earlier draft of this spec and of ADR 0027 claimed to *amend* ADR 0003's "the core is one package" premise.
**That premise does not exist.** `grep -r "core is one package" docs/` returns only this bundle's own files.
ADR 0003 decides **module** layout (core module vs. heavy-client satellite modules) and in fact describes a core
module containing several packages — `adapter/memory`, `adapter/database/sql`, and others already live inside it.

This is a **stronger** justification than the one it replaces: the flat root was never argued for, never
weighed against alternatives, and never written down. There is no decision to overturn — only an accreted shape
to correct. ADR 0003's multi-module decision is untouched by this spec.

## 2. Scope

**In scope:** the package restructure, the channel interface segregation, the EIP renames, the named behavior
types, and the extraction of expression support into its own module. These land as **one breaking window and one
`apidiff` review**, because they touch overlapping symbols and splitting them would cost two reviews of the same
surface.

**Out of scope:** any behavior change, with the four exceptions §2.1 enumerates. This increment is
**behavior-preserving by construction** everywhere else. The evidence is the **normalised per-file diff**:
every moved file, compared against its pre-move snapshot, yields **exactly one** intentional difference
(F8.6, F9.4).

> **ROUND-5 CORRECTION (BLOCKER 2) — the "211 = 211, identical name sets" proof was false in EVERY frame,
> and it was the one place §2 claimed to *prove* behavior preservation.** Measured, root module only:
>
> ```
> $ for c in ab233d9 c83dde9~1 c83dde9 b6ce7bb dadc775; do
>     printf "%s %s\n" "$c" "$(git grep -hE '^func (Test|Example)' $c -- '*_test.go' ':!adapter/*' \
>       | sed -E 's/^func ([A-Za-z0-9_]+).*/\1/' | sort -u | wc -l)"; done
> ab233d9   224
> c83dde9~1 224
> c83dde9   211        <- the only tree where 211 is true
> b6ce7bb   218
> dadc775   221        (226 raw: six TestMain functions collapse to one unique name)
> ```
>
> **211 is a `c83dde9`-only figure that was published as a before-and-after identity.** The name sets are not
> identical in any frame either: `c83dde9` **lost 16** (`ExampleFilterExpr`, `TestWithReleaseExpr`, …) and
> **gained 3**, because that single commit did the extraction *and* Task 1's `*Expr` deletion together.
> `dadc775` has lost 17 and gained 14 against `ab233d9` — every one of them documented in §4.1 (the six
> `*Expr` removals) and §6/D-E.
>
> **The name-set argument is therefore withdrawn, not repaired.** Test-function names legitimately change in
> this window; the normalised per-file diff is the claim that actually survives contact with the tree.

### 2.1 The four deliberate behavior changes

Everything else in this window is a move or a rename. These four are not, and each is a decided consequence
rather than a side effect:

| # | Change | Where decided | Evidence |
|---|---|---|---|
| 1 | `MessageChannel` narrows to send-only; `SubscribableChannel` is added; `DirectChannel.Subscribe` returns `(Subscription, error)` | ADR 0028 §1–§2 | §5, F10.1 (RED transcript) |
| 2 | `ChannelExchange.Close` now **cancels** the reply subscription, so a post-`Close` reply no longer reaches `WithUnmatchedReplySink` | ADR 0028 §6.1 | §5.2a, F10.3 |
| 3 | `channel.WithSingleSubscriber()` — opt-in, **off by default**, so zero change to any existing flow | ADR 0028 §6.2 (D-F) | §4.1, F10.9 |
| 4 | `WithReleaseStrategy` takes the named `ReleaseStrategy` (fallible); the bool-only sugar becomes `WithReleaseWhen` | ADR 0029 §3 (D-E) | §6, F3 |

> **`SettleMembers` was cut from this window** (audit §H1, settled with the user 2026-07-27). The
> "adding a method to a shipped interface must ride the breaking window" argument is void here: nothing is
> tagged, there are no consumers, and every implementer is in this repository — so the Resequencer increment
> that *consumes* the method is equally free to add it, and will have a real caller to pin its four undecided
> semantics. See [RFC-0005 §5](../rfcs/0005-eip-gap-components.md).

## 3. Target package layout (normative — transcribed from the green tree)

Organising principle: **vocabulary + interfaces in the root; every implementation in a package named for the EIP
chapter that defines it.** Nothing in root imports a subpackage.

```
msgin/       root — vocabulary + SPI ONLY
  endpoint/    Consumer, Producer, Gateway, ChannelExchange, Activate/Consume   (EIP ch.10)
  routing/     Filter, Router, Split, Aggregator                                (EIP ch.7)
  transform/   Transform                                                        (EIP ch.8)
  channel/     DirectChannel, QueueChannel, PublishSubscribeChannel, PubSub     (EIP ch.3/ch.4)
  resilience/  ExponentialBackoff, NewCircuitBreaker, NewTokenBucket
expr/  ← its own module — Predicate / RouteFunc / Transformer / … providers      (NOT YET BUILT — Plan 027 Task 10)
```

**Dependency graph after the move — measured, not asserted:**

```
$ for p in . ./endpoint ./routing ./transform ./channel ./resilience; do
    echo "$(go list -f '{{.ImportPath}}' $p): $(go list -f '{{range .Imports}}{{.}} {{end}}' $p \
      | tr ' ' '\n' | grep '^github.com/kartaladev/msgin' | tr '\n' ' ')"; done
github.com/kartaladev/msgin:
github.com/kartaladev/msgin/endpoint:   github.com/kartaladev/msgin
github.com/kartaladev/msgin/routing:    github.com/kartaladev/msgin
github.com/kartaladev/msgin/transform:  github.com/kartaladev/msgin
github.com/kartaladev/msgin/channel:    github.com/kartaladev/msgin
github.com/kartaladev/msgin/resilience: github.com/kartaladev/msgin
```

Root imports nothing from the module; each of the five subpackages imports **only** root. Fan-out from root,
**zero sibling edges** (F9.6, F11.4).

**The invariant is "no core package imports another core package" — not "nothing imports `resilience`".**
Round-2 §A1 falsified the second sentence (3/3 auditors) and it deserved to be falsified: it swept in
`adapter/database/sql` and `adapter/database/sql/harness`, which are *consumers* of the library, not peers of
the core packages. An adapter importing `msgin/resilience` violates nothing. The check with teeth:

```bash
# The subpackage arm MUST subtract the argument packages: `go list -deps` includes them.
go list -deps ./endpoint ./routing ./transform ./channel ./resilience \
  | grep 'kartaladev/msgin/' \
  | grep -vE '^github.com/kartaladev/msgin/(endpoint|routing|transform|channel|resilience)$'   # must be EMPTY

go list -deps . | grep 'kartaladev/msgin/'                                                     # must be EMPTY
```

Run at `dadc775`, both arms are empty (grep exit 1). **The earlier published form of the first arm could not pass
and could not fail informatively**: `go list -deps` emits its own argument packages, so
`go list -deps ./endpoint … | grep 'kartaladev/msgin/'` prints **five** lines on a *correct* tree and six on a
broken one — the gate as written was unsatisfiable, and a worker running it would have concluded the invariant
was violated. The invariant itself always held; only the command was wrong. It was published in four places
(here, §9.1 AC-1, Plan 027 Global Constraint 6, Plan 027 Task 12) and is corrected in all four.

The one edge round 2 found — `poller.go` constructing `ExponentialBackoff` — was **removed, not accepted**,
by giving `endpoint` a local bounded `pollErrorBackoff` (decision **D-A**; ADR 0027 §5a). Adapters do gain
package-level edges into the core subpackages, and that is expected; §3.6 enumerates them.

### 3.1 File-level inventory (normative — generated)

Root goes from **32 source files to 14**.

```
# REGENERATED at dadc775 (round-4 fix pass). Code is byte-identical from 1d7fc80
# through dadc775 — one pin covers every code-derived block in this spec.
$ ls *.go | grep -v _test.go | wc -l
      14
$ ls *.go | grep -v _test.go | tr '\n' ' '
backoff.go channel.go codec.go doc.go errors.go flowcontrol.go groupstore.go handler.go message.go
payload.go reliability.go retry.go spi.go store.go

$ for p in endpoint routing transform channel resilience; do
    printf "%-12s %2d  " $p $(ls $p/*.go | grep -v _test.go | wc -l)
    ls $p/*.go | grep -v _test.go | xargs -n1 basename | tr '\n' ' '; echo; done
endpoint     12  activator.go attempts.go consumer.go credit.go doc.go exchange.go flowcontrol.go gateway.go
                 helpers.go nativereliability.go poller.go producer.go
routing       6  aggregator.go doc.go filter.go helpers.go router.go splitter.go
transform     2  doc.go transformer.go
channel       5  direct.go doc.go pubsub_registry.go pubsub.go queuechannel.go
resilience    4  backoff.go breaker.go doc.go ratelimit.go
```

> **ROUND-4 CORRECTION (B1) — this block omitted all five `doc.go` files and contradicted §3.5 in the same
> document.** It was generated at `c83dde9`, before `1d7fc80` added them (F12.5), and the round-3 pass swept
> the two counts it had flagged (101→102, 42→43) without re-running the *other* blocks the same commit moved.
> **That is the class this fix pass exists to close**, and the pin comment above is the mechanism: a block whose
> header names no commit is unfalsifiable, exactly like §3.6's old working-tree probe (B5).

| Destination | Files (as they exist) | n |
|---|---|--:|
| **root** | `backoff.go` (iface), `channel.go` (ifaces + `Subscription`), `codec.go`, **`doc.go` (NEW)**, `errors.go`, `flowcontrol.go` (ifaces), `groupstore.go`, `handler.go`, `message.go`, `payload.go`, `reliability.go`, `retry.go`, `spi.go`, `store.go` | **14** |
| `endpoint/` | `activator.go`, `attempts.go` (new), `consumer.go`, `credit.go`, **`doc.go` (NEW)**, `exchange.go` (impl half), `flowcontrol.go` (option half), `gateway.go`, `helpers.go` (new), `nativereliability.go` (new), `poller.go`, `producer.go` | 12 |
| `routing/` | `aggregator.go`, **`doc.go` (NEW)**, `filter.go`, `helpers.go` (new), `router.go`, `splitter.go` | 6 |
| `transform/` | **`doc.go` (NEW)**, `transformer.go` | 2 |
| `channel/` | `direct.go` (from `channel.go`'s impl half), **`doc.go` (NEW)**, `pubsub.go` (impl half), `pubsub_registry.go` (impl half), `queuechannel.go` | 5 |
| `resilience/` | `backoff.go` (impl half), `breaker.go`, **`doc.go` (NEW)**, `ratelimit.go` | 4 |
| **deleted** | `expr.go`, `doc_composition.go` (its prose moved into the new root `doc.go` — §3.5) | 2 |

The five subpackage `doc.go` files are **normative** per §3.5 and landed in `1d7fc80`; they are counted here so
this table and §3.5 can no longer disagree.

**Three file names in the table did not exist in the earlier drafts** and are the mechanical consequence of
the helper resolutions in §3.3: `endpoint/helpers.go` and `routing/helpers.go` hold each package's own
`boxMessage`/`nilFuncStep` copies, and `endpoint/nativereliability.go` holds the relocated
`noNativeReliability`. `transform` inlines both helpers directly in `transformer.go` rather than adding a
fourth file, because it has only the one source file.

`poller.go` holds the consumer's poll loop as methods on `*consumer[T]`; **there is no exported `Poller` type
today** — that arrives with RFC-0004 in increment 2. It moves with `consumer.go` because Go requires methods to
live in their receiver's package.

### 3.2 Six files split, not moved — declaration-level (normative, GENERATED)

Each mixes a root-contract declaration with its implementation. **Earlier drafts said two, then five.** It is
**six**: `pubsub_registry.go` is the sixth, per decision **D-B**.

Every row below was produced by parsing the ORIGINAL file's top-level declarations out of the pre-migration
commit and locating each `(kind, name)` pair in the current tree's AST dump — **80 declarations, zero
unlocated** (F11.1):

```
$ mkdir -p <tmpdir>/orig-splits
$ for f in channel.go pubsub.go backoff.go exchange.go flowcontrol.go pubsub_registry.go; do
    git show c83dde9~1:$f > <tmpdir>/orig-splits/$f; done
$ go run docs/plans/027-tools/decls.go <tmpdir>/orig-splits | wc -l
      80
$ # then join against the current tree's decls by (kind, name):
$ for d in . endpoint routing transform channel resilience; do
    go run docs/plans/027-tools/decls.go $d | grep -v '_test\.go' \
      | awk -F'\t' -v D="$d" '{print D"/"$1"\t"$2"\t"$3"\t"$4"\t"$5}'; done > <tmpdir>/new-decls.tsv
```

> **This table is normative and complete.** A declaration that moves without appearing here is a finding; a
> row here that a diff contradicts is a finding. Round-2 §B3's charge was that the hand-written table omitted
> five declarations — it omitted **thirty-one**.

#### `channel.go` → root (interfaces) + `channel` (implementation)

| Declaration | Kind | Vis | From → To |
|---|---|---|---|
| `MessageChannel` | type | exported | `channel.go:10` → **root** `channel.go:20` |
| `DirectChannel` | type | exported | `channel.go:19` → `channel/direct.go:21` |
| `NewDirectChannel` | func | exported | `channel.go:27` → `channel/direct.go:45` |
| `DirectChannel.Subscribe` | method | exported | `channel.go:31` → `channel/direct.go:52` |
| `DirectChannel.Send` | method | exported | `channel.go:46` → `channel/direct.go:82` |

*New in the destination* (from §5's segregation and §5.2's semantics — not moves): root `channel.go:43`
`SubscribableChannel`, root `channel.go:54` `Subscription` (arriving from `pubsub.go`, below), and
`channel/direct.go`'s `directSubscription` (`:33`), `directSubscription.Cancel` (`:42`),
`DirectChannel.release` (`:69`).

#### `pubsub.go` → root (`Subscription`) + `channel` (everything else)

| Declaration | Kind | Vis | From → To |
|---|---|---|---|
| `Subscription` | type | exported | `pubsub.go:37` → **root** `channel.go:54` |
| `FanOutPolicy` | type | exported | `pubsub.go:13` → `channel/pubsub.go:15` |
| `FanOutAllSucceed` | const | exported | `pubsub.go:28` → `channel/pubsub.go:30` |
| `FanOutBestEffort` | const | exported | `pubsub.go:32` → `channel/pubsub.go:34` |
| `pubSubConfig` | type | unexported | `pubsub.go:39` → `channel/pubsub.go:37` |
| `defaultPubSubConfig` | func | unexported | `pubsub.go:44` → `channel/pubsub.go:43` |
| `PubSubOption` | type | exported | `pubsub.go:49` → `channel/pubsub.go:48` |
| `WithFanOut` | func | exported | `pubsub.go:55` → `channel/pubsub.go:54` |
| `WithPubSubLogger` | func | exported | `pubsub.go:59` → `channel/pubsub.go:58` |
| `withConfig` | func | unexported | `pubsub.go:69` → `channel/pubsub.go:87` |
| `subscription` | type | unexported | `pubsub.go:72` → `channel/pubsub.go:90` |
| `subscription.Cancel` | method | exported | `pubsub.go:79` → `channel/pubsub.go:97` |
| `PublishSubscribeChannel` | type | exported | `pubsub.go:86` → `channel/pubsub.go:104` |
| `NewPublishSubscribeChannel` | func | exported | `pubsub.go:95` → `channel/pubsub.go:116` |
| `PublishSubscribeChannel.Subscribe` | method | exported | `pubsub.go:104` → `channel/pubsub.go:128` |
| `PublishSubscribeChannel.remove` | method | unexported | `pubsub.go:116` → `channel/pubsub.go:143` |
| `PublishSubscribeChannel.isEmpty` | method | unexported | `pubsub.go:131` → `channel/pubsub.go:158` |
| `PublishSubscribeChannel.Send` | method | exported | `pubsub.go:149` → `channel/pubsub.go:176` |
| `safeFanOut` | func | unexported | `pubsub.go:173` → `channel/pubsub.go:200` |

*New in the destination:* `channel/pubsub.go:83` `WithSingleSubscriber` (decision D-F, §4.1).

**`Subscription`'s home is root `channel.go`, not a new `subscription.go`** (decision **D-C**). It sits beside
`SubscribableChannel`, the interface that returns it, and root stays at 14 files. Round-2 §A3's charge —
*"`Subscription` has no destination file; 'exactly 14' is unsatisfiable"* — is answered by placement, not by
changing the number.

#### `backoff.go` → root (contract) + `resilience` (implementation)

| Declaration | Kind | Vis | From → To |
|---|---|---|---|
| `BackoffStrategy` | type | exported | `backoff.go:12` → **root** `backoff.go:10` |
| `ExponentialBackoff` | type | exported | `backoff.go:19` → `resilience/backoff.go:12` |
| `ExponentialBackoff.Delay` | method | exported | `backoff.go:27` → `resilience/backoff.go:20` |
| `jitter` | func | unexported | `backoff.go:70` → `resilience/backoff.go:63` |

#### `exchange.go` → root (`RequestReplyExchange`) + `endpoint` (everything else)

| Declaration | Kind | Vis | From → To |
|---|---|---|---|
| `RequestReplyExchange` | type | exported | `exchange.go:35` → **root** `spi.go:118` |
| `defaultReplyTimeout` | const | unexported | `exchange.go:18` → `endpoint/exchange.go:19` |
| `replyCorrelator` | type | unexported | `exchange.go:43` → `endpoint/exchange.go:25` |
| `newReplyCorrelator` | func | unexported | `exchange.go:49` → `endpoint/exchange.go:31` |
| `replyCorrelator.register` | method | unexported | `exchange.go:80` → `endpoint/exchange.go:62` |
| `replyCorrelator.deliver` | method | unexported | `exchange.go:111` → `endpoint/exchange.go:93` |
| `replyCorrelator.closeAll` | method | unexported | `exchange.go:127` → `endpoint/exchange.go:109` |
| `exchangeConfig` | type | unexported | `exchange.go:140` → `endpoint/exchange.go:122` |
| `ExchangeOption` | type | exported | `exchange.go:149` → `endpoint/exchange.go:131` |
| `WithReplyTimeout` | func | exported | `exchange.go:153` → `endpoint/exchange.go:135` |
| `WithUnmatchedReplySink` | func | exported | `exchange.go:172` → `endpoint/exchange.go:154` |
| `WithExchangeClock` | func | exported | `exchange.go:182` → `endpoint/exchange.go:164` |
| `WithExchangeLogger` | func | exported | `exchange.go:198` → `endpoint/exchange.go:180` |
| `ChannelExchange` | type | exported | `exchange.go:209` → `endpoint/exchange.go:191` |
| `NewChannelExchange` | func | exported | `exchange.go:224` → `endpoint/exchange.go:225` |
| `ChannelExchange.receiver` | method | unexported | `exchange.go:256` → `endpoint/exchange.go:266` |
| `ChannelExchange.routeUnmatched` | method | unexported | `exchange.go:269` → `endpoint/exchange.go:279` |
| `ChannelExchange.Exchange` | method | exported | `exchange.go:291` → `endpoint/exchange.go:301` |
| `ChannelExchange.giveUp` | method | unexported | `exchange.go:342` → `endpoint/exchange.go:352` |
| `ChannelExchange.Close` | method | exported | `exchange.go:356` → `endpoint/exchange.go:382` |

**`ChannelExchange.Close` already existed at `exchange.go:356`.** ADR 0028's Consequence *"`ChannelExchange`
**gains** a real `Close`"* was false and is corrected there; §5.2a records what actually changed.

#### `flowcontrol.go` → root (governor interfaces + `OverflowPolicy`) + `endpoint` (options)

| Declaration | Kind | Vis | From → To |
|---|---|---|---|
| `RateLimiter` | type | exported | `flowcontrol.go:16` → **root** `flowcontrol.go:15` |
| `CircuitBreaker` | type | exported | `flowcontrol.go:43` → **root** `flowcontrol.go:42` |
| `ProbeGate` | type | exported | `flowcontrol.go:71` → **root** `flowcontrol.go:70` |
| `OverflowPolicy` | type | exported | `flowcontrol.go:77` → **root** `flowcontrol.go:76` |
| `OverflowBlock` | const | exported | `flowcontrol.go:81` → **root** `flowcontrol.go:80` |
| `OverflowDropNewest` | const | exported | `flowcontrol.go:83` → **root** `flowcontrol.go:82` |
| `OverflowDropOldest` | const | exported | `flowcontrol.go:87` → **root** `flowcontrol.go:86` |
| `OverflowReject` | const | exported | `flowcontrol.go:90` → **root** `flowcontrol.go:89` |
| `OverflowPolicy.String` | method | exported | `flowcontrol.go:94` → **root** `flowcontrol.go:93` |
| `defaultMaxInFlight` | const | unexported | `flowcontrol.go:110` → `endpoint/flowcontrol.go:12` |
| `defaultAttemptTTL` | const | unexported | `flowcontrol.go:116` → `endpoint/flowcontrol.go:18` |
| `defaultPollInterval` | const | unexported | `flowcontrol.go:121` → `endpoint/flowcontrol.go:23` |
| `defaultPollMaxBatch` | const | unexported | `flowcontrol.go:126` → `endpoint/flowcontrol.go:28` |
| `maxPollErrorBackoff` | const | unexported | `flowcontrol.go:131` → `endpoint/flowcontrol.go:33` |
| `WithMaxInFlight` | func | exported | `flowcontrol.go:137` → `endpoint/flowcontrol.go:39` |
| `WithRateLimit` | func | exported | `flowcontrol.go:142` → `endpoint/flowcontrol.go:44` |
| `WithHandlerTimeout` | func | exported | `flowcontrol.go:153` → `endpoint/flowcontrol.go:55` |
| `WithCircuitBreaker` | func | exported | `flowcontrol.go:158` → `endpoint/flowcontrol.go:60` |
| `WithOverflow` | func | exported | `flowcontrol.go:168` → `endpoint/flowcontrol.go:70` |
| `WithAttemptTTL` | func | exported | `flowcontrol.go:189` → `endpoint/flowcontrol.go:91` |
| `WithMaxPayloadBytes` | func | exported | `flowcontrol.go:206` → `endpoint/flowcontrol.go:108` |
| `WithPollInterval` | func | exported | `flowcontrol.go:217` → `endpoint/flowcontrol.go:119` |
| `WithPollMaxBatch` | func | exported | `flowcontrol.go:228` → `endpoint/flowcontrol.go:130` |

Keeping all four governor declarations in root is ADR 0027 §5's decision. It keeps the organising principle
exceptionless and keeps `adapter/memory/queuestore.go:74`'s
`func WithOverflow(p msgin.OverflowPolicy) QueueStoreOption` compiling against root unchanged.

#### `pubsub_registry.go` → root (SPI) + `channel` (registry) — **the sixth split, decision D-B**

| Declaration | Kind | Vis | From → To |
|---|---|---|---|
| `TopicPublisher` | type | exported | `pubsub_registry.go:11` → **root** `spi.go:91` |
| `TopicSubscriber` | type | exported | `pubsub_registry.go:19` → **root** `spi.go:99` |
| `PubSub` | type | exported | `pubsub_registry.go:26` → `channel/pubsub_registry.go:13` |
| `NewPubSub` | func | exported | `pubsub_registry.go:38` → `channel/pubsub_registry.go:25` |
| `PubSub.Publish` | method | exported | `pubsub_registry.go:49` → `channel/pubsub_registry.go:36` |
| `PubSub.Subscribe` | method | exported | `pubsub_registry.go:62` → `channel/pubsub_registry.go:49` |
| `PubSub.TopicCount` | method | exported | `pubsub_registry.go:88` → `channel/pubsub_registry.go:80` |
| `topicSubscription` | type | unexported | `pubsub_registry.go:95` → `channel/pubsub_registry.go:87` |
| `topicSubscription.Cancel` | method | exported | `pubsub_registry.go:103` → `channel/pubsub_registry.go:95` |

`TopicPublisher`'s own godoc says *"Native-topic broker adapters (Kafka, NATS, Redis) implement this using
their own topics, so topic support is handled generically through one SPI"*, and both ADR 0014 and Spec 004
file the pair as **Layer 1 — SPI**. Moving the file whole would have put a root SPI seam in `channel`, so a
future NATS/Redis topic adapter would import `msgin/channel` to implement it — inverting the "adapters
implement root SPI" rule with no decision recorded (round-2 §A4). The split keeps the seam in root.

**Retry policy stays in root.** `RetryPolicy` and `Hooks` are caller-facing configuration value types written
as literals throughout `harness` (`msgin.RetryPolicy{MaxAttempts: 3, DeadLetter: dlq}`), not implementations.

**Every error sentinel a core subpackage returns stays in root, in `errors.go`** — including ones only a
subpackage returns (`ErrNoRoute`, `ErrNilFunc`, `ErrChannelSubscribed`):

```
# scope: root module, at dadc775
$ go run docs/plans/027-tools/decls.go . | grep -v _test.go | awk -F'\t' '$3=="var" && $4 ~ /^Err/ {print $1}' | sort | uniq -c
  43 errors.go
```

**The invariant is "no `Err*` var outside `errors.go` in the six core packages", not a count.** Three rounds
of this bundle have corrected a *number* and had the number break again; the check below §3.2's rule is the
contract. The count at `dadc775` is **43** (it was 42 before `1d7fc80` added `ErrNilSubscription`), and it is
projected to be **42** after D-I removes two and D-J adds one — Spec §4 carries that arithmetic.

**43, not 89.** The "89" in earlier drafts came from `grep -c 'Err[A-Z]' errors.go`, which counts godoc lines
(round-2 §A5, F1).

> **CORRECTED (round 3) — the design rationale rested on a false premise.** This paragraph asserted *"No
> `Err*` var is declared in any other file in the workspace"* and reasoned from it that *"one import for the
> whole error contract beats six"*. **There are 51 such vars**, and the repository's own precedent argues the
> other way:
>
> ```
> $ for d in . endpoint routing transform channel resilience adapter/memory adapter/cron adapter/http \
>        adapter/http/stdlib adapter/database/sql adapter/database/sql/{harness,postgres,mysql,sqlite,dbtest} \
>        adapter/cron/crontest; do
>     go run docs/plans/027-tools/decls.go $d | grep -v '_test\.go' \
>       | awk -F'\t' -v D="$d" '$3=="var" && $4 ~ /^Err/ {print D"/"$1}'; done | sort | uniq -c
> # REGENERATED at dadc775 (round-5). NOTE the sort order: `sort | uniq -c` sorts by PATH, not by count —
> # the previous version of this block printed it descending by count, which is not what the command emits.
>   43 ./errors.go
>    9 adapter/cron/errors.go
>    1 adapter/cron/sqlutil.go
>   15 adapter/database/sql/errors.go
>   26 adapter/http/errors.go
>    1 adapter/cron/sqlutil.go
> ```
>
> The 42-in-`errors.go` figure was always **root-scoped**, and the sentence generalised it to the workspace —
> Plan 027 Global Constraint 0's exact failure shape, a third time.

**The rule that is actually true, and is the one this spec adopts:**

> **A package owns its own error sentinels when callers import it directly; a package whose only import is
> root shares root's closed error contract.** `adapter/http`, `adapter/database/sql`, and `adapter/cron` are
> imported *by consumers*, so each owns an `errors.go` (26 / 15 / 9 sentinels, plus one in
> `adapter/cron/sqlutil.go`). The five core subpackages import **only** root and are imported alongside it in
> every realistic flow, so a caller who has `endpoint` already has `msgin` — splitting root's sentinels across
> six core packages would buy nothing and cost an import per `errors.Is`. *(Stated without a count on purpose:
> the number has moved three times and the rule has not.)*

So `ErrNoRoute`, `ErrNilFunc`, and `ErrChannelSubscribed` stay in root even though only a subpackage returns
them. A reader may expect `routing.ErrNoRoute`; they get `msgin.ErrNoRoute`, and the rule above says why. The
mechanical check is scoped accordingly — **the six core packages, not the workspace**:

```bash
for p in endpoint routing transform channel resilience; do
  go run docs/plans/027-tools/decls.go $p | grep -v '_test\.go' \
    | awk -F'\t' '$3=="var" && $4 ~ /^Err/ {print}'
done                                        # must be EMPTY
```

> **DECIDED (decision D-I, 2026-07-28): the two orphaned expr sentinels LEAVE root.** `ErrInvalidExpression`
> (`errors.go:168`) and `ErrExprResultType` (`errors.go:193`) had **zero users** after the `*Expr` deletion and
> their godoc named constructors that no longer exist (F11.7). They are **deleted from `errors.go`**, and the
> `expr` module (Task 10) declares its own. Root's contract then has a producer inside the root module for
> every sentinel it declares — the claim a *closed* contract has to be able to make.
>
> **The measured precedent, and why it does not contradict this.** The rule above is symmetric, and both arms
> are visible in the tree today:
>
> ```
> # scope: whole workspace, at dadc775 (code byte-identical since 1d7fc80)
> $ grep -rn --include='*.go' -E '^\tErr[A-Za-z]+ +=' . | grep -v _test.go \
>     | sed 's|/[^/]*\.go:.*||' | sort | uniq -c | sort -rn | grep -v '^ *1 errors\.go'
>   26 adapter/http
>   15 adapter/database/sql
>    9 adapter/cron
>
> $ grep -cE '^\tErr[A-Za-z]+ +=' errors.go              # root, before this decision lands
> 43
>
> $ grep -rn --include='*.go' 'msgin\.Err[A-Za-z]*' adapter/ | grep -v _test \
>     | sed 's/:.*msgin\.\(Err[A-Za-z]*\).*/ -> \1/' | sort -u | wc -l
>       27
> ```
>
> A directly-imported package **mints its own sentinel for its own faults** (26 + 15 + 9 across the three
> adapters, **plus one** in `adapter/cron/sqlutil.go` that the `^\t` form does not match — 51 total, the same
> figure the rule paragraph above cites) and **returns root's for contract-level faults** (27 distinct
> file→sentinel pairs: `ErrNilAdapter`, `ErrInvalidCapacity`, `ErrOverflowDropped`, `ErrReplyTimeout`,
> `ErrDuplicateCorrelation`, `ErrGatewayClosed`, …). An invalid
> *expression* is a fault of the expression provider, not of root's messaging contract — root has no notion of
> an expression and, after Task 1, no code that could produce one. It belongs on the first arm.
>
> The cost is stated rather than hidden: a caller who catches both a root fault and an expr fault writes two
> `errors.Is` targets from two packages. They already import `expr` to construct the endpoint, so the import is
> not new.
>
> **This was the last of the two open decisions the round-3 cycle left; it is no longer contingent.** Its
> numeric consequences are carried in §4 and §4.1, and **Task 12 must re-measure them rather than transcribe
> them** — every number below is a projection until it is.

### 3.3 Symbol-level resolution: shared unexported identifiers (normative)

A file-level move-list is blind to unexported identifiers that cross the new boundaries, and — round-2 §B1's
lesson — **it is also blind to unexported *struct field* access, which is a different category from a
declaration**. Both classes are recorded here.

#### 3.3a Declaration crossings — 8 genuine, all resolved

| Symbol | Was | Resolution | Where it landed |
|---|---|---|---|
| `boxMessage` | `payload.go` [root] | **Inline** a copy per destination package | `endpoint/helpers.go:12`, `routing/helpers.go:14`, `transform/transformer.go:47` |
| `nilFuncStep` | `transformer.go` | **Inline** a copy per destination package | `endpoint/helpers.go:19`, `routing/helpers.go:21`, `transform/transformer.go:36` |
| `isPermanent` | `reliability.go` [root] | **Export as `IsPermanent`** — forced (`errors.As` over unexported `*permanentError`) | root `reliability.go:38` |
| `retryAfterOf` | `reliability.go` [root] | **Export as `RetryAfterOf`** — forced (`*retryAfterError`) | root `reliability.go:107` |
| `randomID` | `message.go` [root] | **Export as `NewID`** — chosen, settled with the user 2026-07-27 | root `message.go:190` |
| `noNativeReliability` | `reliability.go` [root] | **Move to `endpoint`** | `endpoint/nativereliability.go:7` |
| `attemptTracker`, `attemptEntry`, `newAttemptTracker` | `retry.go` [root] | **Move to `endpoint`** | `endpoint/attempts.go:26,13,33` |
| `RetryPolicy.delayFor` | `retry.go` [root] | **Delete the method; package-local helper in `endpoint`** | `endpoint/consumer.go:948` `retryDelay` |

**Dissolved as predicted — 10.** Four with `expr.go`'s deletion (`asInt`, `firstHeader`, `aggregatorConfig`,
`forwardSplit` — each now single-package in `routing`), six with the `flowcontrol.go` split (`consumerConfig`,
`defaultMaxInFlight`, `defaultAttemptTTL`, `defaultPollInterval`, `defaultPollMaxBatch`,
`maxPollErrorBackoff` — all landed in `endpoint` alongside their users).

**Two audit rows were false positives, and stay discarded:** `breaker` (a *field name* of type
`CircuitBreaker`, not the unexported struct) and `jitter` (declared and used only in `backoff.go`, so it
travels to `resilience` cleanly). Both re-verified by all three round-2 auditors (round-2 §E).

> **Round-2 §D13 is confirmed and corrected here:** `attemptEntry` is used from `retry.go`, **not**
> `consumer.go`. It travels with `attemptTracker` either way.
> **Round-2 §D11 is confirmed:** `delayFor` had **three** call sites, not two (`consumer.go:730,779`,
> `producer.go:485`). The resolution is unchanged; only the count was wrong.

#### 3.3b Field-access crossings — the class a declaration grep cannot see (decision D-H)

`endpoint` read `Message`'s unexported `payload`/`headers` fields at **six** sites. This is not an identifier
crossing; it is direct struct-field access on a type that stays in root, and no grep over *declarations* can
find it. The compiler found only five of the six, because Go caps at ten errors per package and truncated
(F7) — so the site list was derived by `grep`, then confirmed by the compiler, never the reverse:

```
$ grep -rn '\.payload\|\.headers' endpoint/           # BEFORE the fix
endpoint/producer.go:417:  return msgin.Message[any]{payload: msg.payload, headers: msg.headers}, nil
endpoint/producer.go:419:  b, err := p.codec.Encode(msg.payload)
endpoint/producer.go:423:  return msgin.Message[any]{payload: any(b), headers: msg.headers}, nil
endpoint/consumer.go:694:  msg := msgin.Message[T]{payload: payload, headers: d.Msg.headers}
endpoint/consumer.go:828:  v, ok := m.payload.(T)
endpoint/consumer.go:835:  b, ok := m.payload.([]byte)
```

`producer.go:423` was **never surfaced by the compiler**.

**Resolution (forced, not a choice): rewrite over `msgin.NewMessage[T](payload, m.Headers())` and
`m.Payload()`.** `NewMessage` is literally `Message[T]{payload: payload, headers: headers}`
(`message.go:184-186`) and `Headers` wraps a map, so passing `m.Headers()` aliases the same map the struct
literal did — **identity is preserved bit-for-bit**.

> **NEVER `msgin.New[T]`.** It re-stamps `msgin.message-id` and `msgin.timestamp` on every consumed message,
> and **no existing assertion would catch the regression**. Verified absent:
> `grep -rn 'msgin\.New\[' endpoint/` → exit 1.

**D-H costs nothing structurally.** The `endpoint → root` edge needed no widening and no new exported symbol;
the `Payload()`/`Headers()`/`NewMessage` surface was already sufficient (F7).

#### 3.3c No `internal/` package is created

Every crossing resolves by inlining over public API, by moving the symbol to the package that uses it, or by
an export that was independently justified. Adding `internal/` for two one-line helpers would be more
structure than the problem has. Recorded so the omission reads as a decision.

**Residual, and remaining work:** root's `boxMessage` (`payload.go:30`) and `nilFuncStep` (`handler.go:66`)
are now **dead code** — zero users in root, zero in root's tests — and nothing reports it, because
`.golangci.yml` sets `linters.default: none` so `unused` is off (F11.6). **DELETED in the round-3 pass**
(F12.4) — `apidiff` still reported the **same** removal count afterwards, verified by re-running it rather than
assumed, because both helpers were unexported. *(The additions count moved from 5 to 6 in that same commit,
`1d7fc80`, but for an unrelated reason: `ErrNilSubscription`. The two changes rode together and only the
removals arm is evidence about the dead-helper deletion.)* Root now has **zero uncovered blocks** (§9 AC 7).

### 3.4 Test-file placement (normative — generated)

Tasks that say "move the matching `_test.go`" fail on the root test files that have no single matching source
file. The placement rule that resolves this:

> **A `package X_test` binary may import any package.** So a test that spans packages needs a **home**, not a
> split: place it where its system-under-test lives and let it import the rest.

**The rule has one documented failure mode, and it is the one that actually bit.** *Production* packages are
importable; **test packages are not**. A `package endpoint_test` binary cannot reach an unexported identifier
declared in `package msgin_test` by any mechanism, and `go build ./...` never compiles test binaries, so this
class is invisible until `go vet` or `go test`. §3.4c is therefore part of the normative placement, not an
appendix.

#### 3.4a State the frame before quoting a number

Three different totals are all correct, in three different frames (F8.0, F11.3):

| Frame | Count | Command |
|---|--:|---|
| Baseline inventory, before any change | **45** | `ls *_test.go \| wc -l` at `c83dde9~1` |
| Files **placed** by the migration | **44** | 45 − `expr_test.go`, deleted with `expr.go` |
| Test files in the tree at `b6ce7bb` | **45** | 44 placed + `capability_test.go`, which arrived at `b6ce7bb` (§9.4) |
| **Test files in the tree at `dadc775`** | **50** | 45 + the five `main_test.go` added by `1d7fc80` |

> **ROUND-5 CORRECTION (B1) — a relabel is not a re-measurement.** The third row read *"in the tree today | 45"*;
> the round-4 pass replaced *today* with the pin `c83dde9` **without re-running the count**. At `c83dde9` the
> real count is **44** (`git ls-tree -r --name-only c83dde9 | grep '_test\.go$' | grep -v '^adapter/' | wc -l`),
> because `capability_test.go` arrived one commit later at `b6ce7bb` — so the row as pinned was both false and
> identical to the "placed" frame above it. **Converting an unfalsifiable claim into a falsifiable one is only
> progress if you then run it.** The frame is `b6ce7bb`.

```
# REGENERATED at dadc775 (round-4 fix pass)
$ printf "root %d\n" $(ls *_test.go | wc -l); \
  for d in channel endpoint resilience routing transform; do printf "%-11s %d\n" $d $(ls $d/*_test.go | wc -l); done
root        12
channel      8
endpoint    16
resilience   4
routing      8
transform    2                     # TOTAL 50
```

> **ROUND-4 CORRECTION (B8) — the "today" frame was stale by five, and the §3.4b census below has no row for
> any of them.** `1d7fc80` ("restore the goleak net") added `main_test.go` to **root, `channel`, `resilience`,
> `routing`, `transform`** — five files, five packages.
>
> **`endpoint` is deliberately NOT among them, and this is not a gap.** Its `goleak.VerifyTestMain` lives in
> `consumer_test.go:25`, which predates the split:
> ```
> $ grep -rn 'func TestMain' --include='*_test.go' . | grep -v adapter/
> main_test.go:14:func TestMain(m *testing.M) {
> channel/main_test.go:13:func TestMain(m *testing.M) {
> resilience/main_test.go:12:func TestMain(m *testing.M) {
> endpoint/consumer_test.go:25:func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }
> transform/main_test.go:12:func TestMain(m *testing.M) {
> routing/main_test.go:13:func TestMain(m *testing.M) {
> ```
> **Do not "fix" this by adding `endpoint/main_test.go`** — Go permits exactly one `TestMain` per package, so
> it would not compile. If the file-name inconsistency is ever worth closing, the edit is to *move* the
> existing `TestMain` out of `consumer_test.go`, not to add a second one.

#### 3.4b Placement census (from the green tree)

Columns are **reference counts by package**, measured after placement. "SUT symbols" are the top-3
most-referenced symbols of the destination package — the evidence that decided the file.

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
| `spi_test.go` | **root** | 0 | 0 | 0 | 0 | 0 | 7 | `msgin.Delivery` `msgin.EventDrivenSource` `msgin.OutboundAdapter` |
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
| `settlement_doubles_test.go` | endpoint | 0 | 0 | 0 | 0 | 0 | 38 | *(shared doubles — placed with its four users)* |
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
root 10   channel 7   endpoint 16   resilience 3   routing 7   transform 1     TOTAL 44   (the "placed" frame)
```

**ZERO files are split — not one, and not at the case level either.** §3.4's earlier claim that
`retry_test.go` splits *"at the case level"*, handing its `ExponentialBackoff` cases to `backoff_test.go`, is
a **fabrication with no source to cut from** (round-2 §A7, F8.3):

```
$ git show c83dde9~1:retry_test.go | grep -n "ExponentialBackoff"
26:  msgin.RetryPolicy{MaxAttempts: 3, DeadLetter: nopSink{}, Backoff: msgin.ExponentialBackoff{Initial: time.Millisecond}},
$ git show c83dde9~1:backoff_test.go | grep -c "Delay("
4
```

`retry_test.go`'s single reference is a **fixture value** inside a `RetryPolicy` literal driving `Validate` —
not a delay-computation case. All four `Delay()` cases have always lived in `backoff_test.go`, which §3.4
already listed correctly under `resilience`. The correct mechanical outcome is **two whole-file moves and no
split**.

The multi-package rows (`exchange_test.go` 53 endpoint / 38 channel; `queuechannel_e2e_test.go` 3 endpoint /
1 channel) are the placement rule working as designed.

#### 3.4c Cross-file test-identifier inventory — the class `go build` cannot see (normative)

92 package-level identifiers are declared across the root `_test.go` files; 35 are referenced by more than one
file. Derived at the **AST level**, because `grep -w` over-reports badly — `settle`, `order`, and `backlog`
are ordinary English words that appear in comments, and `grep -w` falsely claimed `settle` was used by three
files the AST says never touch it (F8.4).

**Exactly two identifiers cross a destination boundary, and both were invisible to `go build`:**

| identifier | declared in → dest | users → dest | resolution |
|---|---|---|---|
| `collector` (a 6-line recording `MessageChannel`) | `expr_test.go` → **deleted** | `gateway_test.go` (9×) → endpoint | **Re-declared in `gateway_test.go`**, so it travels to `endpoint` with its only user. Deleting `expr_test.go` left the root test binary RED: `vet: ./gateway_test.go:142:34: undefined: collector` (F2). |
| `order` | `codec_test.go` → **root** | `consumer_test.go`, `consumer_governor_panic_test.go`, `consumer_probegate_wiring_test.go`, `flowcontrol_test.go`, `poller_test.go`, `producer_scheduled_test.go`, `producer_test.go`, `settlement_doubles_test.go` — **all 8** → endpoint | **Duplicated** into `endpoint/settlement_doubles_test.go` (7-line struct + rationale comment). Test doubles; duplication is correct. |

The 25 doubles declared in `settlement_doubles_test.go` (`lockedBuffer`, `recordingSink`, `settle`,
`scriptedSource`, `newSettleDelivery`, …) are used by four files that **all** go to `endpoint`, which is why
that file moves whole and not split — the census proves it rather than asserting it.

**The invariant the plan must encode is the check, not the list:**

```bash
# no package-level test identifier may have a user in a different destination package
go vet ./...                                  # compiles every TEST binary; `go build` does not
```

#### 3.4d Placements a hand-written table gets wrong

| file | naive guess | derived | why |
|---|---|---|---|
| `handler_test.go` | `endpoint` (it exports a `Handler` type) | **root** | Its SUT is `msgin.Chain`/`msgin.To`/`msgin.Step`, all still in root `handler.go`. `endpoint.Handler` is an unrelated generic type; the name collision is a trap. |
| `queuechannel_e2e_test.go` | `endpoint` (3 endpoint refs vs 1 channel ref) | **channel** | Reference *count* is not the SUT. `TestQueueChannel_EndToEnd` drives `channel.NewQueueChannel`; `NewProducer`/`NewConsumer` are the harness. Counting references places this file wrong. |
| `pubsub_integration_test.go` | `endpoint` | **channel** | Same shape: `NewConsumer` is the driver, `channel.PubSub` is the SUT. |
| `example_composition_test.go` | `endpoint` | **root** | Its SUT is `Chain`, which stays in root (§4), and it is what carries the Pipes-and-Filters narrative out of the deleted `doc_composition.go` (§3.5). |
| `flowcontrol_test.go` | root (`OverflowPolicy` stays in root) | **endpoint** | It drives `NewConsumer` + the `With*` options, which move. §3.4e is the consequence. |

#### 3.4e Coverage attribution across the split — read this before comparing any number

Coverage is credited to the package where the **test binary** lives, not where the code lives. Moving a
blackbox test to a sibling package therefore *moves its coverage credit* without dropping a single case.
Measured (F11.11):

```
# REGENERATED at dadc775 (round-4 fix pass); stable across 3 runs
$ GOWORK=off go test ./... -count=1 -cover          # DEFAULT per-package
msgin 95.3%   channel 100.0%   endpoint 99.1%   resilience 99.1%   routing 100.0%   transform 100.0%
```

> **ROUND-4 CORRECTION (B2) — this block read `msgin 81.8%` and was stale by 13.5 points, inside the section
> that *teaches* Global Constraint 0.** 81.8% was the `b6ce7bb` tree; `1d7fc80` then deleted root's dead
> helpers and added `capability_test.go`/`main_test.go`, taking root to **95.3%**. The five sibling figures
> were unaffected and still reproduce exactly.
>
> **The consequence is not cosmetic: root now PASSES the 85% gate on the default profile.** The argument
> below — *"a default-vs-default comparison … fails CLAUDE.md's 85% gate"* — was the original justification
> for the `-coverpkg` rule and **is no longer true of root**. The rule itself still stands, on the narrower
> and still-valid ground that a default comparison across a package split is **not like-for-like**: credit
> follows the test binary, so the numbers describe different things on each side. Do not restate the
> now-false 85% claim; restate the attribution one.

**Name the tree, or the number is meaningless.** Every `-coverpkg=./...` figure below is labelled with the
commit it was taken from, and each was re-run for this document:

| Tree | What it is | `-coverpkg=./...` total |
|---|---|--:|
| `ab233d9` | **pre-refactor** — the flat core, before Task 1 | **93.5%** |
| `c83dde9` | post-extraction, pre-channel-segregation | 93.2% |
| `b6ce7bb` | post-segregation, pre-round-3 fixes | 93.3% |
| **HEAD** (working tree) | after the round-3 code fixes | **93.4%** |

```
$ git archive ab233d9 | tar -x -C <tmp> && (cd <tmp> && GOWORK=off go test ./... -count=1 \
    -coverpkg=./... -coverprofile=ab.cov && go tool cover -func=ab.cov | tail -1)
total:  (statements)  93.5%
$ GOWORK=off go test ./... -count=1 -coverpkg=./... -coverprofile=head.cov \
    && go tool cover -func=head.cov | tail -1
total:  (statements)  93.4%
```

> **CORRECTED (round 3).** F10.8's column headed **BASELINE (93.23%)** is **not the pre-refactor tree** — it
> is the post-extraction tree, measured after Tasks 1–8 had already landed. The true pre-refactor
> `-coverpkg=./...` figure is **93.5%** (`ab233d9`), so the honest whole-window statement is
> **93.5% → 93.4%, a −0.1pt movement**, not the "+0.2pt gain" F12.8 reported by comparing a `-coverpkg`
> number against a *default*-profile 91.9%. The three cited numbers were each correct about their own tree;
> the sentence around them named the wrong one. Plan 027 Global Constraint 0 exists for exactly this.
> **−0.1pt is not a regression to chase**: it is `endpoint/consumer.go:467`'s race arm, covered in roughly
> 1 run in 3 (AC 7).

Root's drop at `b6ce7bb` (**99.3% → 81.8%**) was entirely this artifact, and it is the reason the rule exists.
**At `dadc775` root reads 95.3%**, because `1d7fc80` removed the dead helpers that were dragging it down — so
the *illustration* is historical while the *rule* is permanent. Two root declarations that round-2 §A8 measured
at 0% — `OverflowPolicy.String` and `(*permanentError).Error` — are still fully exercised, just from
`endpoint`'s binary, which is the attribution effect in its pure form.

> **Normative — and it now takes BOTH arms, not one.**
>
> 1. **Every whole-tree coverage comparison uses `-coverpkg=./...` on both sides.** A default-vs-default
>    comparison across a package split is **not like-for-like**: credit follows the test binary, so the two
>    sides measure different things. *(This is the whole justification. The older form added "and fails
>    CLAUDE.md's 85% gate falsely on every extraction task" — true at `b6ce7bb`, **false at `dadc775`**, where
>    root reads 95.3%. Round-4 B2: do not restate the gate claim.)*
> 2. **Any task that adds an exported symbol to a package whose tests live elsewhere ALSO checks the
>    per-package profile.** `-coverpkg` aggregates, so it cannot see a symbol that is uncovered *in its own
>    package* but exercised from a sibling — it reports 100%. Task 9.6 is the live case: its two new `channel`
>    methods are driven only from `endpoint`, and the package-local profile shows `channel` falling
>    100.0% → 98.3% while `-coverpkg` shows both methods fully covered.
>
> Plan 027 Global Constraint 4 states both; round-2 §A8 showed the pre-`-coverpkg` wording actively
> misdiagnosed arm 1 (*"a pure move that loses coverage means tests were dropped"* — the worker is sent
> hunting a bug that does not exist), and the round-4 design audit showed arm 1 alone hides arm 2.

### 3.5 Package documentation (normative)

`doc_composition.go` held the **only** `// Package msgin` comment in the repository and it was deleted. No
lint catches the omission — `.golangci.yml` sets `linters.default: none` and does not enable `ST1000`.

- **Root gained a new `doc.go`** carrying the package comment. **DONE** — verified:
  `grep -rn '^// Package ' *.go` → `doc.go:1`.
- **Each new package gets its own `doc.go`.** **DONE** — all five landed in the round-3 fix pass (F12.5),
  closing F11.8. Each names its EIP chapter and its Spring counterpart, and each discharges CLAUDE.md's
  multi-instance-awareness rule for the components it introduces (§10).
- **The Pipes-and-Filters framing must survive the deletion.** `doc_composition.go:4` stated the model —
  *"endpoints wired as pipes and filters. A `MessageHandler` is one step; a `MessageChannel` is the
  conduit"*. That sentence is the reason `MessageChannel` and `OutboundAdapter` stay distinct (§5.3) and the
  reason `Chain` stays in root (§4), so it is **normative content**. Root's `doc.go` must state: a
  **`MessageChannel` is the Pipe**, a **`Step` is the filter**, and **`Chain` assembles the pipeline**.
- **Each subpackage doc names its EIP chapter and its Spring counterpart, where one exists.**
  `channel` → EIP ch.3 (Pipe) and ch.4 (Publish-Subscribe Channel), `org.springframework.integration.channel`;
  `endpoint` → ch.10, `…integration.endpoint`; `routing` → ch.7, `…integration.router`; `transform` → ch.8,
  `…integration.transformer`. **`resilience` has neither** (round-2 §D15) — its doc states that explicitly and
  cites [ADR 0006](../adrs/0006-resilience-flow-control.md) instead, rather than inventing a chapter.
- **Exactly one `// Package` comment per package — and the check is a COUNT, not `go vet`.**

  > **CORRECTED (round 3), proven by execution.** This bullet asserted *"a duplicate after a merge is a
  > `go vet` failure"*. It is not. A second `// Package transform` comment was planted in a throwaway file and
  > every tool in the gate passed it: `go build ./transform/` exit 0, `go vet ./transform/` exit 0,
  > `gofmt -l transform/` empty, `golangci-lint run ./transform/` → *0 issues*, and `go doc ./transform`
  > rendered without a warning. `ST1000` would not help either — it is off under
  > `.golangci.yml`'s `linters.default: none`, and in any case checks for a *missing* comment, not a duplicate.
  > The same false claim appears in Plan 027 Global Constraint 3 and Task 11's Verify; all three are replaced
  > with the assertion below, which **does** fail (`FAIL transform has 2`).

  ```bash
  for p in . endpoint routing transform channel resilience; do
    n=$(grep -l '^// Package ' $p/*.go 2>/dev/null | wc -l | tr -d ' ')
    [ "$n" = 1 ] || { echo "FAIL $p has $n"; exit 1; }
  done
  ```

  Run at `dadc775` it is silent (exit 0) — all six packages have exactly one.

### 3.6 Adapter and satellite-module blast radius (measured)

Round-2 §A2 (3/3 auditors) found the earlier *"the known non-test adapter code changes are exactly two
sites"* to be understated by ~70–120 sites, with **no task migrating the adapter tree at all**. The measured
figure, from the completed migration (F9.2, F9.3, F9.9):

> **The range is IN the command — and BOTH ENDS of it must be a SHA.** The figures below are re-derived over
> the whole window with the right-hand end **pinned to `dadc775`**, not to `HEAD`.
>
> **ROUND-4 CORRECTION (B6).** The previous version of this block wrote the range as `c83dde9~1..HEAD` and
> published **31 files, +239/−191**. That was exactly right at `0e2dcf0` (re-verified: `git diff --stat
> c83dde9~1..0e2dcf0 -- adapter/` still prints 31/239/191) and wrong at every commit after it, because
> `1d7fc80` re-tidied six satellite `go.mod`/`go.sum` files. **A range ending in `HEAD` is not a pin** — it
> silently re-evaluates on every commit, which is how this table went stale for the *third* time (round-2 §A2,
> round-3 §3.6, now round 4). Naming a SHA is the only form that can be checked later.

```
# scope: adapter/ subtree, range c83dde9~1..dadc775 (both ends fixed)
$ git diff --stat c83dde9~1..dadc775 -- adapter/ | tail -1
 43 files changed, 244 insertions(+), 220 deletions(-)
```

**43 files, +244/−220 across the whole window.** Rolled up per module (`git diff --numstat
c83dde9~1..dadc775 -- adapter/`, bucketed by path):

| module / package | files | +/− | published at `0e2dcf0` |
|---|--:|---|---|
| `adapter/cron` | 3 | +8 −7 | 3 / +8 −7 *(unchanged)* |
| **`adapter/cron/crontest`** (separate module) | 2 | +0 −3 | *(absent — `go.mod`/`go.sum` re-tidy)* |
| `adapter/database/sql` | 7 | +15 −14 | 7 / +15 −14 *(unchanged)* |
| **`adapter/database/sql/harness`** (separate module) | 8 | +93 −80 | 6 / +93 −77 |
| **`adapter/database/sql/{mysql,sqlite,dbtest}`** (separate modules) | 6 | +2 −15 | *(absent — re-tidy)* |
| `adapter/database/sql/postgres` (separate module) | 3 | +9 −12 | 1 / +8 −6 |
| `adapter/http` | 11 | +86 −69 | 11 / +86 −69 *(unchanged)* |
| `adapter/http/stdlib` | 1 | +27 −16 | 1 / +25 −14 |
| `adapter/memory` | 2 | +4 −4 | 2 / +4 −4 *(unchanged)* |

**The twelve newly-visible files are all `go.mod`/`go.sum`**, from `1d7fc80`'s seven-module `expr-lang` re-tidy
(F12.1) — which is also why §7's dependency claim and this section must move together. Four rows are
genuinely unchanged; the rest moved only by that re-tidy plus `adapter/http/stdlib`'s round-3 test edits.

The **occurrence classification** below is scoped to the `c83dde9` requalification pass, which is the only
thing it ever measured; `b6ce7bb`'s additional edits are call-form churn and the rename, not requalification:

```
$ cut -f3 adapt-classify.tsv | sort | uniq -c        # scope: the c83dde9 requalification pass only
 115 CODE
  39 COMMENT
                    # 0 STRING, 0 unclassifiable — the two rewrite passes are provably exhaustive
```

**No `go.mod` needed an edit FOR THE REQUALIFICATION.** Every satellite already `require`s **and** `replace`s
the root module, and `endpoint`/`routing`/`channel`/`resilience` are packages *inside that same module*, so
moving code between them cannot change a module graph:

```
# scope: adapter/ module files, range c83dde9~1..c83dde9 (the requalification commit alone)
$ git diff --stat c83dde9~1..c83dde9 -- 'adapter/**/go.mod' 'adapter/**/go.sum'
(no output)

# the SAME files over the whole window — twelve edits, none of them requalification
$ git diff --stat c83dde9~1..dadc775 -- 'adapter/**/go.mod' 'adapter/**/go.sum' | tail -1
 12 files changed, 3 insertions(+), 27 deletions(-)
```

> **ROUND-4 CORRECTION (B5) — the old claim was unqualified and its proof could not falsify it.** The sentence
> read *"No `go.mod` needed a single edit"* and was backed by `git status --short`, a **working-tree** command
> that returns empty by construction once the change is committed. It would print nothing whether the claim
> were true or false. Twelve module files *were* edited over the window — by `1d7fc80`'s `expr-lang` re-tidy,
> not by the move — so the claim is true only with its scope restored, and the proof is now a committed-range
> diff that can actually contradict it. **A verification command that cannot fail is not a verification.**

New **package**-level edges into the core subpackages (`go list`, including test imports):

| adapter package | edge kind | → core subpackage |
|---|---|---|
| `adapter/cron` | XTEST | `endpoint` |
| `adapter/database/sql` | **NONTEST** | `resilience` |
| `adapter/database/sql` | XTEST | `endpoint` |
| **`adapter/database/sql/harness`** | **NONTEST** | `channel`, `endpoint`, `resilience`, `routing` |
| `adapter/database/sql/postgres` | XTEST | `channel`, `routing` |
| `adapter/http` | XTEST | `channel`, `endpoint`, `resilience` |
| `adapter/http/stdlib` | XTEST | `channel`, `endpoint` |

**Two blind spots this measurement exposes, both normative for the plan:**

1. **`harness` has zero test files of its own** (`go test` reports `[no test files]`) yet carries **69
   non-test selectors** in `lock.go`/`outbound.go`/`source.go`/`groupstore.go`/`harness.go`. A gate that runs
   `go test` per module and reads "no test files" as a pass misses every one of them. Only `go vet` (which
   compiles it) and `dbtest`'s run actually exercise it.
2. **39 godoc mentions no compiler will ever flag.** A task that stops at "green" leaves them wrong. §9 makes
   the tree-wide staleness sweep an explicit gate.

**A mechanical-rewrite artifact worth encoding as a check, not a list:** `msgin` starts with a consonant and
`endpoint` with a vowel, so every prose comment reading "a msgin.Producer" became ungrammatical. Seven sites,
all fixed; the check belongs in the plan as a grep because any future `msgin.X → endpoint.X` comment rewrite
reintroduces the class (F9.5):

```bash
grep -rn --include='*.go' -E '\b[Aa] (endpoint|routing|transform)\.' .          # must be empty
grep -rn --include='*.go' -E '\[(endpoint|routing|channel|transform|resilience)\.[A-Z]' adapter/   # broken doc-links
```

## 4. Root contract after the move (closed)

Root declares types, interfaces, constants, and **pure combinators** — and **no constructor that starts a
running component**. §9.1 states the mechanical form of this check.

The enumeration below is **closed and generated**: any exported root symbol not in it is a finding.

```
# scope: root module only, at dadc775 (code byte-identical since 1d7fc80)
$ go run docs/plans/027-tools/decls.go . | grep -v '_test\.go' | awk -F'\t' '$5=="exported" && $3!="method" {print $4}' | sort -u | wc -l
     102
```

> **This figure moved from 101 to 102 and the earlier number was NOT stale prose.** `1d7fc80` added
> `ErrNilSubscription` (the `(nil, nil)`-from-`Subscribe` guard in `NewChannelExchange`), and the round-3 pass
> deliberately left every count un-edited so they would move **once**, together with the two decisions that
> were still open. Both are now decided (D-I in §3.2, D-J in §5.1), so the counts move here, in one place.

**The decomposition sums to 102 — stated in one unit so it can be checked:**

| Group | n | Members |
|---|--:|---|
| Vocabulary **types** | 8 | `Message[T]`, `Headers`, `MessageOption`, `Delivery`, `Subscription`, `MessageGroup`, `MessageGroupClaim`, `Hooks` |
| Vocabulary **constants** | 11 | `HeaderMessageID`, `HeaderTimestamp`, `HeaderContentType`, `HeaderCorrelationID`, `HeaderDeliveryCount`, `HeaderSequenceNumber`, `HeaderSequenceSize`; `OverflowBlock`, `OverflowReject`, `OverflowDropOldest`, `OverflowDropNewest` |
| **SPI / interfaces** | 21 | `MessageChannel`, `SubscribableChannel`, `MessageHandler`, `Step`, `HandlerFunc`, `PollingSource`, `EventDrivenSource`, `OutboundAdapter`, `NativeReliability`, `LiveValueSource`, `ScheduledSender`, `RequestReplyExchange`, **`TopicPublisher`**, **`TopicSubscriber`**, `ChannelStore`, `MessageGroupStore`, `PayloadCodec`, `BackoffStrategy`, `RateLimiter`, `CircuitBreaker`, `ProbeGate` |
| Policy & codec **types** | 4 | `RetryPolicy` (+ `Validate`), `OverflowPolicy` (+ `String()`), `JSONPayloadCodec`, `BytesPayloadCodec` |
| Error **sentinels** | 43 | all in `errors.go` — §3.2 |
| **Constructors & combinators** (the allow-list) | 15 | `New`, `NewMessage`, `NewHeaders`, `NewID`, `Chain`, `To`, `PayloadOf`, `WithPayload`, `WithClock`, `WithID`, `WithHeaders`, `Permanent`, `IsPermanent`, `RetryAfter`, `RetryAfterOf` |
| | **102** | |

**The two decided-but-not-yet-implemented changes move it to 102 again, by a different route.** These are
**projections, not measurements** — no code has changed since `1d7fc80`, and **Task 12 re-runs the command
above rather than transcribing this table**:

| Step | Δ interfaces | Δ sentinels | Root exported |
|---|--:|--:|--:|
| Measured at `dadc775` | — | — | **102** |
| **D-I** — `ErrInvalidExpression`, `ErrExprResultType` leave root (§3.2) | — | −2 | 100 |
| **D-J** — `ExclusiveSubscribable` + `ErrSharedReplyChannel` are added (§5.1) | +1 | +1 | **102** |

The coincidence of the endpoints is arithmetic, not design: 43 − 2 + 1 = **42** sentinels and 21 + 1 = **22**
interfaces, so 8 + 11 + 22 + 4 + 42 + 15 = 102. `endpoint.WithSharedReplyChannel` is D-J's third new symbol
and is **not** in this count — it lives in `endpoint`, and this table is root-scoped.

> `TopicPublisher`/`TopicSubscriber` are **new to this list** — decision D-B. They were never named in any of
> the five bundle documents or any RFC, while an earlier §3.1 quietly moved their file whole into `channel`
> (round-2 §A4). They are root SPI, as ADR 0014 and Spec 004 §105 file them.
>
> `HandlerFunc` is a **type** (`type HandlerFunc func(…)`), counted once under SPI. Round-1 §H4's allow-list
> listed it among the "constructors and combinators", which double-counted it; the arithmetic above is the
> corrected one.

The 15 allow-listed constructors and combinators start nothing, hold no state, and own no lifecycle.

`Chain` and `To` stay in root by decision (§H4): `Chain` is a pure combinator, and together with `Step` (the
filter) and `MessageChannel` (the pipe) it is the **Pipes-and-Filters vocabulary** — pushing the assembler into
`endpoint` while the pipe stays in root would split one pattern across two packages.

### 4.1 The exported-surface diff (generated)

**The baseline is a committed file, not a `/tmp` artifact.** It lives at
[`docs/plans/027-root-api-baseline.txt`](../plans/027-root-api-baseline.txt) (written by `apidiff -w` before
Task 1) so a fresh clone or a `/tmp` reap cannot make this gate unrunnable. Re-derived at `dadc775`, after the
round-3 code fixes:

```
# scope: root module only, at dadc775 (code byte-identical since 1d7fc80)
$ apidiff docs/plans/027-root-api-baseline.txt .
Incompatible changes:
- Activate: removed
  … (95 lines, all ": removed")
Compatible changes:
- ErrNilSubscription: added
- EventDrivenSource: added
- IsPermanent: added
- NewID: added
- RetryAfterOf: added
- SubscribableChannel: added

$ apidiff docs/plans/027-root-api-baseline.txt . | grep -c ': removed'
      95
$ apidiff docs/plans/027-root-api-baseline.txt . | grep -c ': added'
       6
```

**Six additions, 95 removals — re-verified at `dadc775`, not carried over.** Deleting root's dead `boxMessage`
and `nilFuncStep` (F12.4) did **not** move these numbers, because both were unexported; that was **checked by
re-running `apidiff`, not assumed**. Removals are expected and are the point: this is a breaking window, the
repository has zero tags, and no consumer exists. **The requirement is to enumerate them, not to minimise
them.**

> **CORRECTED (round 3).** The prose here previously read *"the **93** decompose as"* and *"`WithReleaseStrategy`
> is in the **86**"* — two hand-typed numbers sitting directly above a table that sums to **95 / 87**. The
> generated table was right both times; only the sentences framing it were wrong, and ADR 0027's Consequences
> repeated the 93. This is the same defect the F11.2 `CORRECTION` block was written to close: *any number
> reached by reading output instead of piping it is a hand-typed number.*

The **95** decompose as follows; the decomposition is a **verified partition**, checked by set arithmetic
against the symbol→destination map (`docs/plans/027-tools/symmap.tsv`), not by reading the list:

| Class | n | What |
|---|--:|---|
| Relocated into a subpackage | 87 | `NewConsumer`, `NewProducer`, `Filter`, `Split`, `Transform`, `NewRouter`, `NewAggregator`, `NewDirectChannel`, `NewQueueChannel`, `NewPublishSubscribeChannel`, `NewPubSub`, `ExponentialBackoff`, `NewCircuitBreaker`, `NewTokenBucket`, every `With*` option, every `*Option` type, `Consumer`/`Producer`/`Gateway`/`ChannelExchange`/`Router`/`Aggregator`/`Handler`/`OutboundGateway`, `FanOutPolicy` + its two consts, `PubSub`, `Activate`, `Consume` |
| Deleted outright (`*Expr`) | 6 | `FilterExpr`, `RouterExpr`, `TransformExpr`, `SplitExpr`, `WithCorrelationExpr`, `WithReleaseExpr` |
| Renamed | 1 | `StreamingSource` (its replacement `EventDrivenSource` is on the additions side) |
| The segregation itself | 1 | `MessageChannel.Subscribe` |
| | **95** | |

```
$ apidiff docs/plans/027-root-api-baseline.txt . \
    | awk '/^Incompatible/{f=1;next}/^Compatible/{f=0}f' | sed 's/^- //;s/: removed$//' > removed.txt
$ wc -l < removed.txt                                                             # 95
$ comm -12 <(sort removed.txt) <(cut -f2 docs/plans/027-tools/symmap.tsv | sort -u) | wc -l    # 87 relocated
$ grep -cE '^(FilterExpr|RouterExpr|TransformExpr|SplitExpr|WithCorrelationExpr|WithReleaseExpr)$' removed.txt  # 6
$ grep -c '^StreamingSource$' removed.txt ; grep -c '^MessageChannel\.Subscribe$' removed.txt   # 1  1
$ comm -23 <(sort removed.txt) <(cut -f2 docs/plans/027-tools/symmap.tsv | sort -u) \
    | grep -vE '^(FilterExpr|RouterExpr|TransformExpr|SplitExpr|WithCorrelationExpr|WithReleaseExpr|StreamingSource|MessageChannel\.Subscribe)$'
(no output — nothing unaccounted for)
```

87 + 6 + 1 + 1 = 95, with an empty residual set.

> **NO LONGER CONTINGENT — both open decisions are closed (2026-07-28), and the diff moves.** The block that
> stood here made these numbers conditional on the fate of `ErrInvalidExpression` and `ErrExprResultType`.
> **D-I (§3.2) removes both from root**, and **D-J (§5.1) adds `ExclusiveSubscribable` and
> `ErrSharedReplyChannel`**, so the projected diff once Tasks 9.5/9.6/10 land is:
>
> | | removals | additions |
> |---|--:|--:|
> | Measured at `dadc775` | **95** | **6** |
> | D-I: two sentinels leave root | +2 → 97 | — |
> | D-J: two symbols enter root | — | +2 → 8 |
> | **Projected at Task 12** | **97** | **8** |
>
> **These are projections and are labelled as such.** No code has changed since `3d0b87a`. Task 12 **re-runs
> `apidiff` and takes its output as the truth**; if it disagrees with this table, the table is wrong. The
> 87 + 6 + 1 + 1 partition above is unaffected — D-I adds two *new* removals (`ErrInvalidExpression`,
> `ErrExprResultType`) as a fifth class, and D-J adds only to the additions side.

> **`WithReleaseStrategy: removed` is in the 87, and that is a signature change, not a relocation.** Decision
> **D-E** retyped it from `func(MessageGroup) bool` to the named, fallible `ReleaseStrategy`; `apidiff`
> reports a changed func parameter as a removal. It also relocates to `routing`. Both facts are true at once
> and the migration guide must say both.

The six additions:

```go
// errors.go — the (nil, nil)-from-Subscribe guard in NewChannelExchange (added
// in 1d7fc80; §5.1). The exchange owns the Subscription until Close, so an
// SPI implementation that breaks Subscribe's contract is rejected where the
// offending input is still named, not by a nil-deref inside Close.
var ErrNilSubscription = errors.New("msgin: channel returned a nil subscription")

// reliability.go — forced: both inspect unexported wrapper types, so no
// other package can reimplement them.
func IsPermanent(err error) bool                       // was isPermanent
func RetryAfterOf(err error) (time.Duration, bool)     // was retryAfterOf

// message.go — chosen (settled with the user 2026-07-27): one id scheme for
// message ids and correlation ids, reusable by a future external exchange adapter.
func NewID() string                                    // was randomID

// channel.go — the subscribing half of the segregated contract (§5, ADR 0028 §1).
type SubscribableChannel interface { … }

// spi.go — the EIP rename (§6, ADR 0029 §1).
type EventDrivenSource interface { … }                 // was StreamingSource
```

`channel.WithSingleSubscriber` (decision D-F) does **not** appear in this diff because `apidiff` compares the
**root package**, and the option lives in `channel`. It is new exported surface all the same, off by default,
reusing the existing `msgin.ErrChannelSubscribed` sentinel so a caller's existing `errors.Is` handling covers
it unchanged.

**Two godoc corrections these exports force** (round-2 §D16, §D17), both normative:

- **`IsPermanent` is a policy classifier, not a marker inspector.** It returns true for `ErrPayloadType`,
  `ErrPayloadDecode`, and `ErrPayloadTooLarge`, which never passed through `Permanent`. Calling it *"the
  natural public twin of `Permanent`"* understates what exporting freezes into the contract; the godoc must
  enumerate the classifier's policy, not just name the twin.
- **`RetryAfterOf`'s nil case becomes public.** Its rationale for skipping a nil guard (*"the only caller
  never passes nil"*) is void on export. The nil case is now public surface and needs a test.

## 5. Channel interface segregation (normative)

```go
// MessageChannel is a send-only conduit — the EIP Pipe. Every endpoint that
// merely forwards a message (a discard target, a default route, a router
// destination, an exchange request channel, an HTTP inbound target) takes this,
// so any channel implementation qualifies.
type MessageChannel interface {
    Send(ctx context.Context, msg Message[any]) error
}

// SubscribableChannel additionally accepts handlers. Spring calls this
// SubscribableChannel; the Subscription return is msgin's addition, giving the
// caller an unsubscribe handle.
type SubscribableChannel interface {
    MessageChannel
    Subscribe(h MessageHandler) (Subscription, error)
}
```

Satisfaction after the change — the point of the exercise:

| Type | `MessageChannel` | `SubscribableChannel` | Before |
|---|---|---|---|
| `channel.DirectChannel` | ✅ | ✅ | satisfied `MessageChannel` only |
| `channel.PublishSubscribeChannel` | ✅ | ✅ | satisfied **neither** |
| `channel.QueueChannel` | ✅ | — (is a `PollingSource`) | satisfied **neither** |
| every `OutboundAdapter` (e.g. `*memory.Broker`) | ✅ | — | satisfied **neither** |

**`DirectChannel.Subscribe` changes signature** from `error` to `(Subscription, error)` so both subscribable
channels satisfy one contract. This is the breaking half of the change and is deliberate: without it,
`ChannelExchange`'s reply channel can only ever be a `DirectChannel`.

**`PollableChannel` is deliberately not defined.** It would duplicate the existing `PollingSource` SPI's exact
method set (`Poll(ctx, max) ([]Delivery, error)`, already implemented by `QueueChannel`), and no signature in the
library would take one. It can be added later non-breakingly. Documented divergence from Spring's three-way
split — see ADR 0028 §3.

### 5.0 The call-site census — state the SCOPE RULE, never a bare count

Three documents have now stated this count and **all three were wrong**: round 1 said *four of five*, ADR 0028
repeated it, round 2 "corrected" it to *six of seven*. Both searched only the pattern core and stopped at the
module boundary. The measured answer is **nine**.

> **Normative scope rule:** the census covers **every non-test `MessageChannel` occurrence in the workspace,
> `adapter/` included**. Re-derive it; do not cite a number from a document.

```bash
grep -rn "msgin\.MessageChannel\|MessageChannel interface" --include="*.go" . \
  | grep -v "_test.go" | grep -v "^./docs" | grep -v '// '        # → 16 lines: 1 declaration + 15 uses
```

The nine public positions this yields — **eight send-only, one subscribing**:

| # | Position | File |
|---|---|---|
| 1 | `routing.WithDiscardChannel` | `routing/filter.go:18` |
| 2 | `routing.WithDefaultChannel` | `routing/router.go:18` |
| 3 | `routing.NewRouter`'s `pick` return | `routing/router.go:29,37` |
| 4 | `routing.WithOutputChannel` | `routing/aggregator.go:55` |
| 5 | `routing.WithExpiredGroupChannel` | `routing/aggregator.go:133` |
| 6 | `endpoint.NewChannelExchange`'s `request` | `endpoint/exchange.go:225` |
| 7 | **`msghttp.ServeAsync`'s `target`** | `adapter/http/inbound.go:116` |
| 8 | **`stdlib.NewInbound`'s `target`** | `adapter/http/stdlib/inbound.go:33` |
| — | `endpoint.NewChannelExchange`'s `reply` — the only subscriber, now `SubscribableChannel` | `endpoint/exchange.go:225` |

**Rows 7–8 are a user-visible capability widening that no artifact previously recorded.** The HTTP inbound
handler's `target` now accepts a `QueueChannel`, a `PublishSubscribeChannel`, and any `OutboundAdapter` — so
**an HTTP request can be parked in a durable queue channel instead of requiring a synchronous subscriber**.
That is desirable, it is exactly what §10 argues the narrowing enables for a multi-instance deployment, and it
must appear in `MIGRATION.md` and in the two functions' godoc.

Rows 4–5 matter for the same reason round-2 §A6 flagged them: a durable `QueueChannel` as an Aggregator
**output** or **expired-group** sink is a real multi-instance capability, and §9.4's test must cover those
positions, not just the discard/default/request three.

**Row 3 is the one every enumeration has dropped, including the list of what the other enumerations dropped.**
`routing.NewRouter`'s `pick` **return** (`routing/router.go:29,37`) is the only position of the eight where the
destination is chosen at **message time** by *caller-supplied* code. It is therefore the sharpest case of the
widening: a user's `pick` can now return a durable `QueueChannel` per message, which was a compile error
before. §9.4's test must cover it.

### 5.1 Exchange reply-channel exclusivity

`ChannelExchange`'s dedicated-reply-channel guarantee was enforced **only** by `DirectChannel` returning
`ErrChannelSubscribed` on a second subscriber. `PublishSubscribeChannel.Subscribe` had no such guard. Widening
`reply` to `SubscribableChannel` therefore makes **two exchanges sharing one pub-sub reply channel a valid
program**: every reply fans out to both receivers, and the non-owner hands a full copy of the other exchange's
reply to its `WithUnmatchedReplySink` — typically a dead-letter or audit sink.

**Required, and normative:**

1. `NewChannelExchange` **stores the `Subscription`** returned by `reply.Subscribe(...)` and **`Cancel()`s it
   in `Close()`**. Without this the widening introduces a subscription leak that did not previously exist.
   `Close`'s godoc previously said *"The reply receiver remains subscribed (channels have no unsubscribe)"*,
   which is false once `Subscribe` returns a handle.
2. Exclusivity is **probed at construction and rejected by default** — decision **D-J**
   ([ADR 0030](../adrs/0030-reply-channel-exclusivity-probe.md), 2026-07-28), which **amends ADR 0028 §6.2's
   default posture**. `channel.WithSingleSubscriber()` (decision D-F) is unchanged and becomes the mechanism a
   `PublishSubscribeChannel` uses to pass the probe. A cross-exchange *registry* stays rejected.

   Root gains an **optional capability**, in the established sense of `NativeReliability` / `ScheduledSender`
   (ADR 0002) — the core asserts for it and behaves correctly when the assertion fails:

   ```go
   type ExclusiveSubscribable interface {
   	SubscribableChannel
   	SingleSubscriber() bool   // reports THIS channel, in THIS process
   }
   ```

   `NewChannelExchange` rejects **before subscribing**, so a rejected exchange leaves no subscription behind:

   ```go
   if ex, ok := reply.(msgin.ExclusiveSubscribable); ok && !ex.SingleSubscriber() && !cfg.allowShared {
   	return nil, msgin.ErrSharedReplyChannel
   }
   ```

   **A channel that does not implement the probe is accepted** — the `ok &&` keeps the SPI open to
   third-party reply channels. Both in-tree implementations do implement it (`DirectChannel` → always `true`;
   `PublishSubscribeChannel` → `cfg.single`), and those two are **every** `SubscribableChannel` in the
   workspace, so the unknown arm is reachable only from outside the repo. `endpoint.WithSharedReplyChannel()`
   is the opt-out for a deliberate fan-out.

   **Normative branch set** (four arms, a truth table — each needs a case): probe absent → accept; probe
   `true` → accept; probe `false` → `ErrSharedReplyChannel`; probe `false` + opt-out → accept.
3. The **two-exchanges-over-one-`PublishSubscribeChannel`** case is a test asserting the documented fan-out
   behavior, so the trade-off is pinned rather than discovered later. **Under D-J that test must pass
   `endpoint.WithSharedReplyChannel()` on BOTH of its constructions** —
   `TestChannelExchange_sharedPubSubReplyChannel` (`endpoint/exchange_test.go:413`) builds `exA` at `:446`
   under `require.NoError` **and** `exB` at `:453` over the same plain `NewPublishSubscribeChannel()`, and
   D-J turns both into `ErrSharedReplyChannel`. Apply the option unconditionally: in the
   `WithSingleSubscriber` case the channel passes the probe and the option is inert. The test keeps asserting
   the fan-out; it just has to ask for it. *(Round-4 M11: this said "its first exchange", understating the
   edit by one construction.)*

### 5.2 `DirectChannel`'s `Subscription` semantics (decided)

| Question | Decision |
|---|---|
| After `Cancel()`, does a second `Subscribe` succeed? | **Yes.** `Cancel` releases the slot; the channel returns to its unsubscribed state. |
| `Send` between `Cancel` and the next `Subscribe`? | **`ErrNoSubscriber`**, exactly as before any subscriber existed. |
| `Cancel` racing an in-flight `Send`? | The in-flight `Handle` **runs to completion**; `Cancel` only prevents *subsequent* dispatch. Matches `PublishSubscribeChannel`. |
| `Cancel` called twice? | **Idempotent, never panics.** |
| `Subscribe(nil)` — what `Subscription`? | **`(nil, ErrNilHandler)`.** The error path returns no handle. |
| **A stale handle's `Cancel` after a resubscribe?** | **It must NOT evict the current subscriber.** The slot is released on **identity** (`if c.sub == s`), the same defence `PublishSubscribeChannel.remove` and `replyCorrelator.deregister` already use. |

> The sixth row is **new**, and it is not a nicety: with a naive `sync.Once` + "clear the handler", the
> sequence `Cancel` → `Subscribe` → old-handle `Cancel` silently evicts the *new* subscriber and breaks a live
> flow. Forced by the design, unanswered by the earlier five-row table (F10.5.1).

### 5.2a `Close` changes an observable behavior — a decided consequence, not a footnote

`ChannelExchange.Close` **already existed** before this window (`exchange.go:356`). §5.1's `replySub.Cancel()`
does not *add* `Close`; it **changes what `Close` does**:

- **Before:** the reply receiver stayed subscribed forever, so a reply arriving after `Close` was absorbed by
  the receiver and routed to `WithUnmatchedReplySink`.
- **After:** the receiver is unsubscribed, so a post-`Close` reply is the **channel's** problem — a
  `DirectChannel` returns `ErrNoSubscriber` **to the reply sender**, and a `PublishSubscribeChannel` delivers
  it to whoever else is subscribed. It never reaches `WithUnmatchedReplySink`.

This is not a leak fix with no downside: it **moves a failure from a silent sink to the sender's error
return**. That is the better behavior, but it is a behavior change inside an increment declared
behavior-preserving, so it is listed in §2.1, stated on `Close`'s godoc, recorded in `MIGRATION.md`, and
pinned by `TestChannelExchange_closeCancelsReplySubscription`.

### 5.3 `MessageChannel` and `OutboundAdapter` are kept distinct (audit B4 / decision §H3)

The narrowed `MessageChannel` is **method-identical** to the existing `OutboundAdapter` (`spi.go:56`) — the
exact duplication §3 of ADR 0028 refuses to create for `PollableChannel`. **Both are kept**, and the identity is
documented as deliberate in **both** godocs.

**Governing rationale — consistency with Pipes and Filters.** EIP ch.3's foundational pattern is *filters*
(processing steps) connected by *pipes* (channels). **`MessageChannel` IS the Pipe** — a first-class concept in
the pattern this library's composition model is built on. `OutboundAdapter` is a **Channel Adapter** at the
system boundary (EIP ch.4). They are **two different patterns that happen to share a method signature**, not two
names for one thing; collapsing or aliasing them would erase the Pipe from the type system of a
pipes-and-filters library. Spring draws the same line independently (its outbound adapters are
`MessageHandler`s, distinct from `MessageChannel`), and Go's structural typing already makes the two
interchangeable at every call site — so keeping both names costs nothing in flexibility.

A type alias (`type MessageChannel = OutboundAdapter`) was **declined**: it would prevent the two roles ever
diverging, a constraint the design does not want to commit to.

ADR 0028 §3's "new surface must earn its keep" rule **does not apply**: it governs *adding* an interface with no
consumer, whereas `MessageChannel` is existing surface with call sites that narrowed into coincidence.

**Consequence, which §9.4 must cover:** every shipped `OutboundAdapter` now also satisfies `MessageChannel`, so
it becomes a legal discard target, default route, router destination, exchange request channel, **and HTTP
inbound target**. This is a larger capability widening than the four-row table above advertises, and it
**voids ADR 0013's F2 rationale** (amended in place there).

**Corroborating evidence from the migration — restated, because the earlier version of this paragraph was
factually wrong.** It claimed *"five test fakes were **deleted, not migrated**"*. **All five survive.** What
was deleted is their vestigial no-op `Subscribe` **stubs** — and that is *stronger* evidence for the
segregation, not weaker, because it shows each fake wanted `Send` and carried `Subscribe` only as interface
tax:

```
$ git grep -n 'func (.*) Subscribe(.*msgin.MessageHandler) error' ab233d9 -- '*_test.go'
ab233d9:aggregator_settlement_test.go:35:  func (c *idsAggChannel) Subscribe(msgin.MessageHandler) error { return nil }
ab233d9:aggregator_test.go:37:            func (c *fakeAggChannel) Subscribe(msgin.MessageHandler) error { return nil }
ab233d9:aggregator_test.go:208:           func (c *failNthChannel) Subscribe(msgin.MessageHandler) error { return nil }
ab233d9:exchange_test.go:721:             func (c *scriptedChannel) Subscribe(_ msgin.MessageHandler) error { return nil }
ab233d9:expr_test.go:35:                  func (c *collector) Subscribe(msgin.MessageHandler) error { return nil }

$ grep -rn --include='*_test.go' 'Subscribe(.*MessageHandler) error' .
(no output, exit 1)                      # all five stubs gone

$ # and all five FAKES are still here, moved with their tests:
routing/aggregator_test.go:22             type fakeAggChannel struct {
routing/aggregator_test.go:157            type failNthChannel struct {
routing/aggregator_settlement_test.go:24  type idsAggChannel struct {
endpoint/gateway_test.go:19               type collector struct{ got []msgin.Message[any] }
endpoint/exchange_test.go:811             type scriptedChannel struct {
```

**Five no-op `Subscribe` stubs deleted; five fakes migrated.** The conclusion is unchanged and now properly
evidenced: **five of the six `MessageChannel` implementations in the test suite never wanted `Subscribe` at
all** (F10.6, corrected in F13).

> **This also reconciles a contradiction inside this document.** §3.4c records `collector` as **re-declared**
> in `gateway_test.go` (it is — `endpoint/gateway_test.go:19`), while this section called it *deleted*. §3.4c
> was right; §5.3 was wrong. The two no longer disagree.

## 6. Renames and behavior types

**Breaking rename.** `StreamingSource` → **`EventDrivenSource`**: the canonical EIP term is *Event-Driven
Consumer* (Spring: `EventDrivenConsumer`), and "streaming" collides with unrelated streaming-data vocabulary.

**Scope, measured — and the `.go`-only figure is not the whole job:**

```
$ grep -rn 'StreamingSource' . --exclude-dir=.git --exclude-dir=docs
(no output, exit 1)
# REGENERATED at dadc775 (round-4 fix pass)
$ grep -rn 'EventDrivenSource' --include='*.go' . | wc -l ; grep -rl 'EventDrivenSource' --include='*.go' . | wc -l
      31      13
$ grep -rn 'EventDrivenSource' . --exclude-dir=.git --exclude-dir=docs | wc -l
      36
```

**31 occurrences across 13 `.go` files, all in the root module** — ADR 0029 §1's sizing is right, and
audit F7's correction of the earlier "seven-module" claim is confirmed. **But five more live in `CLAUDE.md`
(2) and `MESSAGING.md` (3)**, for **36 across 15 files**.

> **ROUND-4 CORRECTION (B9).** This census read **30 / 12 / 35 / 14** and was bolded as *"exactly right"*. The
> thirteenth `.go` file is **`endpoint/doc.go`**, added by `1d7fc80` (F12.5) — the same commit behind B1, B2,
> B6 and B8. Re-derived at `dadc775`: **31 across 13 `.go` files; 36 across 15 files overall.** ADR 0029 §1's
> sizing conclusion is unaffected; only the arithmetic moved.

**`MESSAGING.md` is named nowhere in the bundle** and carries three of the five mentions (F10.4). Two
consequences:

- The rename touches a **user-visible string**: `errors.go:22`'s `ErrUnsupportedSource` message reads
  *"msgin: source implements neither PollingSource nor EventDrivenSource"* (round-2 §D14). It belongs in
  `MIGRATION.md`.
- The verification gate is `--include='*.go'` **plus the two root narratives**, and **must exclude `docs/`**:
  129 `.md` hits across 29 files live there, including shipped ADRs 0002/0006/0008/0009/0010/0017/0018/0023
  and shipped specs, which CLAUDE.md forbids rewriting (round-2 §C4, F11.10).

**The `Stream` method keeps its name** on the renamed interface. It describes the mechanism accurately, Spring
offers no counterpart name to align with, and renaming it would churn every adapter for no gain.

**`Exchange` is kept, qualified — citation VERIFIED.** Root keeps `RequestReplyExchange`; the implementation is
`endpoint.ChannelExchange`.

> **The AMQP-disclaimer line DOES NOT EXIST YET.** This sentence, and ADR 0029 §2, are written in the present
> tense as though it were already shipped. It is not:
>
> ```
> $ grep -rn -i 'amqp' --include='*.go' .
> (no output, exit 1)
> ```
>
> It is an **outstanding obligation of §8**, owned by Plan 027 Task 11, not a description of the tree.
`org.springframework.integration.gateway.RequestReplyExchanger` exists and is Spring Integration's default
gateway service interface; Spring 6.5 adds `AsyncRequestReplyExchanger`. We keep our
`RequestReplyExchange`/`Exchange` form rather than Spring's `-er` agent noun — see RFC-0002 §7.1.

**Behavior types.** Each endpoint's behavioral closure gets a named func type, with the package carrying the
qualifier rather than the type repeating it:

```go
// package routing
type Predicate[A any]    func(ctx context.Context, m msgin.Message[A]) (bool, error)
type RouteFunc           func(ctx context.Context, m msgin.Message[any]) (msgin.MessageChannel, error)
type SplitFunc[A, B any] func(ctx context.Context, m msgin.Message[A]) ([]msgin.Message[B], error)
type CorrelationStrategy func(m msgin.Message[any]) (string, error)      // SHIPPED — routing/aggregator.go:25
type ReleaseStrategy     func(g msgin.MessageGroup) (bool, error)        // SHIPPED — routing/aggregator.go:35

// package transform
type Transformer[A, B any] func(ctx context.Context, m msgin.Message[A]) (msgin.Message[B], error)
```

Two of the six are shipped; the other four and the combinators are Plan 027 Task 9.

**`ReleaseStrategy` is fallible, and `WithReleaseStrategy` takes it — decision D-E.** The bool-only form in
earlier drafts was the *sugar's* shape mistaken for the contract's. The shipped API:

```go
func WithReleaseStrategy(fn ReleaseStrategy) AggregatorOption            // routing/aggregator.go:82 — fallible
func WithReleaseWhen(fn func(msgin.MessageGroup) bool) AggregatorOption  // routing/aggregator.go:89 — sugar, wraps to (bool, nil)
```

This **renames** the old `WithReleaseStrategy` and **removes** the earlier draft's proposed `WithRelease`.
Both are breaking, which is free, and both are in `MIGRATION.md` and §4.1's `apidiff` expectations. It is
symmetric with `WithCorrelationStrategy`, which is already typed with its named type — round-2 §D1's point
exactly: `agg.WithReleaseStrategy(myReleaseStrategy)` did not compile under the earlier draft.

**D-E is a Task 1 prerequisite, not Task 9 work.** This is a *task-ordering* fact and it is load-bearing
(F3). `aggregator_test.go` held 10 references to the `*Expr` API. They split three ways, and the earlier plan
treated them as one undifferentiated group:

| Case | Branch | Verdict |
|---|---|---|
| M-1 empty group snapshot | `toGroupEnv` guard (in `expr.go`) | genuinely leaves with expr → Task 10 |
| M-6 non-`A` member → `ErrPayloadType` | `toGroupEnv` guard (in `expr.go`) | genuinely leaves with expr → Task 10 |
| **H-1 reaper fall-through** | `reapGroup`, **core** | **must survive** |
| **H-2 drain-loop residual release-check error** | `release`, **core** | **must survive** |
| **H-3 drain-loop residual `releaseOnce` failure** | `release`, **core** | **must survive** |

H-1/H-2/H-3 are reachable **only** when the release check can return an error. Before this window the sole
fallible release strategy was `WithReleaseExpr`. **So deleting the `*Expr` constructors removes the only
driver for three core hot-path branches, and deferring D-E to Task 9 loses them silently.** Resolution taken:
D-E was pulled forward into Task 1 and H-1/H-2/H-3 rewritten over a Go-func
`requireQtyRelease(min) msgin.ReleaseStrategy` helper. Coverage preserved.

**`cfg.optErr` is deleted — decision D-D.** Confirmed by the compiler: after `expr.go`'s deletion,
`grep -n optErr aggregator.go` returned only the two *read* sites; `expr.go` held **every** writer (F5). The
field and its `NewAggregator` guard were unreachable, not merely untested, so ADR 0029 §3's claim that the
fallible type *"fixes all three"* was false — it rescues only the `Handle` release-decision branch, and the
`NewAggregator` branch is deleted rather than rescued.

**Two fixtures died with M-6 and nothing reported it.** `mixedTypeAddStore` and `mixedTypeGroup` existed only
to drive it; after the deletion nothing referenced them, and `unused` is off, so they were removed by hand.
`emptyGroupAddStore` **is** still live (`aggregator_settlement_test.go:246`) — the two are not symmetric
despite looking it (F4).

Combinators are methods (`Predicate.And`/`Or`/`Not`) — the payoff that distinguishes naming these types from
leaving them anonymous. Each godoc names its Spring equivalent so a Spring-trained reader still finds it.

**Combinator nil semantics are normative, because the naive implementation panics.** `p.And(nil)`,
`nil.Or(q)`, and `nil.Not()` are all reachable from ordinary caller code (`var p routing.Predicate[T]` is
nil, and calling a method on a nil func value is legal Go). CLAUDE.md forbids panicking on caller input, and
this package already has a settled answer for exactly this shape — `routing/filter.go:29` returns
`nilFuncStep()` for a nil `pred`, degrading to `msgin.ErrNilFunc` at dispatch. The combinators follow it: a
nil receiver or a nil argument yields a `Predicate[A]` returning `(false, msgin.ErrNilFunc)` **at evaluation**
(the combinators are pure and return a `Predicate`, not `(Predicate, error)`, so there is nowhere else to put
it), reusing the **existing** sentinel rather than minting one. **The nil check precedes the short-circuit**:
`p.Or(nil)` must surface `ErrNilFunc` even when `p` evaluates true. Plan 027 Task 9 enumerates the branches.

A bare closure remains assignable, so **call sites are source-compatible**. `apidiff` will nonetheless report the
parameter-type change on each typed constructor; that is expected and benign, and the plan records it as a
*reviewed, source-compatible* entry rather than claiming zero output.

## 7. Expression support moves out of the core

The six `*Expr` constructors are **removed from the core outright** — no deprecated shims, since nothing is
tagged and there is no consumer a shim would protect. They are reborn in a separate **`expr` module** whose
providers return the §6 types:

```go
func Predicate[A any](s string) (routing.Predicate[A], error)   // compiles once; fails at construction
func Release[A any](s string)   (routing.ReleaseStrategy, error) // (bool, error) — carries eval failures
```

**The provider shape is NOT uniform, and it is NOT non-generic.** Both errors were in earlier drafts:

1. **Not uniform** (round-2 §D2). `RouterExpr` took `routes map[string]MessageChannel` in addition to the
   expression, and had **two** construction validations of its own.
2. **Not non-generic** (round 3, compile-proven). Every deleted original was `[A any]` —
   `WithCorrelationExpr[A any]`, `WithReleaseExpr[A any]`, `RouterExpr[A any]` — and `A` is load-bearing
   twice: `compile[A]` (`expr.go:35`) type-checks `payload.Field` against `A`, which is what makes ADR 0019's
   fail-at-construction contract real; and `PayloadOf[A]` (`expr.go:129,224,284,331`) **is** the M-6
   `ErrPayloadType` branch Task 10 mandates. A non-generic `Correlation`/`Release`/`RouteFunc` would not
   compile and could not carry that branch.

The provider set, stated honestly:

```go
func Predicate[A any](s string)      (routing.Predicate[A], error)
func SplitFunc[A, B any](s string)   (routing.SplitFunc[A, B], error)
func Transformer[A, B any](s string) (transform.Transformer[A, B], error)
func Correlation[A any](s string)    (routing.CorrelationStrategy, error)
func Release[A any](s string)        (routing.ReleaseStrategy, error)
func RouteFunc[A any](s string, routes map[string]msgin.MessageChannel) (routing.RouteFunc, error)
```

**`A` is not inferable from a `string`**, so every call instantiates explicitly — `expr.Release[Order]("…")`.
Each provider's godoc must say so. Task 10's branch list must carry `RouteFunc`'s two extra construction
validations.

**The module owns its own error sentinels — decision D-I (§3.2), decided 2026-07-28.**

```go
// expr/errors.go
var (
	// ErrInvalidExpression is the construction-time fault: the expression is
	// empty, unparseable, or fails type-checking against A. The wrapped error
	// carries the offending source text.
	ErrInvalidExpression = errors.New("msgin/expr: invalid expression")
	// ErrExprResultType is the evaluation-time fault: a compiled expression
	// evaluated to a value that is not the asserted output type.
	ErrExprResultType = errors.New("msgin/expr: expression result type mismatch")
)
```

Root's `msgin.ErrInvalidExpression` and `msgin.ErrExprResultType` are **deleted**, not aliased. An alias would
keep the dead names in root's closed contract while pretending they had moved, and `errors.Is` against an
alias of a *different* `errors.New` value does not match — the alias would have to be `= msgin.ErrX`, which is
exactly the dependency D-I removes.

Callers migrate from `errors.Is(err, msgin.ErrInvalidExpression)` to `errors.Is(err, expr.ErrInvalidExpression)`.
They already import `expr` to build the endpoint, so no new import is introduced. `MIGRATION.md` (Task 12)
carries both lines.

**The message prefix is `msgin/expr:`, and the tree does NOT have one convention to appeal to.** Measured
rather than assumed:

```
# scope: whole workspace, at dadc775
$ grep -hoE 'errors\.New\("[^"]*"' adapter/*/errors.go adapter/*/*/errors.go adapter/cron/sqlutil.go \
    | sed 's/errors.New("//' | cut -d: -f1 | sort | uniq -c
  26 msghttp
  10 msgin/cron
  15 msgin/sql
```

Each prefix tracks its own **package name** (`cron`, `sql`, `msghttp`), not a single house style, and the
`msgin/`-qualified form is the majority of *packages* (2 of 3) while `msghttp` is the majority of *sentinels*
(26 of 51). `msgin/expr:` is chosen because the package name is `expr`, which is too generic to identify the
library in a bare error string. Recorded here so a reviewer does not read a non-existent convention out of the
three-way split.

The compile error lives at the provider call, so the base constructors stay non-fallible and inline-composable
and the "invalid expression fails at construction" contract of ADR 0019 is preserved. Runtime failures wrap the
**source expression text**, which is the debuggability mitigation ADR 0029 §3 traded the interface shape for.

**A separate module is required, not a subpackage** — a subpackage of the root module would leave `expr-lang` in
the root `go.mod` and deliver none of the benefit. The rule this follows, stated so it is not arbitrary against
RFC-0004's opposite conclusion for `robfig`: *a zero-transitive dependency is pushed to its own module when its
weight is material to consumers who do not use it* — `expr-lang` at 7.1 MB is; `robfig/cron` at 144 KB is not.

**The new module needs a `replace`, and CI needs it too** (round-2 §C2). `git tag | wc -l` → **0**, and every
satellite module carries `require github.com/kartaladev/msgin v0.0.0` + `replace … => ../..`. Without the same
pair, `expr` cannot resolve the root module under `GOWORK=off` — which is exactly how CI's `module` job runs.
The `go.mod` `expr` ships with must contain both; a `use` line in `go.work` is necessary but not sufficient.

**Sequencing consequence (load-bearing).** All six `*Expr` constructors returned `Step`, `*Router`, or
`AggregatorOption` — types that move to `routing`/`transform`. So `expr.go` could not remain in root once those
moved, and splitting it across two new packages only to delete it afterwards is throwaway work. **The `*Expr`
deletion is therefore sequenced first**, before any package extraction. Between that task and the `expr`
module's arrival, expression support is absent from the branch — acceptable within a branch that restores it
before merge.

**Dropping `expr-lang` touched every module at once, and Task 1 only did the root one.** All six satellite
`go.mod`s carried `github.com/expr-lang/expr // indirect` under a `replace` to the local root, and CI runs
`go mod tidy` + `git diff --exit-code` **per module**. F6's *"`expr-lang` drops cleanly"* was a **root-only
measurement stated workspace-wide**, and it stayed false for the other six modules for the whole window —
5 of 6 CI matrix jobs would have been red. **The satellites were re-tidied in this pass (F12.1).** The claim
now carries its module scope, per Plan 027 Global Constraint 0:

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

`go.sum` is checked explicitly, not just `go.mod` — 12 `go.sum` lines went with the 6 `go.mod` lines (F12.1).
`clockwork` and `robfig/cron/v3` remain; the root module's direct-dependency count goes **3 → 2**.

## 8. Godoc alignment (normative)

The non-breaking documentation fixes RFC-0002 accepted, listed here because they are acceptance criteria, not
optional polish.

> **These nine bullets had NO OWNING TASK, and five were unmet.** They were written in the indicative ("carries
> the line", "state the widened contract") as though they described the tree, so nothing in Plan 027 was ever
> going to produce them. **Plan 027 Task 11 now owns all nine**, each with a grep-verifiable checkbox. The
> status column below was measured at `dadc775`, after the five subpackage `doc.go` files landed (F12.5).

| # | Obligation | Status at `dadc775` | Evidence |
|---|---|---|---|
| 1 | Name the in-process request-reply pattern **Correlation Identifier**, with **Return Address** as the distributed seam (§10) | ⚠️ **HALF** — "Return Address" is present (`channel.go:33`, `endpoint/doc.go:19`); **"Correlation Identifier" appears in no `.go` file** | `grep -rn -i 'correlation identifier' --include='*.go' .` → exit 1 |
| 2 | `DirectChannel`'s deliberate single-subscriber restriction vs Spring's load-balanced multi-subscriber; competing consumers come from the worker pool | ✅ **MET** | `channel/direct.go:15,17` |
| 3 | `RequestReplyExchange` carries the line disclaiming AMQP's broker-side-routing-table meaning | ❌ **UNMET** | `grep -rn -i 'amqp' --include='*.go' .` → exit 1, workspace-wide |
| 4 | Every named behavior type in §6 names its Spring equivalent — verified **per type**, not sampled | ❌ **UNMET for the two shipped types.** `routing.CorrelationStrategy` and `routing.ReleaseStrategy` name no Spring counterpart; only `routing/doc.go` and `transform/doc.go` do, at package level | `grep -n -B8 'type CorrelationStrategy' routing/aggregator.go \| grep -i spring` → exit 1 (same for `ReleaseStrategy`) |
| 5 | `MessageChannel` and `OutboundAdapter` each state they are method-identical **by design** (§5.3) | ✅ **MET** | `channel.go:18-19`, `spi.go:48-51` |
| 6 | Root's `doc.go` states the Pipes-and-Filters model in §3.5's terms | ✅ **MET** | `doc.go:5-14` — Pipe / filter / `Chain` all named |
| 7 | `msghttp.ServeAsync` and `stdlib.NewInbound` state the **widened `target` contract** (§5.0 rows 7–8) | ❌ **UNMET.** Both godocs discuss nil-ness and method filtering; neither says any `MessageChannel` — including a durable `QueueChannel`, a `PublishSubscribeChannel`, or any `OutboundAdapter` — now qualifies | `adapter/http/inbound.go:110-116`, `adapter/http/stdlib/inbound.go:32-33` |
| 8 | `ChannelExchange.Close` states the post-`Close` reply behavior change (§5.2a) | ✅ **MET** | `endpoint/exchange.go:363-369` (`BEHAVIOR AFTER CLOSE`) |
| 9 | `IsPermanent` documents its **classifier policy**, not merely that it twins `Permanent` (§4.1) | ✅ **MET** | `reliability.go:33-37` — enumerates the three sentinels and the `ErrHandlerPanic` exclusion |
| **10** | **`SubscribableChannel`'s godoc cross-references `ExclusiveSubscribable`** (D-J) | ⛔ **N/A until Task 9.6** | Without it the optional capability is **undiscoverable from its own supertype**, so the accept-unknown arm becomes permanent for exactly the third-party channels most likely to fan out |
| **11** | **`ExclusiveSubscribable.SingleSubscriber` states it is a report about THIS channel in THIS process, and that implementations must be safe for CONCURRENT USE** (D-J) | ⛔ **N/A until Task 9.6** | msgin never calls it concurrently, so a third-party implementer's data race is invisible to msgin's own `-race` suite. Also note the wrapper escape hatch (embed + shadow the method) |
| **12** | **`NewChannelExchange`'s godoc states FOUR outcomes** — rejected · accepted-exclusive · accepted-no-probe · **accepted-but-only-exclusive-within-this-process** — and enumerates **`ErrChannelSubscribed`**, which it returns unwrapped from `reply.Subscribe` (`exchange.go:250`) and which its error list omits today | ⛔ **N/A until Task 9.6** | ADR 0030 §Topology. The fourth outcome is the one a caller assumes away, because "the core rejects a shared reply channel" reads as a guarantee |
| **13** | **`endpoint.WithSharedReplyChannel`'s godoc states it SUPPRESSES THE PROBE and does not confer shareability** — on a `DirectChannel` the second exchange still gets `ErrChannelSubscribed` | ⛔ **N/A until Task 9.6** | CLAUDE.md's sensible-defaults rule names option godoc specifically; this is the one new symbol the plan left undocumented |

**Three unmet (3, 4, 7), one half-met (1), and four more (10–13) that arrive with Task 9.6 — eight Task 11
checkboxes in total. All are Plan 027 Task 11 checkboxes** — 10–13 are gated on 9.6 having landed, so Task 11 must run after it.

> **Obligations 10–13 are round-4 additions** (design audit BLOCKER 1 consequence 2, MINOR 2, MINOR 3,
> MINOR 7). They exist because D-J adds three exported symbols and a new acceptance outcome, and the
> as-written Task 9.6 specified godoc for only two of them. This is the §8 failure mode repeating in
> miniature: a new symbol whose documentation obligation has no owning checkbox.

### 8.0a §10's four multi-instance godoc obligations — same treatment

CLAUDE.md's multi-instance rule makes these normative too, and they were equally unowned:

| # | Obligation (§10) | Status at `dadc775` | Evidence |
|---|---|---|---|
| a | `DirectChannel` / `PublishSubscribeChannel` / the exchange correlator are **in-process only**, with **Return Address** named as the distributed answer | ✅ **MET** | `channel.go:33`, `channel/doc.go` (*"every channel here is IN-PROCESS ONLY"*), `endpoint/doc.go:19` |
| b | `channel.PubSub` is an in-process registry, naming the root `TopicPublisher`/`TopicSubscriber` SPI as the distributed answer | ✅ **MET at package level** — `channel/doc.go` names both. The `PubSub` **type** godoc (`channel/pubsub_registry.go:10`) still says only "in-process topic registry"; folding one clause into it is optional polish, not a gap | `channel/doc.go` |
| c | `channel.WithSingleSubscriber()` is a **single-process** guard and must never read as a distributed exclusivity guarantee | ❌ **UNMET.** The godoc (`channel/pubsub.go:69-83`) covers what it does, why it is off by default, and the exchange mis-wiring — but never states the process boundary, which ADR 0028 §6.2 explicitly requires | `grep -n -A20 'func WithSingleSubscriber' channel/pubsub.go` |
| d | `RetryPolicy.MaxAttempts` is **per-instance**, so the effective global bound behind a load balancer is `N × MaxAttempts` | ❌ **UNMET.** `retry.go:37-41` says nothing about topology. (`endpoint/producer.go:201` and `adapter/http/outbound.go:288` discuss per-instance *Retry-After*, a different thing) | `grep -rn 'N × MaxAttempts' --include='*.go' .` → exit 1 |

**Two of four unmet.** Both are Plan 027 Task 11 checkboxes.

### 8.1 The staleness sweep is a gate, because nothing else catches it

`go build`, `go vet`, `go test`, and `gofmt` are **all blind** to a stale `msgin.X` in a comment. Two arms are
needed, and the second is new (F9.8, F11.7):

**The tools are committed, not in `/tmp`.** `decls` and `qualify` live at
[`docs/plans/027-tools/`](../plans/027-tools/) as `//go:build ignore` programs run with `go run`, and
`symmap.tsv` sits beside them. **No gate in this bundle may depend on `/tmp`** — the derivation run's binaries
under `/tmp/msgin-derive/` are gone on any fresh clone or `/tmp` reap, which would have made this gate and
Task 12's invariant block unrunnable with no rebuild instructions.

```bash
# Regenerate symmap.tsv first — it is derived, and it goes stale on every move:
for p in endpoint routing transform channel resilience; do
  go run docs/plans/027-tools/decls.go $p | grep -v '_test\.go' \
    | awk -F'\t' -v P=$p '$5=="exported" && $3!="method" {print P"\t"$4}'
done | sort -u -k2,2 > docs/plans/027-tools/symmap.tsv        # 91 symbols at dadc775

# ARM 1 — MOVED symbols still qualified as msgin.X
while IFS=$'\t' read -r pkg sym; do
  grep -rn --include='*.go' --exclude-dir=docs "msgin\.${sym}\b" .
done < docs/plans/027-tools/symmap.tsv  # currently 2 survivors: codec.go:33, routing/aggregator_test.go:21

# ARM 2 — CONSTRUCTOR/OPTION/SENTINEL-SHAPED names (With*/Err*/New*) referenced UNQUALIFIED in a
# LINE comment, that are declared nowhere in the ten scanned packages.
# An INVARIANT within that shape, not an enumeration: no edit is needed when a symbol is deleted,
# and it catches names that NEVER existed (typos) as well as names that stopped existing.
# KNOWN BLIND SPOTS — stated, because an overclaimed gate is a decorative gate (round-5 MINOR 5):
#   * shape: a name outside With|Err|New is invisible (FilterExpr, StreamingSource, boxMessage...)
#   * block comments: grep -o '//.*' never sees /* ... */
#   * scope: the declared-side scan covers 10 package dirs, not the 6 satellite modules
# Arm 1 covers the moved-symbol class; NEITHER arm covers a deleted non-With/Err/New name, so a
# task that deletes such a symbol must grep for it explicitly.
comm -23 \
  <(grep -rh --include='*.go' -o '//.*' . \
      | grep -oE '(^|[^.[:alnum:]_])(With|Err|New)[A-Z][A-Za-z0-9]*' \
      | grep -oE '(With|Err|New)[A-Z][A-Za-z0-9]*' | sort -u) \
  <({ go run docs/plans/027-tools/decls.go . ; for p in endpoint routing transform channel resilience \
        adapter/memory adapter/cron adapter/database/sql adapter/http adapter/http/stdlib; do \
        go run docs/plans/027-tools/decls.go ./$p; done; } \
      | awk -F'\t' '$5=="exported"{sub(/^.*\./,"",$4); print $4}' | sort -u) \
  | grep -vxE 'ErrNoRows|ErrUnexpectedEOF|ErrUnsupported|ErrNoPayloadCodec|WithBusyTimeout|WithImage|WithInstanceID|WithJournalMode|WithSharedMemory|WithX'
# at dadc775 → exactly one line: WithRelease   (routing/aggregator.go:316 — see below)
```

**The arm-2 allow-list is short, and every entry is justified** — an unexplained allow-list is how a gate
becomes decorative:

| Allowed | Why |
|---|---|
| `ErrNoRows`, `ErrUnexpectedEOF`, `ErrUnsupported` | stdlib sentinels named unqualified in prose |
| `WithBusyTimeout`, `WithImage`, `WithJournalMode`, `WithSharedMemory` | options of satellite modules / testcontainers, outside the ten packages scanned |
| `WithInstanceID` | a **deliberate negative**, same class as `ErrNoPayloadCodec`: `adapter/cron/sqlelector.go:33` records that *"its `WithInstanceID` was removed as YAGNI"*. *(Round-5 MINOR 6: this was filed under the satellite-module row, which is false — `adapter/cron` IS one of the ten scanned packages and the symbol is declared nowhere.)* |
| `ErrNoPayloadCodec` | a **deliberate negative** — `endpoint/producer.go:617` says *"there is no `ErrNoPayloadCodec`"* (ADR 0009 D4). The gate must not force a doc to stop naming what does not exist |
| `WithX` | generic placeholder prose (`adapter/http/options.go:141`, `doc.go:50`) |

> **ROUND-4 REDESIGN (B3/B4/exec-B1) — the previous arm 2 was VACUOUS and its survivor list was fiction.**
> The published pattern was a hardcoded list of the six `*Expr` names, and it returns **zero hits** — at
> `dadc775`, at `3d0b87a`, and at `0e2dcf0`. It had *never* matched anything. Meanwhile both this spec and
> Plan §9.5.1 published *"7 survivors"* at named lines, every one of which now holds unrelated text
> (`errors.go:156` is a `WithDefaultChannel` comment; `routing/splitter.go:52` says "Split constructor";
> `routing/aggregator_test.go:1276` says `WithReleaseStrategy` — the pattern cannot match any of them).
>
> **A gate whose command and whose published result disagree is worse than no gate**: the box ticks with zero
> work while the enumeration misleads anyone who reads it instead. The replacement asserts a *property*
> — within its shape, every name a comment mentions is a name that exists — rather than enumerating instances
> of the violation, which is why it finds `WithRelease`, a name that **never existed** and that no
> deleted-symbol list could ever have contained.
>
> **Its reach is narrower than the first draft of this paragraph claimed** (round-5 MINOR 5, proven by planted
> probe comments): a `// Probe: FilterExpr … StreamingSource are gone.` line produces **no hit**, because the
> extractor only matches `With|Err|New` names — so the redesign covers **2 of the 6** `*Expr` names the old
> arm targeted, and misses the `StreamingSource` rename entirely. It is a strictly better gate than the one it
> replaced (which matched *nothing*), but it is **not** the universal staleness check the wording implied. The
> blind spots are now stated in the command block itself rather than discovered by the next auditor.

**The one genuine survivor at `dadc775`:** `routing/aggregator.go:316` —
`// release-decision error (the WithRelease strategy failed)`. The option is `WithReleaseStrategy`.

Both arms must be empty (arm 2 modulo the allow-list above). Run the sweep **after the last move**, tree-wide
— it is *not* an adapter-only task: the core-extraction tasks leave their own stale mentions behind.

## 9. Acceptance criteria

1. **Layout & C-full** — root holds the **14** source files enumerated in §3.1, its exported surface matches
   §4's closed list exactly (**102** exported non-method symbols — measured 102 at `dadc775`, and projected to
   return to 102 after D-I removes two and D-J adds two; §4 carries the arithmetic and Task 12 **measures**
   rather than transcribes it), and **no core package imports another core package**:

   ```bash
   go list -deps . | grep -E 'kartaladev/msgin/(endpoint|routing|transform|channel|resilience)'          # EMPTY

   # NOTE the second grep: `go list -deps` includes its ARGUMENT packages, so without it this arm
   # prints five lines on a correct tree and can never be empty (§3).
   go list -deps ./endpoint ./routing ./transform ./channel ./resilience \
     | grep 'kartaladev/msgin/' \
     | grep -vE '^github.com/kartaladev/msgin/(endpoint|routing|transform|channel|resilience)$'          # EMPTY
   ```

   `go list -deps` reports the **non-test** dependency closure, which is exactly the invariant: root's
   `package msgin_test` files legitimately import the new subpackages (§3.4) and must not fail this check.
   *(Adapters importing `msgin/resilience` are consumers, not peers — §3.)*
2. **Move-list fidelity** — `apidiff` against the pre-window baseline reports **only** §4.1's removals and
   additions, each reconciled against §3.1–§3.3. Measured **95 / 6** at `dadc775`; **projected 97 / 8** once
   D-I and D-J land (§4.1). An unexplained entry blocks the merge — and a *number* that disagrees with §4.1
   is a defect in §4.1, not in the measurement.
3. **Dependency** — `go list -deps .` excludes `expr-lang`, and **all seven existing modules are
   `go mod tidy`-clean with `expr-lang` absent from every `go.mod` AND every `go.sum`**, proved by the
   per-module loop in §7 (not by measuring root and generalising — Plan 027 Global Constraint 0).
   (`robfig/cron` **stays** — RFC-0004's settled decision.)
4. **Capability** — a test proves a `QueueChannel`, a `PublishSubscribeChannel`, **and** a shipped
   `OutboundAdapter` (e.g. `*memory.Broker`) can each serve at **all eight** send-only positions §5.0
   enumerates. Today it covers **three** (`capability_test.go:152,163,174` — filter discard, router default,
   exchange request), i.e. **9 of the required 24 subtests**. The five missing are rows **3, 4, 5, 7, 8**:
   `routing.NewRouter`'s `pick` **return**, the Aggregator **output** and **expired-group** sinks, and the two
   HTTP inbound targets. Row 3 is the one every draft has dropped, and it is the position where a
   *user-supplied* `pick` returns a durable channel chosen at **message time** — the widening's sharpest case.
   3 targets × 6 core positions live in root's `capability_test.go`; 3 × 2 HTTP positions live in
   `adapter/http` and `adapter/http/stdlib`. This is the defect in §1.2, so it must fail against the
   pre-window code. **A compile failure produces no `FAIL` line** — the RED artifact is the compiler
   transcript from `go test -c -o /dev/null .`, not `go vet` (which stops after one type-error batch).
5. **Behavior preservation** — every pre-existing test passes, modified only where it names a moved or
   narrowed signature. No test's assertions change, outside §2.1's four exceptions. **Proved by the
   normalised per-file diff** — *not* by a `Test*`/`Example*` name-set identity, which §2 withdrew in round 5
   as false in every frame (224 → 211 → 221 unique names across the window; 17 left and 14 arrived, all
   documented in §4.1 and §6).
6. **Gates** — `go test ./... -race` green in **every one of the eight modules standalone** (`GOWORK=off`, as
   CI runs it), `go vet`, `golangci-lint`, `gofmt`, `govulncheck` clean, `go mod tidy` a no-op in every
   module, and `CGO_ENABLED=0 go build ./...` succeeds. **`go test` on `harness` proves nothing** — it has no
   test files of its own; `go vet` (which compiles it) plus `dbtest`'s Docker run are what exercise it.
7. **Coverage** — measured with **`-coverpkg=./...` on both sides** (§3.4e), ≥85% workspace-wide, and every
   hot-path and typed-error branch covered.

   **The exception list is enumerated, not summarised.** The earlier wording named *two* gaps; a full
   max-deduplicated enumeration of the six core packages at `b6ce7bb` found **eleven** uncovered blocks (ten
   stable plus the flaky `consumer.go:467` arm), so *"no new uncovered block was introduced"* was true only
   against the wrong baseline. **Five were fixed in the round-3 pass; six remain,** each with a disposition:

   ```
   $ awk 'NR>1 { if ($3+0 > m[$1]) m[$1]=$3+0 } END { for (k in m) if (m[k]==0) print k }' head.cov \
       | grep -E '^github\.com/kartaladev/msgin/(endpoint|routing|transform|channel|resilience)/|^github\.com/kartaladev/msgin/[a-z_]+\.go' | sort
   ```

   | Block | Disposition |
   |---|---|
   | `endpoint/consumer.go:467.20,469.15` | **Accepted, flaky.** The `case <-ctx.Done():` arm of the dispatch select — covered in roughly 1 run in 3 (F10.8). A gate that diffs one run against another produces false regressions here. |
   | `endpoint/gateway.go:30.27,32.3` | **Accepted, pre-existing.** Byte-identical before and after the split. |
   | `endpoint/nativereliability.go:9.52,9.68` | **Accepted, pre-existing.** The `noNativeReliability` no-op, relocated unchanged (§3.3a). |
   | `endpoint/poller.go:152.11,153.80` | **Accepted, pre-existing.** |
   | `endpoint/poller.go:164.12,166.3` | **Accepted, pre-existing.** |
   | `resilience/breaker.go:179.28,181.3` | **Accepted, pre-existing** — this is the `toHalfOpen` gap at **87.5%**, 87.5% before the split too (round-2 §D7). |
   | ~~`handler.go:66.25,67.45` · `:67.45,68.64` · `:68.64,68.85`~~ | **FIXED** — root's dead `nilFuncStep` deleted (F12.4). |
   | ~~`payload.go:30.51,32.2`~~ | **FIXED** — root's dead `boxMessage` deleted (F12.4). |
   | ~~`reliability.go:39.16,41.3`~~ | **FIXED** — `IsPermanent`'s `err == nil` arm, now covered by `TestIsPermanent` (F12.3). |

   **Root now has zero uncovered blocks.** Any block not in the six-row accepted list above is a finding.
8. **Docs** — `MIGRATION.md` written; CLAUDE.md's architecture blueprint and dependency policy updated in the
   same commit; `MESSAGING.md` reconciled; §3.5's package docs complete (**all six present** — the five
   subpackage `doc.go` files landed in the round-3 pass, F12.5); §8's **nine** godoc bullets and §10's **four**
   multi-instance obligations complete — audited per bullet in §8/§8.0a, **six of the thirteen unmet at `dadc775`** (plus obligations 10–13, which arrive with Task 9.6),
   all owned by Plan 027 Task 11; §8's godoc
   alignment complete and **§8.1's two-arm staleness sweep empty**.
9. **Reply-channel exclusivity (D-J, §5.1, [ADR 0030](../adrs/0030-reply-channel-exclusivity-probe.md))** — a
   table test covers **all four arms** of the probe truth table (probe absent → accepted; `true` → accepted;
   `false` → `ErrSharedReplyChannel`; `false` + `WithSharedReplyChannel()` → accepted), and the
   probe-absent arm is driven by a **test-local `SubscribableChannel` that omits the method**, since no
   in-tree type can produce that arm. `NewChannelExchange`'s reply godoc states all three acceptance
   outcomes, including that an unknown channel is **accepted**. The rejection happens **before**
   `reply.Subscribe`, proved by a case asserting the channel has no subscriber after a rejected construction.
10. **Expr sentinels (D-I, §3.2, §7)** — `grep -n 'ErrInvalidExpression\|ErrExprResultType' errors.go` is
    **empty**, the `expr` module declares both with the `msgin/expr:` prefix, and no alias to the deleted root
    vars exists anywhere (`grep -rn 'msgin\.ErrInvalidExpression\|msgin\.ErrExprResultType' --include='*.go' .`
    → empty).

## 10. Multi-instance topology (CLAUDE.md mandatory review)

This increment moves and narrows existing components; it introduces no new cross-instance state. The
topology statements it must **preserve and state explicitly in godoc**:

- **`DirectChannel`, `PublishSubscribeChannel`, `ChannelExchange`'s correlator — in-process only.** A Go
  channel and an in-memory correlator map cannot cross a process boundary. `SubscribableChannel` is therefore
  an **in-process contract**: a reply arriving at instance B for a request made on instance A is not reachable
  through it. The distributed answer is the **Return Address** pattern via a future external
  `RequestReplyExchange` adapter — the seam stays in root (§3.2 keeps the interface there precisely so the
  implementation can live outside), and this increment must not narrow it shut
  (cf. [Spec 010 §8.1](010-messaging-gateway.md), [ADR 0022](../adrs/0022-messaging-gateway.md)).
- **`channel.PubSub` is an in-process topic registry** — `map[string]*PublishSubscribeChannel` guarded by a
  local mutex (`channel/pubsub_registry.go:13`). Two instances each hold their own registry; a `Publish` on
  instance A never reaches a subscriber on instance B. The godoc says "in-process" today but **does not name
  the distributed answer**: a native-topic broker adapter implementing the root
  **`TopicPublisher`/`TopicSubscriber`** SPI (Kafka/NATS/Redis topics). D-B keeps that seam in root precisely
  so the adapter can supply it without a core change. *(Round-2 §D3 — this component was omitted from the
  mandatory review entirely.)*
- **`channel.WithSingleSubscriber()` is a single-process guard**, exactly like `DirectChannel`'s. Two
  instances each holding their own `PublishSubscribeChannel` still each accept a subscriber. It must never be
  documented as a distributed exclusivity guarantee.
- **`endpoint`'s `attemptTracker` holds per-instance attempt counts** (`endpoint/attempts.go:26`), so
  **`RetryPolicy.MaxAttempts` is per-instance across N nodes**: a message redelivered to a different instance
  starts its count at zero, and the effective global bound is `N × MaxAttempts`. This applies **only** to
  sources without a native delivery-count header; a source with one (`NativeReliability`) is unaffected. The
  distributed answer is the broker's own redelivery count, or a shared **idempotency/dedup store**. This must
  be stated on `RetryPolicy.MaxAttempts`'s godoc — a caller sizing a poison-message threshold behind a load
  balancer will otherwise get `N×` what they asked for. *(Round-2 §D3.)*
- **`QueueChannel` over a durable `ChannelStore`** is the multi-instance-safe conduit; competing consumers come
  from the worker pool plus the store's claim semantics, unchanged here.
- **§5.1's exchange exclusivity is an in-process statement.** Two exchanges sharing a reply channel is a
  single-process misconfiguration; the cross-process equivalent is two *instances* sharing one durable reply
  topic, which Return Address addresses by carrying the reply destination in the message.
- **`ExclusiveSubscribable.SingleSubscriber()` reports THIS channel in THIS process (D-J,
  [ADR 0030](../adrs/0030-reply-channel-exclusivity-probe.md)).** N instances behind a load balancer each hold
  their own `PublishSubscribeChannel`; under `WithSingleSubscriber` each reports `true` and each accepts its
  exchange — correctly, because each **is** exclusive within its process. The probe therefore adds **no**
  cross-instance state and makes **no** cross-instance claim, and its godoc must say so in the same terms
  `WithSingleSubscriber`'s does. **A probe returning `true` for a channel that is shared across processes
  would be precisely the false guarantee the registry alternative was rejected for** (ADR 0028 §6.2), which is
  why the method's contract is worded as a report rather than a guarantee. The distributed answer is unchanged
  and remains **Return Address** via the future external `RequestReplyExchange` adapter.

Narrowing `MessageChannel` to send-only **widens** what a distributed deployment can plug in — a durable
`QueueChannel` now qualifies everywhere a `DirectChannel` was previously required, including as an
**HTTP inbound target** (§5.0 rows 7–8) and as an Aggregator **output/expired-group** sink (rows 4–5), and so
does every outbound adapter (§5.3). This change moves in the right direction for the topology rule.

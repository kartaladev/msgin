# ADR 0001 — Message payload typing: generics with a codec seam

- **Status:** Accepted (2026-07-16)
- **Context source:** [Spec 001 — Messaging core](../specs/001-messaging-core.md) §3–§5; [`MESSAGING.md`](../../MESSAGING.md) §2.1, §2.8, §2.12
- **Related:** [ADR 0002 — Adapter SPI](0002-adapter-spi.md)

## Context

`msgin`'s message envelope must decide how the **payload (body)** is typed. EIP's *Message*
pattern (`MESSAGING.md` §2.1) splits a message into **header** (metadata the system uses) and
**body** (application data the system passes through opaquely). Two forces pull in opposite
directions:

- EIP's **Datatype Channel** (§2.8) says each channel should carry one data type so the receiver
  knows what to expect without inspecting content — and the source itself notes this maps to a
  compile-time `Channel[T]` in a typed language, buying type safety and self-documenting code.
- The **adapter boundary is inherently untyped** — bytes on a wire, columns in a table — and
  **Canonical Data Model** (§2.12) argues a general library should standardize the *envelope*, not
  the *business body*.

`msgin` also names **debuggability** (compile-time errors, readable types) a first-class goal.

## Decision

Use **generics on the caller-facing API**, with the message's serialization split into **two
independent encodings** placed in different layers (this corrects an earlier formulation the design
audit found broken — see "Audit correction" below):

- `Message[T]`, `Producer[T]`, `Consumer[T]`, `Handler[T]` on the caller surface. The channel's
  payload type is a compile-time contract (Datatype Channel realized as `Channel[T]`).
- **Payload codec — `T` ⟷ `[]byte`.** Business (de)serialization (`PayloadCodec[T]`, default JSON).
  It **requires `T`**, so it lives in the **typed runtime/endpoint layer**, which knows `T`. On send
  the runtime encodes `T`→`[]byte`; on receive it decodes `[]byte`→`T`.
- **Envelope framing — `(headers, body-bytes)` ⟷ storage.** How headers + opaque body map to a
  backend's storage shape. It is **type-agnostic** and lives in the **adapter**.
- The adapter SPI is non-generic and carries `Message[any]`, where `Payload` is `[]byte` for wire
  adapters (the runtime pre-encoded the body) or the **live Go value** for the in-memory adapter
  (no payload codec, zero-copy). The runtime chooses decode-vs-assert by whether a `PayloadCodec` is
  configured, and construction validates the pairing.

The library fixes the **envelope** (headers: id, timestamp, content-type, correlation-id,
delivery-count); the application owns the **body** type.

## Audit correction

An earlier draft claimed the inbound `T` ⟷ `any` step was "a cheap type assertion, not
serialization," with a single non-generic `Codec.Decode([]byte) (Message[any], error)` living inside
each wire adapter. This is **wrong on the inbound path**: a wire adapter has `[]byte` and does not
know `T`, so `json.Unmarshal` produces `map[string]any`/`float64`, and `msg.Payload.(T)` fails on
every wire message (the in-memory adapter would mask it in early tests). The fix is the split above:
the **typed decode `[]byte`→`T` must happen where `T` is known (the runtime)**, while the adapter does
only type-agnostic envelope framing. On the inbound path the runtime step *is* deserialization, not a
mere assertion.

## Consequences

**Positive**
- Compile-time type safety and self-documenting endpoints on the surface callers touch — directly
  serves the debuggability goal.
- The untyped edge is *confined*: type assertions do not litter application code; they happen once,
  in the runtime, behind a typed error.
- Faithful to EIP: Datatype Channel on top, opaque body at the transport, envelope standardized.

**Negative / costs**
- Two encodings to keep straight (payload codec vs envelope framing) — mitigated by placing them in
  distinct, documented layers (runtime vs adapter).
- The runtime must know whether to decode `[]byte` (wire) or assert a live value (memory); resolved
  by whether a `PayloadCodec` is configured, validated at construction (typed error, ADR 0002/§5).
- Heterogeneous flows still require `Message[any]` at the caller when a stage is genuinely
  type-agnostic; this is expected and available.
- Generics add some signature verbosity in the endpoint layer.

**Rejected alternatives**
- **Fully `any` payload** — simplest core, but pushes type assertions and runtime type errors into
  every consumer; weakens the debuggability goal.
- **`any` core + optional typed façade** — flexible, but two layers to learn and a leak risk; the
  chosen design already gives an untyped seam exactly where it's unavoidable (adapters) without a
  parallel façade layer.

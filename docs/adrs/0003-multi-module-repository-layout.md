# ADR 0003 — Multi-module monorepo: heavy-client adapters as separate modules

- **Status:** Accepted (2026-07-16). Supersedes the "single module" lean recorded in `CLAUDE.md`'s open decisions.
- **Context source:** [Spec 001 — Messaging core](../specs/001-messaging-core.md) §10; `CLAUDE.md` → Dependency policy, open decision #1

## Context

`msgin` mandates **minimal dependencies for consumers** — every direct dependency of a module is
forced on everyone who imports it. v1 ships six adapters, three of which need heavy third-party
clients: `redis` (go-redis), `nats` (nats.go), and `database/pgx` (jackc/pgx). The `memory`,
`database/sql`, and `http` adapters need only the standard library. We must package these so that a
consumer who uses only, say, the in-memory adapter does not inherit go-redis, nats.go, and pgx in
their module graph — while still giving heavy adapters ergonomic, turnkey constructors backed by
their real client.

## Decision

Adopt a **multi-module monorepo** — one git repository containing several Go modules:

```
msgin/                              module github.com/kartaladev/msgin   (CORE: stdlib + clockwork + cenkalti/backoff)
├── adapter/memory/                 in CORE (stdlib; reference test double)
├── adapter/http/                   in CORE (net/http; stdlib)
├── adapter/database/sql/           in CORE (database/sql; Postgres + MySQL dialects)
├── adapter/database/pgx/  go.mod   module …/adapter/database/pgx   (imports jackc/pgx/v5)
├── adapter/redis/         go.mod   module …/adapter/redis          (imports go-redis)
├── adapter/nats/          go.mod   module …/adapter/nats           (imports nats.go)
├── go.work                         local multi-module dev/test
```

- **Core module** = pattern core + SPI + runtime + `memory` + `http` + `database/sql`. Its only
  non-stdlib dependencies are `clockwork` (ADR 0004) and `cenkalti/backoff/v4` (ADR 0005). `memory`
  stays in-core because it is the reference test double the core's own blackbox tests use; `http`
  and `database/sql` stay in-core because they need only the standard library.
- **`database/pgx`, `redis`, `nats`** are **separate modules**, each `require`-ing the core module
  and importing its real client directly, shipping turnkey constructors. Consumers `go get` only the
  adapter modules they use.

## Consequences

**Positive**
- Consumers inherit only the heavy clients for adapters they actually import; the core stays
  stdlib + clockwork. Satisfies the minimal-deps mandate.
- Heavy adapters get real, turnkey APIs (no forcing users to hand-wire a client through a narrow,
  leaky interface — which was especially poor for JetStream's large surface).
- Each adapter module is versioned and released independently.
- Precedent: Watermill and otel-contrib package brokers/integrations as separate modules the same way.

**Negative / costs**
- **Releases use module-path-prefixed tags** — `v0.1.0` (core), `adapter/database/pgx/v0.1.0`,
  `adapter/redis/v0.1.0`, `adapter/nats/v0.1.0`. The tag-driven release workflow must handle multiple
  prefixes. (Revises the release model noted in `CLAUDE.md`.)
- Local development needs a **`go.work`** to build/test across modules together.
- CI tests each module (a small matrix).
- Cross-module changes may span two tags; the core must keep backward compatibility for adapter
  modules per SemVer.

**Rejected alternatives**
- **Single module + injected client interfaces** — still makes the user depend on the client, but
  forces them to hand-wire it; a narrow interface over JetStream is leaky.
- **Single module importing clients directly** — pulls go-redis + nats.go + pgx into *every*
  consumer's `go.sum`, even memory-only users. Violates the minimal-deps mandate.

# msgin RFCs — pre-v1 refactor program

A coordinated set of RFCs for one pre-v1 refactor: reduce reader/maintainer cognitive load and align with the
EIP / Spring Integration consensus. They share a **single breaking window** (`v0.x`, SemVer-major intent) and
one design language: **interfaces + value types in the core; implementations and providers in isolated
packages; dependencies point inward.**

Each RFC follows the [RFC template](https://gist.github.com/rowlando/416f41e34fe32840c5634a660df790e1). All are
**Draft**. They precede the CLAUDE.md artifact chain (spec → plan → ADR) — an accepted RFC spawns those.

## Index

| RFC | Title | Scope | Breaking? |
|---|---|---|---|
| [0001](0001-core-package-restructure.md) | Core package restructure | Split flat root into `channel`/`endpoint`/`resilience`; SPI+engine stay in root | Yes (amends ADR 0003) |
| [0002](0002-eip-alignment.md) | EIP semantic & lexical alignment | Fidelity-audit renames + godoc fixes | Partly |
| [0003](0003-endpoint-behavior-types.md) | Endpoint behavior types & provider model | Named `FilterPredicate`/`RoutingFunction`/…; expr becomes a provider; drop expr from core | Partly (amends ADR 0019) |
| [0004](0004-trigger-scheduling.md) | Trigger-driven scheduling: Poller & scheduled sources | One `Trigger` SPI; extract `Poller`; dissolve `adapter/cron` | Partly (amends ADR 0016/0017) |

## How they relate

```
0001 (packages) ──┬── defines "the engine"/runtime home used by ──▶ 0004 (Poller lives with the engine)
                  └── defines "endpoint" pkg used by ──────────────▶ 0003 (behavior types + endpoint/expr)
0002 (naming) ── renames land in the same apidiff pass as 0001; new names in 0003/0004 must not re-drift
0004 (Trigger SPI) ── one SPI serves BOTH the Poller and the (dissolved) cron scheduled-source
```

- **0001 is the substrate** — it decides where `runtime`/`endpoint`/`resilience` live; 0003 and 0004 place
  their new code into those homes.
- **0002 is cross-cutting** — its renames ride 0001's package moves (one breaking review); 0003/0004 adopt
  Spring-aligned names so they add no new drift.
- **0004 unifies** the Poller (RFC's item a) and the cron dissolution (item b) under one `Trigger` SPI —
  design them together, not as two shapes.

## Recommended sequencing (one breaking window)

1. **Non-breaking first** (can land ahead of the window): RFC-0003 phase 1 (name the behavior types),
   RFC-0004 phases 2–3 (extract `Poller`, add triggers, `WithTrigger` sugar), RFC-0002 godoc fixes.
2. **The window** (breaking, one ADR + apidiff pass): RFC-0001 package moves + RFC-0002 renames together;
   then RFC-0003 phase 3 (drop expr from core) and RFC-0004 phases 4–5 (cron dissolution, robfig isolation).
3. **Deferred:** RFC-0001 C-full (extract the engine to `runtime`) — once the API stabilises.

**Sequence the whole program after the pending feature roadmap** (see `docs/HANDOVER.md`); package moves and
renames conflict badly with in-flight feature branches. Split from a quiet `main`.

## Using this for a refactor session

Read CLAUDE.md, then this index, then the RFCs in number order. Each RFC's §5 Implementation Plan is
phase-decomposed and green-per-increment; its §7 Open Questions are the decisions to settle before coding. Per
CLAUDE.md, promote each accepted RFC to a spec + ADR(s) + plan, run the adversarial design audit on the bundle,
and execute via SDD.

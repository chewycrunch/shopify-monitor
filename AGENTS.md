# shopify-monitor

## LID

- Mode: Full
- Version: 1.3.0

Consult the `linked-intent-dev` skill for all code changes. Changes walk the
arrow — HLD → LLD → EARS → edge audit → tests → code — with a stop at each
phase boundary. Bug fixes walk it too; there is no short-circuit.

Design docs live under `docs/`:

- `docs/high-level-design.md` — the root HLD.
- `docs/intent/<segment>/<segment>-design.md` — leaf LLDs.
- `docs/intent/<segment>/<segment>-specs.md` — EARS specs owned by that leaf.

Code and tests carry `@spec` annotations citing the EARS IDs they implement or
verify, placed at the entry point of the behaviour's implementation graph.

---
parent: high-level-design
prefix: DET
---

# Change Detection

## Context and Design Philosophy

This segment owns the answer to "what changed, and is it worth telling anyone
about?" It consumes a crawl from `catalog-acquisition` and holds the record
those crawls are compared against.

Two ideas govern everything below.

**A record is only ever written from an observation.** Availability is set from
seeing a variant in a crawl, never inferred from not seeing one. Crawls are
routinely incomplete — a page fails and its region of the catalogue goes unread
that cycle — and a record that treats absence as information would read a
network failure as thousands of products selling out. This is the contract that
makes `catalog-acquisition`'s tolerance of partial crawls safe, and it is the
one rule here that must not be relaxed without revisiting that segment.

**Recording state and reporting a change are separate steps.** State is recorded
for every variant observed, unconditionally. Whether that constitutes a
reportable event is decided afterwards, from what the record held before. Fusing
the two — writing the record only on the branches that report — leaves
transitions that report nothing unrecorded, and a record that has silently
stopped tracking cannot detect the next transition either.

## The Availability Record

One record per store, mapping variant identifier to whether that variant was
available the last time it was observed.

Variant identifiers are the unit rather than products, because availability is a
per-variant property: a shoe in eleven sizes restocks one size at a time.

The record is held in memory for the life of the process. A restart discards it
and re-establishes it from a fresh baseline crawl, which is a deliberate trade:
persistence would buy continuity across restarts at the cost of a store of truth
that can itself be stale or corrupt, and a restart is cheap.

## Baseline and Watch

A store's first crawl establishes its record and reports nothing. Every variant
in it is being seen for the first time, so treating first sightings as events
would make startup report the entire catalogue — tens of thousands of variants
for a large store.

This is why the baseline crawl must be complete. A variant missing from the
baseline is indistinguishable, on the next crawl, from one that has just
appeared, so an incomplete baseline converts a failed page into a burst of
spurious reports on the following cycle.

Every crawl after the first is a watch crawl, and its differences against the
record are candidate events.

## Classifying a Change

For each variant observed in a watch crawl, the previous record and the observed
availability determine the outcome:

| Previously recorded | Observed | Outcome |
| --- | --- | --- |
| not recorded | available | **new variant** — report |
| not recorded | unavailable | record only |
| unavailable | available | **restock** — report |
| available | unavailable | record only |
| unchanged | unchanged | record only |

The record is written in every row, including the rows that report nothing.

**Only a transition into purchasable is reported.** Both reporting rows describe
the same thing from the operator's side: something is buyable now that was not
buyable before. A variant appearing already unavailable is a listing, not an
event — a store publishing a product ahead of a drop can produce hundreds of
them in a cycle, and reporting those would bury the case worth acting on. A
variant going out of stock is recorded because the next restock depends on it,
but going out of stock is not itself news.

## Interaction with Incomplete Crawls

A variant on a page a crawl failed to read is not observed, so its record is
untouched and no event is considered for it. The next crawl that reads that page
compares against the record as it stood, and any transition that completed while
the page was unread is detected then.

A transition that both began and ended inside an unread window is lost rather
than delayed: a variant that restocked and sold out again between two successful
reads of its page presents as unchanged. This is accepted. Detecting it would
require the store to report history, which the catalogue endpoint does not.

## Decisions & Alternatives

| Decision | Chosen | Alternatives Considered | Rationale |
| --- | --- | --- | --- |
| Record update | Write on every observation, before classifying | Write only on the branches that report an event | Writing only where an event fires leaves the unreported transitions unrecorded, so a variant that sells out keeps a stale "available" record and its next restock compares equal and reports nothing. Every variant available when first observed would be permanently undetectable after its first sell-out. |
| Absence | Never changes a record | Treat absence as unavailable; treat absence as deletion | Crawls are routinely partial by design, so absence is ambiguous between "not stocked" and "not read". Acting on it converts a failed page into thousands of false events. |
| Unit of state | Variant | Product | Availability is per-variant; a product-level record cannot express one size restocking. |
| Baseline reporting | Silent | Report first sightings uniformly | Every variant is a first sighting at baseline, so uniform reporting means reporting the whole catalogue at startup. |
| Reportable events | Transitions into purchasable only | Also report sell-outs; report all first sightings | Both reporting rows mean "buyable now, not before". Sell-outs and unavailable listings are recorded because later detection depends on them, but neither is actionable on its own, and unavailable first sightings arrive in bulk when a store stages a drop. |
| Persistence | In memory, rebuilt on restart | Persist the record across restarts | A restart costs one baseline crawl. A persisted record adds a second source of truth that can be stale or corrupt, for a saving measured in one crawl. |

## Open Questions & Future Decisions

### Deferred

1. **A delisted variant that returns available is not reported.** A variant
   recorded as available, removed from the catalogue, then republished still
   available compares equal and reports nothing. When the catalogue was read
   completely — no pages missed — absence is unambiguous and could be recorded
   as a third state, making the variant's return a first sighting again. This is
   only sound on a complete crawl, which is precisely the distinction partial
   tolerance already turns on. Not built: it addresses one narrow case, and a
   variant that returns *unavailable* and later restocks is already detected.
   Note that a product deleted and recreated rather than republished receives
   new identifiers, and is already detected as new.
2. **The record grows without bound.** Every variant ever observed is retained
   for the life of the process, including variants long delisted. A large
   catalogue is tens of thousands of entries, which is small; nothing evicts
   them, which does not scale with monitored stores.
3. **Rapid transitions are not rate limited.** A variant flapping between
   available and unavailable reports on every transition into available. No
   suppression or debounce is applied.

## References

- `docs/high-level-design.md` — the exactly-one-notification success metric.
- `docs/intent/catalog-acquisition/catalog-acquisition-design.md` — partial
  crawl tolerance, which depends on this segment never inferring from absence.
- `docs/intent/notification/notification-design.md` — the consumer of reported
  events.

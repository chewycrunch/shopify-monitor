# Change Detection — Specs

Specs owned by `docs/intent/change-detection/change-detection-design.md`
(prefix `DET`).

The *availability record* is the per-store map from variant identifier to the
availability last observed for that variant. A *baseline crawl* is the first
catalogue read for a store, which establishes that record; every later read is a
*watch crawl*.

## The availability record

- [x] **DET-RECORD-001**: The system shall maintain one availability record per monitored store, keyed by variant identifier.
- [ ] **DET-RECORD-002**: When a variant is observed in any crawl, the system shall write that variant's observed availability to the availability record, whether or not the observation produces a reportable event.
- [x] **DET-RECORD-003**: When a variant that the system has previously recorded is absent from a crawl, the system shall leave that variant's recorded availability unchanged.
- [x] **DET-RECORD-004**: The system shall not report an event for a variant that is absent from a crawl.
- [x] **DET-RECORD-005**: When the process starts, the system shall begin with an empty availability record for every store and rebuild it from a baseline crawl.

## Baseline

- [x] **DET-BASE-001**: When a store's baseline crawl completes, the system shall record the availability of every variant it observed and shall report no events for that crawl.
- [x] **DET-BASE-002**: When a store's baseline crawl completes, the system shall report the total number of variants recorded.

## Classifying a watch crawl

- [ ] **DET-EVENT-001**: When a watch crawl observes a variant that is not present in the availability record and that variant is available, the system shall report a new-variant event.
- [ ] **DET-EVENT-002**: When a watch crawl observes a variant that is not present in the availability record and that variant is unavailable, the system shall record it without reporting an event.
- [x] **DET-EVENT-003**: When a watch crawl observes a variant recorded as unavailable and that variant is now available, the system shall report a restock event.
- [ ] **DET-EVENT-004**: When a watch crawl observes a variant recorded as available and that variant is now unavailable, the system shall record it without reporting an event.
- [x] **DET-EVENT-005**: When a watch crawl observes a variant whose availability matches the availability record, the system shall report no event for it.
- [x] **DET-EVENT-006**: When the system reports a new-variant or restock event, it shall identify the store, the product title, the product handle, the variant title, and the variant identifier.

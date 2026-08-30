# Catalog Acquisition — Specs

Specs owned by `docs/intent/catalog-acquisition/catalog-acquisition-design.md`
(prefix `ACQ`).

A *crawl* is one complete read of one store's catalogue. A *baseline crawl* is
the crawl that establishes a store's initial availability record; every later
crawl is a *watch crawl*. Both are catalogue reads and share the paging and
proxy rules below; they differ only where a spec names one of them.

## Paging

- [x] **ACQ-PAGE-001**: When requesting a page of a store's catalogue, the system shall request the endpoint's maximum page size of 250 products.
- [x] **ACQ-PAGE-002**: When beginning a crawl, the system shall request pages starting at page 1 and increasing by one, without gaps.
- [x] **ACQ-PAGE-003**: When a requested page returns fewer products than the requested page size, the system shall treat that page as the final page of the catalogue and request no further pages for that crawl.
- [x] **ACQ-PAGE-004**: When a crawl completes, the system shall present the products of all fetched pages in ascending page order, regardless of the order in which the pages were received.
- [x] **ACQ-PAGE-005**: When reading a product from a catalogue page, the system shall read its identifier, title, and handle, and for each of its variants the variant's identifier, title, and availability.
- [x] **ACQ-PAGE-006**: When a store's first catalogue page returns no products, the system shall complete the crawl reporting an empty catalogue rather than an error.
- [ ] **ACQ-PAGE-008**: If a catalogue page after the first is refused because it lies beyond the endpoint's pagination limit, then the system shall end the crawl with the pages already read rather than failing it.
- [ ] **ACQ-PAGE-009**: If the first catalogue page of a crawl is refused as a bad request, then the system shall fail the crawl.
- [ ] **ACQ-PAGE-007**: The system shall request the same page size for every page of a crawl.
- [ ] **ACQ-DEPTH-001**: When a store is configured with a maximum product count greater than zero, the system shall stop a crawl once it has collected that many products and shall return no more than that many.
- [ ] **ACQ-DEPTH-002**: When a store is configured with a maximum product count greater than zero, the system shall request no more pages than are needed to reach that count.
- [ ] **ACQ-DEPTH-003**: Where a store is configured with a maximum product count of zero, the system shall crawl until the catalogue ends rather than stopping at a product count.
- [ ] **ACQ-DEPTH-004**: When a store's catalogue ends before its configured maximum product count is reached, the system shall complete the crawl with the products the catalogue held.

## Proxy rotation

- [x] **ACQ-PROXY-001**: When issuing a catalogue page request, the system shall draw the next proxy from the shared pool for that individual request, rather than reusing one proxy for a whole crawl.
- [x] **ACQ-PROXY-002**: The system shall draw proxies for every monitored store from one shared pool, rather than assigning a distinct pool or partition to each store.
- [ ] **ACQ-PROXY-003**: While the proxy pool holds fewer entries than the configured page concurrency, the system shall issue no more concurrent page requests than there are proxies in the pool.
- [ ] **ACQ-PROXY-004**: While the proxy pool is empty, the system shall issue catalogue page requests one at a time.
- [ ] **ACQ-PROXY-005**: While the proxy pool is empty, the system shall report that catalogue requests are using direct connections once at startup, and shall not report it again for each page request.
- [x] **ACQ-PROXY-006**: If a proxy entry cannot be parsed into a usable URL, then the system shall report that entry as unusable and issue the request over a direct connection.
- [x] **ACQ-PROXY-007**: When rendering a proxy as a URL, the system shall include an explicit scheme, and shall percent-encode any credentials it carries.
- [x] **ACQ-PROXY-008**: While page requests for any store are being issued concurrently, the system shall advance the shared pool's rotation without data races.

## Crawl failure and timing

- [x] **ACQ-FAIL-001**: The system shall abandon an individual catalogue page request that has not completed within the configured per-request timeout.
- [ ] **ACQ-FAIL-002**: The system shall abandon a crawl that has not completed within the configured crawl deadline, cancelling any page requests still in flight.
- [ ] **ACQ-FAIL-003**: If a page request fails during a watch crawl, then the system shall retain the pages that succeeded, record the failed page as missed, and continue the crawl rather than discarding it.
- [ ] **ACQ-FAIL-004**: When a watch crawl completes with one or more missed pages, the system shall report how many pages were missed.
- [ ] **ACQ-FAIL-005**: If a page request fails during a watch crawl, then the system shall not re-request that page within the same crawl.
- [ ] **ACQ-FAIL-011**: If a catalogue page request fails, then the system shall retry that page, drawing a different proxy for each attempt, before treating the page as failed.
- [ ] **ACQ-FAIL-012**: When a catalogue page has failed every permitted attempt, the system shall report how many attempts were made.
- [ ] **ACQ-FAIL-013**: If a catalogue page is refused because it lies beyond the endpoint's pagination limit, then the system shall not retry that page.
- [x] **ACQ-FAIL-006**: If any page request fails during a baseline crawl, then the system shall treat the entire baseline crawl as failed and shall not establish a partial baseline.
- [x] **ACQ-FAIL-007**: When a baseline crawl fails, the system shall retry the baseline crawl in full until it completes or the process is shutting down.
- [x] **ACQ-FAIL-008**: If a watch crawl fails entirely, then the system shall retry on the next cycle rather than ending that store's monitoring.
- [x] **ACQ-FAIL-009**: While consecutive watch crawls for a store are failing, the system shall report each failure with the number of consecutive failures so far.
- [x] **ACQ-FAIL-010**: When a watch crawl succeeds for a store whose previous crawl failed, the system shall report the recovery once, naming how many consecutive failures preceded it.

## Poll cadence

- [x] **ACQ-CADENCE-001**: When any crawl for a store completes, whether baseline or watch, the system shall wait that store's configured interval before beginning that store's next crawl, so that a store's crawls never overlap.
- [x] **ACQ-CADENCE-002**: When a store begins monitoring, the system shall issue its baseline crawl without first waiting that store's configured interval.

# Store Configuration — Specs

Specs owned by `docs/intent/store-config/store-config-design.md` (prefix `CFG`).

The *stores file* is the CSV naming each monitored store and where its alerts
go. Its location comes from `MONITOR_WEBSITES_FILE`.

## Reading the stores file

- [x] **CFG-STORES-001**: The system shall locate the stores file's columns by their header names rather than by position.
- [x] **CFG-STORES-002**: If the stores file's header has no `url` column or no `webhook` column, then the system shall report which is missing and shall not start monitoring.
- [x] **CFG-STORES-003**: When an optional column is absent from the stores file, the system shall apply the corresponding global default to every store.
- [x] **CFG-STORES-004**: When an optional column is present but blank for a store, the system shall apply the corresponding global default to that store.

## Refusing a configuration that cannot work

- [x] **CFG-VALID-001**: If a store's `url` is blank, then the system shall report the file and line and shall not start monitoring.
- [x] **CFG-VALID-002**: If a store's `webhook` is blank, then the system shall report the file and line and shall not start monitoring.
- [x] **CFG-VALID-003**: If a store's `url` is not an absolute http or https URL, then the system shall report the file, line, and offending value and shall not start monitoring.
- [x] **CFG-VALID-004**: If a store's `webhook` is not an absolute http or https URL, then the system shall report the file, line, and offending value and shall not start monitoring.
- [x] **CFG-VALID-005**: If a store's `delay` is present and is not a positive whole number, then the system shall report the file, line, and offending value and shall not start monitoring.
- [x] **CFG-VALID-006**: If a store's `max_products` is present and is neither zero nor a positive whole number, then the system shall report the file, line, and offending value and shall not start monitoring.
- [x] **CFG-VALID-007**: If the stores file contains no store rows, then the system shall report that it has nothing to monitor and shall not start monitoring.
- [x] **CFG-VALID-008**: The system shall report a stores file line number counting the header as line 1, so that it matches what an editor shows.
- [x] **CFG-VALID-009**: The system shall not contact a store or a webhook in order to validate the stores file.

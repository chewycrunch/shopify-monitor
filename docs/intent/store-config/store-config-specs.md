# Store Configuration — Specs

Specs owned by `docs/intent/store-config/store-config-design.md` (prefix `CFG`).

The *stores file* is the TOML file naming each monitored store and where its
alerts go. Its location comes from `MONITOR_WEBSITES_FILE`. Each monitored store
is one `[[store]]` table in that file, carrying a `url`, a `webhooks` list, and
optionally a `delay` and a `max_products`. A *global default* is the value from
the environment that applies to a store which sets none of its own.

## Reading the stores file

- [ ] **CFG-STORES-003**: When a store in the stores file omits an optional field, the system shall apply the corresponding global default to that store.
- [ ] **CFG-STORES-005**: The system shall read the stores file as TOML, taking each `[[store]]` table in it as one monitored store.
- [ ] **CFG-STORES-006**: When the system reads a store from the stores file, it shall record the line on which that store's table begins, so that a later refusal of that store can name the line.
- [ ] **CFG-STORES-007**: If a store's table in the stores file carries a field the system does not recognise, then the system shall report that field with the file and line and shall not start monitoring.

## Refusing a configuration that cannot work

- [ ] **CFG-VALID-001**: If a store in the stores file has no `url`, or its `url` is empty, then the system shall report the file and line and shall not start monitoring.
- [ ] **CFG-VALID-002**: If a store in the stores file has no `webhooks` list, or that list is empty, then the system shall report the file and line and shall not start monitoring.
- [ ] **CFG-VALID-003**: If a store's `url` is not an absolute http or https URL, then the system shall report the file, line, and offending value and shall not start monitoring.
- [ ] **CFG-VALID-004**: If any entry in a store's `webhooks` list is not an absolute http or https URL, then the system shall report the file, line, and offending entry and shall not start monitoring.
- [ ] **CFG-VALID-005**: If a store's `delay` is present and is not a positive whole number of milliseconds, then the system shall report the file, line, and offending value and shall not start monitoring.
- [ ] **CFG-VALID-006**: If a store's `max_products` is present and is neither zero nor a positive whole number, then the system shall report the file, line, and offending value and shall not start monitoring.
- [ ] **CFG-VALID-007**: If the stores file contains no stores, then the system shall report that it has nothing to monitor and shall not start monitoring.
- [ ] **CFG-VALID-008**: The system shall report a stores file line number that matches the line an editor shows for that position in the file.
- [ ] **CFG-VALID-009**: The system shall not contact a store or a webhook in order to validate the stores file.
- [ ] **CFG-VALID-010**: Where a store sets `max_products` to zero, the system shall apply zero to that store rather than treating the field as unset and applying the global default.
- [ ] **CFG-VALID-011**: If the stores file is not well-formed TOML, then the system shall report the file and the position of the fault and shall not start monitoring.
- [ ] **CFG-VALID-012**: If the stores file's first non-blank, non-comment line is a comma-separated list of field names rather than TOML, then the system shall report that the file is in the superseded comma-separated format and shall not start monitoring.

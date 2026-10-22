# Store Configuration — Specs

Specs owned by `docs/intent/store-config/store-config-design.md` (prefix `CFG`).

The *stores file* is the TOML file naming each monitored store and where its
alerts go. Its location comes from `MONITOR_STORES_FILE`. Each monitored store
is one `[[store]]` table in that file, carrying a `url`, a `webhooks` list, and
optionally a `delay` and a `max_products`. A *global default* is the value from
the environment that applies to a store which sets none of its own.

## Reading the stores file

- [x] **CFG-STORES-003**: When a store in the stores file omits an optional field, the system shall apply the corresponding global default to that store.
- [x] **CFG-STORES-005**: The system shall read the stores file as TOML, taking each `[[store]]` table in it as one monitored store.
- [x] **CFG-STORES-006**: When the system reads a store from the stores file, it shall record the line on which that store's table begins, so that a later refusal of that store can name the line.
- [x] **CFG-STORES-007**: If the stores file carries a field or table name the system does not recognise, then the system shall report that name with the file and line and shall not start monitoring, rather than ignoring it.

## Refusing a configuration that cannot work

- [x] **CFG-VALID-001**: If a store in the stores file has no `url`, or its `url` is empty, then the system shall report the file and line and shall not start monitoring.
- [x] **CFG-VALID-002**: If a store in the stores file has no `webhooks` list, or that list is empty, then the system shall report the file and line and shall not start monitoring.
- [x] **CFG-VALID-003**: If a store's `url` is not an absolute http or https URL, then the system shall report the file, line, and offending value and shall not start monitoring.
- [x] **CFG-VALID-004**: If any entry in a store's `webhooks` list is not an absolute http or https URL, then the system shall report the file, the line the store's table begins on, and the offending entry, and shall not start monitoring — whether or not that store's other entries are usable.
- [x] **CFG-VALID-005**: If a store's `delay` is present and is not a positive whole number of milliseconds — including where it is present as some other type — then the system shall report the file, line, and offending value and shall not start monitoring.
- [x] **CFG-VALID-006**: If a store's `max_products` is present and is neither zero nor a positive whole number — including where it is present as some other type — then the system shall report the file, line, and offending value and shall not start monitoring.
- [x] **CFG-VALID-007**: If the stores file is well-formed and carries no unrecognised names yet contains no stores, then the system shall report that it has nothing to monitor and shall not start monitoring.
- [x] **CFG-VALID-008**: The system shall report a stores file line number that matches the line an editor shows for that position in the file.
- [x] **CFG-VALID-009**: The system shall not contact a store or a webhook in order to validate the stores file.
- [x] **CFG-VALID-010**: Where a store sets `max_products` to zero, the system shall apply zero to that store rather than treating the field as unset and applying the global default.
- [x] **CFG-VALID-011**: If the stores file cannot be parsed as TOML at all, then the system shall report the file and the position of the fault and shall not start monitoring.
- [x] **CFG-VALID-012**: If the stores file's first non-blank, non-comment line is a comma-separated list of field names rather than TOML, then the system shall report that the file is in the retired comma-separated format and shall not start monitoring, in preference to reporting it as a parse fault.

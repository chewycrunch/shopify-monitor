---
parent: high-level-design
prefix: CFG
---

# Store Configuration

## Context and Design Philosophy

This segment owns the operator's inputs: the stores file, the proxies file, and
the environment variables and flags that surround them.

Its guiding principle is that **a misconfiguration is discovered at startup, not
in production silence.** The monitor is a background process that nobody watches
once it is running. A store configured without a destination for its alerts
still polls, still diffs, still finds restocks, and reports them nowhere — and
looks healthy the entire time. That failure is indistinguishable from a quiet
market, so it can persist for as long as nobody thinks to check.

Configuration that cannot possibly work is therefore refused before the first
crawl, naming the file and line so the fix is obvious.

## Where settings come from

Settings are environment variables and flags, parsed together so each has both
forms. Bulk operator data — the stores, the proxies — lives in files, because it
is lists rather than settings.

Nothing reads a `.env` file inside the process. Every runner already parses one
(the task runner, Compose, systemd), so an in-process loader would duplicate
that and make behaviour depend on the working directory the process happened to
start in.

## The stores file

A CSV with a header row. Columns are located by name rather than position, so a
file may add optional columns or reorder them, and one written before an option
existed keeps working.

| Column | Required | Meaning |
| --- | --- | --- |
| `url` | yes | Store base URL, absolute, `http` or `https` |
| `webhook` | yes | Absolute URL to post alerts for this store to |
| `delay` | no | Milliseconds to rest between crawls |
| `max_products` | no | Newest products to crawl; `0` for the whole reachable catalogue |

An absent optional column, or a blank cell in one, means the global default. A
present but unparseable value is an error rather than a fallback: a delay of
`"fast"` is a mistake being made, not an intention to accept the default, and
silently substituting one would hide it.

### What is refused

- A header missing `url` or `webhook`.
- A row whose `url` or `webhook` is blank.
- A `url` or `webhook` that is not an absolute `http` or `https` URL.
- A `delay` that is not a positive integer.
- A `max_products` that is not zero or a positive integer.
- A file with no store rows at all.

Each is reported with the file and the line as an editor numbers it, and stops
startup. The last is included because a monitor with nothing to monitor
otherwise starts, finds no work, and exits successfully — which reads as a crash
to anyone watching a restart policy, and as silence to anyone watching Discord.

Validation is deliberately shallow. That a URL is well-formed is checkable
before any network call; that it points at a real store, or that a webhook is
still live, is not knowable without asking, and asking at startup would turn a
transient outage into a refusal to boot.

## The proxies file

One entry per line, `host:port` or `host:port:user:pass`. Blank lines and lines
beginning with `#` are ignored, so a file can be annotated and a trailing
newline is harmless. A malformed entry stops startup, naming the line.

The file is optional: absent, the monitor runs over direct connections. This is
correct for a first run and viable for small catalogues, and is reported once at
startup so the condition is visible without a line per request.

## Decisions & Alternatives

| Decision | Chosen | Alternatives Considered | Rationale |
| --- | --- | --- | --- |
| Column identification | By header name | By position | A positional file breaks whenever a column is added, and cannot express "this store has a delay but no product cap". |
| A blank optional cell | Falls back to the global default | Error | Blank is how a CSV expresses "not set for this row", and requiring every row to fill every column defeats having defaults. |
| An unparseable optional value | Error | Fall back to the default | A wrong value is a mistake in progress. Substituting the default hides it and produces behaviour the operator did not ask for. |
| A missing destination | Refuses to start | Warn and poll anyway | A store that detects restocks and reports them nowhere looks identical to a healthy one. |
| A stores file with no rows | Refuses to start | Start and idle; start and exit | With nothing to monitor the process exits successfully and immediately, which reads as a crash loop under a restart policy. |
| Validation depth | Shape only, no network | Verify the store and webhook respond | Reachability is not knowable without asking, and a transient outage at startup should not prevent booting. |

## Open Questions & Future Decisions

### Deferred

1. **Duplicate store URLs are not detected.** Two rows for one store produce two
   independent monitors with independent records, doubling that store's request
   load and its alerts.
2. **The stores file is read once at startup.** Adding or retuning a store means
   restarting the process, which discards every store's availability record and
   re-baselines all of them.
3. **A webhook's shape is not checked beyond being an absolute URL.** A URL that
   is well-formed but not a webhook fails per alert, at the point of sending,
   rather than at startup.

## References

- `docs/high-level-design.md` — configuration as environment plus data files.
- `docs/intent/catalog-acquisition/catalog-acquisition-design.md` — how the
  delay, product cap, and proxies are used.

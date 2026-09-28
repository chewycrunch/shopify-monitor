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

A setting that governs a resource shared between stores belongs in the
environment rather than beside a store, because two stores sharing that resource
could name conflicting values with no correct way to resolve them. A setting
that governs one store belongs in the stores file beside it.

## The stores file

TOML, one table per monitored store:

```toml
[[store]]
url = "https://kith.com"
webhooks = [
  "https://discord.com/api/webhooks/000000000000000000/aaaa",
  "https://discord.com/api/webhooks/111111111111111111/bbbb",
]
delay = 300000
max_products = 6000

# Paused for the season — kept rather than deleted.
# [[store]]
# url = "https://smallstore.com"
# webhooks = ["https://discord.com/api/webhooks/222222222222222222/cccc"]
```

| Field | Required | Meaning |
| --- | --- | --- |
| `url` | yes | Store base URL, absolute, `http` or `https` |
| `webhooks` | yes | One or more absolute URLs to post this store's alerts to |
| `delay` | no | Milliseconds to rest between crawls |
| `max_products` | no | Newest products to crawl; `0` for the whole reachable catalogue |

An omitted optional field means the global default. A present but unparseable
value is an error rather than a fallback: a delay of `"fast"` is a mistake being
made, not an intention to accept the default, and silently substituting one
would hide it.

### Why a structured format rather than a delimited one

`webhooks` is a list, and a list is the thing a row of columns cannot hold. A
delimited format can encode one — a separator inside a single cell — but that
encoding is a second grammar layered on the first, invisible to the parser and
unenforced. The moment a destination grows any attribute of its own, the
encoding has to grow with it.

Two properties follow from the format that the previous one could not offer:

**A store can be commented out.** Pausing a store is a routine act — a drop is
over, a store is misbehaving, a webhook is being rotated — and a format with no
comment syntax forces the operator to delete the row and keep it somewhere else.

**Nesting is explicit rather than positional.** There is no header row to keep
aligned with the rows beneath it, and no way for a value to land in the wrong
field by being in the wrong position.

### What is refused

- A file that is not well-formed.
- A store with no `url`, or with a `url` that is not an absolute `http` or
  `https` URL.
- A store whose `webhooks` list is absent or empty.
- A `webhooks` entry that is not an absolute `http` or `https` URL.
- A `delay` that is not a positive integer.
- A `max_products` that is not zero or a positive integer.
- A file with no stores at all.

Each is reported with the file and the line, and stops startup. The last is
included because a monitor with nothing to monitor otherwise starts, finds no
work, and exits successfully — which reads as a crash to anyone watching a
restart policy, and as silence to anyone watching Discord.

Validation here is deliberately shallow, and deliberately ignorant of what a
webhook URL addresses. That a URL is well-formed is checkable before any network
call; that it points at a real store is not knowable without asking, and asking
at startup would turn a transient outage into a refusal to boot. Whether a
well-formed URL is one this program can actually deliver to is a question for
the segment that delivers — it is refused at startup too, using the line number
this segment records against each store.

### Recognising the superseded format

A file whose first meaningful line is a comma-separated header is reported as
the format it is, rather than as a TOML syntax error. The stores file is the one
file an operator hand-edits, so the single most likely failure after this change
is pointing the monitor at the file they already had — and a parser complaining
about an unexpected character on line 1 does not tell them that.

## The proxies file

One entry per line, `host:port` or `host:port:user:pass`. Blank lines and lines
beginning with `#` are ignored, so a file can be annotated and a trailing
newline is harmless. A malformed entry stops startup, naming the line.

The file is optional: absent, the monitor runs over direct connections. This is
correct for a first run and viable for small catalogues, and is reported once at
startup so the condition is visible without a line per request.

It stays a plain list because it is one: a proxy carries no per-entry settings,
so there is nothing for a structured format to express.

## Decisions & Alternatives

| Decision | Chosen | Alternatives Considered | Rationale |
| --- | --- | --- | --- |
| Stores file format | TOML | CSV; YAML | A store now owns a list, which a row of columns cannot hold without inventing a separator the parser cannot see. TOML expresses it natively, admits comments so a store can be paused rather than deleted, and has no significant whitespace — which matters for the one file an operator edits by hand. YAML would do the same but silently reinterprets unquoted values. |
| Webhooks field | Always a list | A single string; a string accepting a separator; repeated single-webhook rows | One shape means one parse path and one set of rules. Accepting both a string and a list doubles the grammar to save four characters. Repeated rows for one store would create two monitors, two records, and double the crawl load. |
| Field names | Unchanged from the previous format | Renamed to suit the new one | An operator converting a file is already changing its shape; changing the vocabulary at the same time makes a mechanical edit into a re-reading. |
| Delay units | Milliseconds, as an integer | A duration string such as `"5m"` | The environment defaults are integer milliseconds, and one convention read two ways is worse than one that is merely terse. |
| An omitted optional field | Falls back to the global default | Error | Omission is how the format expresses "not set here", and requiring every store to state every field defeats having defaults. |
| An unparseable optional value | Error | Fall back to the default | A wrong value is a mistake in progress. Substituting the default hides it and produces behaviour the operator did not ask for. |
| A store with no destination | Refuses to start | Warn and poll anyway | A store that detects restocks and reports them nowhere looks identical to a healthy one. |
| A stores file with no stores | Refuses to start | Start and idle; start and exit | With nothing to monitor the process exits successfully and immediately, which reads as a crash loop under a restart policy. |
| Validation depth | Shape only, no network | Verify the store and webhook respond | Reachability is not knowable without asking, and a transient outage at startup should not prevent booting. |
| The superseded format | Detected and named | Parsed as well, for a transition period; left to fail as a syntax error | Supporting both indefinitely doubles the parse paths for a population of one operator. Failing with a syntax error tells that operator nothing about what actually happened. |

## Open Questions & Future Decisions

### Deferred

1. **Duplicate store URLs are not detected.** Two tables for one store produce
   two independent monitors with independent records, doubling that store's
   request load and its alerts.
2. **The stores file is read once at startup.** Adding or retuning a store means
   restarting the process, which discards every store's availability record and
   re-baselines all of them.
3. **A store's webhooks are not checked for duplicates here.** The same URL
   listed twice is harmless — delivery collapses it to one alert — so it is not
   worth refusing, but neither is it reported.

## References

- `docs/high-level-design.md` — configuration as environment plus data files,
  and the tenet placing per-store behaviour beside the store.
- `docs/intent/catalog-acquisition/catalog-acquisition-design.md` — how the
  delay, product cap, and proxies are used.
- `docs/intent/notification/notification-design.md` — what a webhook URL has to
  be for a destination to handle it, and why delivery settings are global rather
  than per store.

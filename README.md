# shopify-monitor

Concurrent Go monitor for Shopify stores. Polls `/products.json`, diffs variant state, and fires webhook alerts on new variants or restocks.

## Quick Start

```bash
cp config/example.websites.csv config/websites.csv   # then add your stores
go run main.go
```

Proxies are optional — without `config/proxies.txt` the monitor uses direct
connections. Run `go run main.go --help` for the full generated option list.

## Configuration

Settings come from environment variables or flags; the runtime data files live
in [config/](config/), which is gitignored apart from the `example.*` templates.

Settings a store can override for itself are named `MONITOR_DEFAULT_*`. They
resolve in three layers: the built-in value, then the environment, then that
store's own column in the stores file. Settings without the prefix apply to the
whole process. The former `MONITOR_DELAY` and `MONITOR_MAX_PRODUCTS` names are
refused at startup rather than ignored.

A gitignored `.env` beside the Taskfile is picked up by `task run` and by
`docker compose`. Nothing else reads it — a bare `go run main.go` ignores it, so
pass the variables inline or use `task run`. A real environment variable takes
precedence over `.env` either way.

| Env var                        | Flag                     | Default               | Description                                                                                                        |
| ------------------------------ | ------------------------ | --------------------- | ------------------------------------------------------------------------------------------------------------------ |
| `MONITOR_DEFAULT_DELAY`        | `--default-delay`        | `5000`                | Rest between crawls in ms, for stores that set no `delay` of their own                                             |
| `MONITOR_DEFAULT_MAX_PRODUCTS` | `--default-max-products` | `6000`                | Newest products crawled, for stores that set no `max_products` of their own; `0` for the whole reachable catalogue |
| `MONITOR_WEBSITES_FILE`        | `--websites-file`        | `config/websites.csv` | CSV of store URL and webhook URL pairs                                                                             |
| `MONITOR_PROXIES_FILE`         | `--proxies-file`         | `config/proxies.txt`  | One proxy per line; optional                                                                                       |
| `MONITOR_LOG_FORMAT`           | `--log-format`           | `text`                | `text` or `json`                                                                                                   |
| `MONITOR_LOG_LEVEL`            | `--log-level`            | `info`                | `debug`, `info`, `warn`, or `error`                                                                                |
| `MONITOR_PAGE_WORKERS`         | `--page-workers`         | `5`                   | Catalogue pages fetched at once, each through its own proxy                                                        |

Logs go to stderr. `text` is readable at a terminal and in `journalctl`; set
`json` where something parses the output, such as a container shipping to Loki
or ELK. Under systemd, `text` also avoids duplicating the timestamp and priority
journald already records.

At `info` the log carries only events worth reading — startup, `new variant`,
`restock`, and errors. The per-poll heartbeat is `debug`, because at a 2.5s delay
it would otherwise be tens of thousands of lines a day per store and drown the
events. Set `debug` when you need to confirm a store is being polled at all.

The two path defaults are relative on purpose. The image's `WORKDIR` is `/app`,
so mounting your config directory at `/app/config` makes them resolve inside a
container exactly as they do for `go run main.go` from the repo root — the same
default works in both places, with the env vars there for when you run the built
binary from elsewhere.

---

### config/websites.csv — Stores

```csv
url,webhook,delay,max_products
https://kith.com,https://discord.com/api/webhooks/YOUR_ID/YOUR_TOKEN,300000,6000
https://smallstore.com,https://discord.com/api/webhooks/YOUR_ID/YOUR_TOKEN,60000,200
```

| Column         | Description                                                                                                                   |
| -------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `url`          | Shopify store base URL — no trailing slash, no `/products.json`                                                               |
| `webhook`      | Discord webhook URL to receive alerts for that store                                                                          |
| `delay`        | Optional. Milliseconds to rest between crawls; falls back to `MONITOR_DEFAULT_DELAY`                                          |
| `max_products` | Optional. Newest products to crawl; `0` for the whole reachable catalogue, blank falls back to `MONITOR_DEFAULT_MAX_PRODUCTS` |

Columns are matched by header name, so order does not matter and the optional
ones can be left out entirely. Add one row per store; each runs in its own
goroutine.

The file is validated at startup and a problem is fatal, naming the line as your
editor numbers it. `url` and `webhook` must both be present and absolute `http`
or `https` URLs, `delay` must be a positive whole number, `max_products` zero or
positive, and the file must contain at least one store. Nothing is contacted to
check it — a store or webhook being briefly down will not stop the monitor
starting. A store with a blank `webhook` is refused rather than run, because one
that finds restocks and reports them nowhere looks exactly like a healthy one.

### Choosing `max_products`

Products are returned newest-published first, and a stock change does not move a
product, so crawl depth decides which products can be seen restocking. Live
inventory sits at the front: on a large store the first few thousand products
are mostly in stock, while past roughly 7,500 the catalogue is years old and
effectively sold out. Crawling deeper costs bandwidth and makes a clean crawl
less likely without adding much that can change.

Set it to a store's whole catalogue when that is small — a store with 200
products crawls in a single request — and to the depth where a large store's
inventory stops being live otherwise. The default of 6,000 is about three months
of a busy store.

`0` disables the cap and crawls until the catalogue ends. Note that Shopify
itself refuses to paginate past 25,000 products, so on a larger store `0` reaches
that wall rather than the true end.

---

### config/proxies.txt — Proxies

```text
# blank lines and #-comments are ignored
host:port
host:port:username:password
```

One proxy per line, with optional basic auth. The monitor round-robins through
all entries and falls back to a direct connection if the file is empty or
missing. A malformed line is a startup error naming the line number.

---

## Docker (optional)

The monitor is a single static binary with no runtime dependencies — `go build`
and running it under systemd or a terminal multiplexer is a perfectly good
deployment. A prebuilt image is published if you'd rather not compile:

```bash
docker run -d --restart unless-stopped \
  -v ./config:/app/config:ro \
  --log-opt max-size=10m --log-opt max-file=3 \
  ghcr.io/chewycrunch/shopify-monitor:latest
```

Or, if the host already uses Compose, drop this beside your `config/` directory:

```yaml
services:
  monitor:
    image: ghcr.io/chewycrunch/shopify-monitor:latest
    volumes:
      - ./config:/app/config:ro
    env_file:
      - path: .env
        required: false
    restart: unless-stopped
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
```

Three things worth knowing either way:

- **The mount target must be `/app/config`.** That is the image's `WORKDIR`, and
  it is what makes the relative path defaults resolve in a container exactly as
  they do locally. Mount somewhere else and you must set `MONITOR_WEBSITES_FILE`
  and `MONITOR_PROXIES_FILE` to match.
- **Create `config/` before the first run.** If it does not exist, Docker
  silently creates it empty rather than failing, and the monitor exits with
  `open config/websites.csv: no such file or directory`.
- **Cap the logs.** Output is one JSON object per line on stderr, forever; an
  uncapped long-running poller will eventually fill the host disk.

The bind mount is read-only because the monitor never writes to these files, and
it keeps webhook tokens and proxy credentials out of the image.

---

## Project Structure

```text
main.go             Entry point — loads config, starts per-store monitor goroutines
config/             Runtime data files (gitignored except example.*)
internal/config/    Config struct and env/flag parsing
internal/monitor/   Core polling logic — fetches /products.json and diffs variant state
internal/proxy/     ProxyManager — reads proxies.txt and rotates entries per request
internal/webhook/   Webhook sender — formats and POSTs embeds on new variant or restock
internal/utils/     Shared types mirroring the Shopify products API response
```

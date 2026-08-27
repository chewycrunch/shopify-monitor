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

| Env var                 | Flag              | Default              | Description                                          |
| ----------------------- | ----------------- | -------------------- | ---------------------------------------------------- |
| `MONITOR_DELAY`         | `--delay`         | `2500`               | Pause between polling cycles in ms. Recommended: 2000–5000 |
| `MONITOR_WEBSITES_FILE` | `--websites-file` | `config/websites.csv` | CSV of store URL and webhook URL pairs               |
| `MONITOR_PROXIES_FILE`  | `--proxies-file`  | `config/proxies.txt`  | One proxy per line; optional                         |

The two path defaults are relative on purpose. The image's `WORKDIR` is `/app`,
so mounting your config directory at `/app/config` makes them resolve inside a
container exactly as they do for `go run main.go` from the repo root — the same
default works in both places, with the env vars there for when you run the built
binary from elsewhere.

---

### config/websites.csv — Stores

```csv
url,webhook
https://kith.com,https://discord.com/api/webhooks/YOUR_ID/YOUR_TOKEN
```

| Column    | Description                                                          |
| --------- | -------------------------------------------------------------------- |
| `url`     | Shopify store base URL — no trailing slash, no `/products.json`      |
| `webhook` | Discord webhook URL to receive alerts for that store      |

Add one row per store. Each store runs in its own goroutine.

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

## Docker

```yaml
services:
  monitor:
    image: ghcr.io/chewycrunch/shopify-monitor:latest
    volumes:
      - ./config:/app/config:ro
    env_file: .env
    restart: unless-stopped
```

The bind mount keeps webhook tokens and proxy credentials out of the image. If
`./config` does not exist on the host, Docker silently creates it empty rather
than failing, and the monitor exits with `open config/websites.csv: no such file
or directory` — create your config files before the first `up`.

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

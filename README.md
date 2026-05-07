# shopify-monitor

Concurrent Go monitor for Shopify stores. Polls `/products.json`, diffs variant state, and fires webhook alerts on new variants or restocks.

## Quick Start

```bash
go run ./cmd/main.go
```

## Configuration

All config lives in [config/](config/). Three files are required.

### config/config.json — Timing

```json
{
  "delay": 2500
}
```

| Field   | Type     | Description                                           |
| ------- | -------- | ----------------------------------------------------- |
| `delay` | int (ms) | Pause between polling cycles. Recommended: 2000–5000  |

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
host:port
host:port:username:password
```

One proxy per line, with optional basic auth. The monitor round-robins through all entries. Falls back to a direct connection if the file is empty or missing.

---

## Project Structure

```text
cmd/        Entry point — loads config, starts per-store monitor goroutines
config/     Runtime config files (not committed)
monitor/    Core polling logic — fetches /products.json and diffs variant state
proxy/      ProxyManager — reads proxies.txt and rotates entries per request
webhook/    Webhook sender — formats and POSTs embeds on new variant or restock
utils/      Shared types mirroring the Shopify products API response
```

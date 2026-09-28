---
parent: high-level-design
prefix: NOTIFY
---

# Notification

## Context and Design Philosophy

This segment owns one question: a change has been detected — how does it reach
the operator?

It consumes events from `change-detection` and is the last stage of the
pipeline. Nothing downstream depends on it, which means its failures are
survivable and its design can favour the monitor's continued operation over its
own completeness.

Three ideas govern everything below.

**Delivery must never pace detection.** The poll loop and the delivery path run
on different clocks, and the delivery clock belongs to the destination. A store
staging a drop can produce hundreds of events in one cycle; a webhook endpoint
will accept them at a few per second. Delivering those inline would stop the
crawl for minutes and convert a busy store into a blind one — the precise
failure this project exists to prevent.

**A destination is recognised, not declared.** The operator supplies a URL. What
kind of destination it is follows from the URL itself, so adding a second kind
never asks the operator to restate something the URL already says.

**The delivery unit is the webhook, not the store.** Every limit that constrains
this segment is imposed per webhook. Two stores pointing at one webhook are one
consumer of that budget, and a design that models them as two will exceed it.

## The Destination Seam

A destination is anywhere an event can be delivered.

```go
// A Destination delivers an event to somewhere an operator watches.
type Destination interface {
    Send(ctx context.Context, e utils.Event) error
    Name() string
}
```

`Send` takes the event rather than exposing a method per event kind. A method
per kind spreads the cost of a new kind across every implementation; a switch
inside one destination's formatter keeps it in a single place.

**`Send` enqueues; it does not deliver.** Its error reports that the event was
not accepted — the queue is full, or the destination is disabled or shut down. A
delivery that is accepted and later fails is logged by the destination, not
returned to the caller, because by then the caller has moved on to the next
crawl. This is the one surprising thing about the interface and it is
deliberate: a caller that could act on a delivery error would have to wait for
delivery, which is the coupling this segment exists to avoid.

`Name` identifies the destination in logs. It names the kind and enough of the
URL to tell two destinations apart, and never the credential the URL carries — a
webhook URL's token grants the right to post, so a log line quoting one in full
hands it to anyone who can read the logs.

## Recognising a Destination

```go
func (r *Registry) For(rawURL string) (Destination, error)
```

Each destination kind declares the URLs it handles. A URL matching no known kind
is an error raised at startup rather than when the first event fires. An
operator who mistypes a webhook host learns before the monitor begins polling,
rather than during the drop it was deployed for.

Recognition happens as stores become monitors, after the stores file has been
parsed, so this segment needs nothing from the file's format. Each store carries
the line it was read from, so the refusal still names the row an operator has to
edit — which is the property that makes a startup refusal actionable rather than
merely early.

Recognition by URL means the stores file carries no destination-type field.
There is nothing for an operator to set inconsistently with the URL beside it,
and a second destination kind is additive.

## One Destination Per Webhook

The registry caches by URL: two calls to `For` with the same URL return the same
`Destination`. Each unique webhook URL therefore has exactly one instance,
holding one queue, one sender goroutine, and one rate-limit state, shared by
every store that names it.

This is what makes the per-webhook budget correct. Independent instances per
store would each pace themselves correctly and collectively exceed the limit,
and the resulting rejections would be indistinguishable from the store's own.

It also settles where delivery settings live: because one destination serves
many stores, anything governing how it paces or retries is global, not
per-store. Two stores naming one webhook with conflicting values would have no
correct resolution.

The registry is explicit rather than a package-level singleton so that its
goroutines have an owner and a shutdown, and so tests can construct one without
touching global state.

## Fan-out

A store may name several webhooks. Fan-out is composition rather than a special
case:

```go
func Fanout(ds ...Destination) (Destination, error)
```

The composite is itself a `Destination`, so a store holds one whatever its
configuration says, and nothing upstream distinguishes one webhook from four.
Composing exactly one destination yields that destination rather than a wrapper.
Composing none is an error: a store with nowhere to report is refused when the
stores file is read, so reaching this constructor without a destination is a
defect rather than a configuration a caller should be handed a working object
for.

A failure to one destination does not prevent delivery to the others; the errors
are joined and returned together. Because retry lives inside each destination,
the composite needs no retry of its own — and must not have one, since retrying
the composite would re-deliver to the destinations that already succeeded.

`Fanout` discards duplicate destinations. Two identical URLs resolve through the
registry to the same instance, so duplicates are detectable by identity, and an
operator who lists a webhook twice gets one alert rather than two.

## Delivery

Each destination runs one sender goroutine draining a buffered queue.

**Pacing.** The endpoint's own accounting governs, and a local estimate is only
the fallback.

A destination that reports how many further requests it will accept, and how
long until that allowance renews, is paced by those figures: requests go out as
fast as the queue supplies them until the allowance is spent, then wait for it to
renew. Nothing is guessed, so the sender neither idles below the limit nor
discovers it by exceeding it.

A destination that refuses a delivery and names a wait interval is obeyed over
any allowance it reported earlier. A refusal is later information than the
figures that preceded it, and disregarding a stated interval is what escalates a
per-webhook limit into a host-wide block affecting every destination the monitor
sends to.

A configured minimum interval between requests covers the case where a
destination reports no allowance at all — the reporting is documented as
optional, so a sender that only works when it is present would fail silently and
intermittently. It defaults to zero, because a destination that does report its
allowance needs no local guess, and a non-zero value would be latency bought for
nothing.

**Retry.** Failures divide by whether trying again could work:

| Response | Treatment |
| --- | --- |
| Rate limited | Wait the interval the endpoint names, then retry. Not a failure. |
| `5xx`, network error, timeout | Retry a bounded number of times with exponential backoff. |
| `404`, `401`, `403` | The webhook has been deleted or revoked. Retrying cannot succeed. |
| Other `4xx` | The request itself is unacceptable. Retry cannot change it, so the event is dropped and the next event is unaffected. |

**Disabling a destination.** A destination tracks consecutive delivery failures;
any success resets the count. Crossing a configured threshold disables it for
the life of the process: its queue is discarded, further events are refused at
`Send`, and the fact is reported once at error level.

A threshold rather than an immediate stop, because the statuses that look
permanent are not always — an edge network can answer `403` for a minute — and
because a single counter covering both kinds of failure is one rule to
understand rather than two. Disabled until restart rather than periodically
retried, because a re-created webhook has a different URL, so recovery requires
an operator to edit the stores file, which requires a restart anyway.

The alternative is worse in a specific way: a destination retrying a dead
webhook forever keeps its queue permanently full, so one bad row in the stores
file silences the good rows behind it.

**Overflow.** A full queue drops its oldest entry to admit the new one, and the
count of dropped events is reported. The freshest event is the one worth
keeping: a restock alert delivered ten minutes late has already been acted on by
everyone it would have helped. Blocking instead would push back into the poll
loop, which is the coupling this segment refuses.

Because overflow drops from the middle of a stream and destinations fail
independently, delivery order is best-effort. Nothing downstream of an alert
depends on the order alerts arrive in.

**Shutdown.** On a graceful shutdown, senders stop accepting new events and
drain what they hold, bounded by a deadline; whatever remains at the deadline is
dropped and counted. Draining matters because the events in a queue at shutdown
are the ones detected most recently.

## What an Alert Carries

The segment specifies the information an alert conveys. How that information is
rendered belongs to the destination, because the rendering vocabulary is the
vendor's — one service's colour integer is another's markup block.

An alert identifies:

- the store the event came from,
- the product, by title and by a link to its page on that store,
- the variant, by title and identifier,
- whether the variant is newly listed or has restocked,
- when the change was detected,
- and the product's image, where the catalogue lists one.

The event carries its own detection time and its own store. A destination shared
by several stores has no single store to infer, and a queued event may be
delivered minutes after it was detected, so a delivery-time clock would
misreport when the change happened.

## The Discord Destination

Recognised by host — `discord.com` or `discordapp.com` — with an
`/api/webhooks/` path. Named in logs as the kind plus the webhook's id segment,
excluding the token.

One embed per event. The product title is the embed title and links to
`{store}/products/{handle}`; variant title, variant identifier, and store are
fields; the product's first image, when it has one, is the thumbnail; the
detection time is the embed timestamp. Embed colour distinguishes a restock from
a newly listed variant, so the kind reads before the text does.

Discord bounds embed titles, field values, and total embed size, and answers an
overrun with a rejection this segment treats as permanent. Text is therefore
truncated to fit rather than sent at full length: an alert naming a product by
an abbreviated title is useful, and a rejected alert is not.

Discord rate limits per webhook. It reports the remaining allowance and the time
until it renews on every response, including accepted ones, and names a wait
interval when it refuses a delivery for rate. The allowance figures are
documented as optional, so their absence is expected rather than exceptional.

## Decisions & Alternatives

| Decision | Chosen | Alternatives Considered | Rationale |
| --- | --- | --- | --- |
| Delivery timing | Asynchronous, queued per destination | Deliver inline in the poll loop | A webhook endpoint accepts a few requests per second. A cycle producing hundreds of events would stall the crawl for minutes, so a busy store — the one worth watching — would be the one that stops being watched. |
| Interface shape | One `Send` taking the event | A method per event kind | A method per kind makes every new event kind a change to every destination. A switch in one formatter confines it. |
| `Send` semantics | Returns acceptance, not delivery | Return the delivery result | Returning the delivery result requires the caller to wait for delivery, which reintroduces the coupling asynchrony removes. |
| Destination identity | Recognised from the URL | A `type` field in the stores file | The URL already identifies the vendor unambiguously. A separate field can disagree with it, and only ever restates it. |
| Instance scope | One per unique webhook URL, process-wide | One per store | Every relevant limit is per webhook. Per-store instances each pace correctly and collectively exceed the budget. |
| Delivery settings scope | Global | Per store, in the stores file | A destination is shared across the stores that name it, so per-store values for one webhook can conflict with no correct resolution. |
| Registry | Explicit, owned by the caller | Package-level singleton built in `init()` | Sender goroutines need an owner and a shutdown, and a global cannot be constructed twice for a test. |
| Fan-out | A `Destination` composing destinations | Storing a list per monitor and looping at the call site | Composition keeps the caller ignorant of how many webhooks a store has, so nothing upstream changes when that count does. |
| Retry placement | Inside each destination | Above the interface, around `Send` | Retry rules are vendor-specific. Above the interface, retrying a fan-out would re-deliver to destinations that already succeeded. |
| Reported wait intervals | Always obeyed, over any earlier allowance | Configurable alongside local pacing | Exceeding a stated interval does not deliver sooner — the request is refused either way — and sustained disregard escalates from a per-webhook limit to a host-wide block affecting every destination. A refusal is also later information than the allowance that preceded it. |
| Pacing source | The destination's reported allowance, with a configured interval as fallback | A fixed configured interval; no pacing until refused | A reported allowance is exact, so pacing by it neither idles below the limit nor finds it by exceeding it. A fixed interval is a guess that costs latency when too slow and refusals when too fast. Sending until refused wastes the requests it spends learning, and the refusals themselves carry the escalation risk. |
| Fallback interval default | Zero | A conservative non-zero interval | It applies only where a destination reports no allowance. Where one is reported it is redundant, and a non-zero default would delay every delivery to buy nothing. |
| Failing destination | Disabled until restart after consecutive failures cross a threshold | Immediate stop on a permanent status; retry indefinitely | Statuses that look permanent are sometimes transient, and one counter is one rule rather than two. Indefinite retry keeps a queue permanently full, so one bad row silences the healthy rows behind it. Recovery needs a new URL and therefore a restart regardless. |
| Queue overflow | Drop oldest, report the count | Block the producer; drop newest; unbounded queue | A stale restock alert has no value, so the newest event is the one worth the slot. Blocking paces detection by delivery; an unbounded queue converts an unreachable webhook into unbounded memory. |
| Shutdown | Drain within a deadline, then drop | Drop immediately; drain without bound | Events held at shutdown are the most recently detected. An unbounded drain lets an unresponsive endpoint prevent the process from exiting. |
| Presentation | Owned by each destination | Specified once in this segment | Rendering vocabulary is vendor-specific; a colour integer means nothing to a destination that has no colours. The segment fixes what an alert conveys, not how it looks. |
| Alert timestamp | Detection time, carried on the event | Time of delivery | A queued event may be delivered minutes after detection, and the operator is acting on when the stock changed. |

## Open Questions & Future Decisions

### Deferred

1. **Events are not batched.** Discord accepts several embeds in one message, so
   batching would raise effective throughput substantially and is the obvious
   lever if a drop ever outruns the queue. Not built: it complicates retry,
   since a partially-rejected batch has no per-embed outcome, and the
   single-event path has to exist regardless.
2. **Price is not shown.** `catalog-acquisition` does not read variant price —
   see `ACQ-PAGE-005` — so an alert cannot state one without widening what a
   crawl parses. Worth doing if operators find alerts under-informative; it is a
   cascade into a sibling segment rather than a change here.
3. **No suppression of repeated alerts.** A variant flapping between available
   and unavailable produces an alert on every transition into available. This is
   `change-detection`'s classification, not this segment's delivery, but the
   operator experiences it here.
4. **Only fan-out, not routing.** Every event for a store goes to every webhook
   that store names. There is no per-product, per-variant, or per-event-kind
   selection, per the root design's non-goals.
5. **Delivery is not observable beyond logs.** There is no counter of events
   delivered, dropped, retried, or refused by a disabled destination that an
   operator can read without reading log lines.
6. **A disabled destination cannot be re-enabled without a restart**, including
   in the case where the operator fixes the cause and the URL is unchanged.

7. **A destination shared by several stores serves them first-come,
   first-served.** Stores poll on their own intervals, so a store polling every
   few seconds can place its events ahead of one polling every few minutes on
   the queue they share. Both reach the same channel, so the operator sees one
   stream either way; no fairness is arbitrated between them.
8. **Pacing cannot see a limit imposed below the webhook.** Discord also
   constrains a channel, and several webhooks can address one channel. Each
   would pace correctly against its own allowance and collectively exceed the
   channel's. The channel is not derivable from a webhook URL, so this is
   undetectable here rather than unhandled — it surfaces as refusals the retry
   path already absorbs.
9. **Whether webhook execution reports a request allowance is unconfirmed.**
   The allowance figures are documented for the API generally and as optional,
   without webhook execution being named either way. The fallback interval
   covers their absence, so the design is correct regardless, but which path is
   actually taken is worth observing against a live webhook rather than assumed.

## References

- `docs/high-level-design.md` — the exactly-one-notification success metric, and
  the non-goal bounding this segment to fan-out rather than routing.
- `docs/intent/change-detection/change-detection-design.md` — the producer of
  the events delivered here, and the source of an event's detection time.
- `docs/intent/store-config/store-config-design.md` — where an operator names a
  store's webhooks, and where an empty or unrecognised destination URL is
  refused.

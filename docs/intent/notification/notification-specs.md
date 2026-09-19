# Notification — Specs

Specs owned by `docs/intent/notification/notification-design.md` (prefix
`NOTIFY`).

An *event* is a reportable change produced by `change-detection` — a variant
newly listed as available, or a variant that has restocked. A *destination* is
somewhere an event can be delivered, named by the operator as a webhook URL in
the stores file; Discord is the only kind currently recognised. An *alert* is
one event rendered for one destination. The system *accepts* an event when it
takes responsibility for delivering it, which happens before delivery is
attempted.

## Recognising and composing destinations

- [x] **NOTIFY-DEST-001**: The system shall determine a destination's kind from its webhook URL, without the operator declaring that kind separately.
- [x] **NOTIFY-DEST-002**: If a webhook URL in the stores file matches no recognised destination kind, then the system shall report the offending URL and shall not start monitoring.
- [x] **NOTIFY-DEST-003**: When more than one store names the same webhook URL, the system shall deliver those stores' events through a single shared destination holding one queue and one rate-limit state.
- [x] **NOTIFY-DEST-004**: The system shall identify a destination in log output by its kind together with enough of its URL to distinguish it from other destinations, and shall never write the credential the URL carries.
- [x] **NOTIFY-DEST-005**: When a store names several webhook URLs, the system shall deliver every one of that store's events to every one of those URLs.
- [x] **NOTIFY-DEST-006**: When a store names the same webhook URL more than once, the system shall deliver one alert per event to that URL rather than one per mention.
- [x] **NOTIFY-DEST-007**: If delivery of an event to one of a store's destinations fails, then the system shall still attempt delivery of that event to that store's other destinations.
- [x] **NOTIFY-DEST-008**: When an event has been accepted by some of a store's destinations and refused by others, the system shall retry only the destinations that refused it.

## Accepting events for delivery

- [x] **NOTIFY-QUEUE-001**: When an event is reported for delivery, the system shall return from that report without waiting for the event to be delivered.
- [x] **NOTIFY-QUEUE-002**: The system shall hold no more than 256 accepted-but-undelivered events per destination.
- [x] **NOTIFY-QUEUE-003**: When an event is reported to a destination already holding its maximum undelivered events, the system shall discard that destination's longest-waiting undelivered event in order to accept the newly reported one.
- [x] **NOTIFY-QUEUE-004**: When the system discards an undelivered event because a destination's queue is full, it shall report the destination and the number of events discarded.
- [x] **NOTIFY-QUEUE-005**: While a store's events are awaiting delivery, the system shall continue crawling that store's catalogue on its configured interval.
- [x] **NOTIFY-QUEUE-006**: The system shall deliver a destination's events in the order that destination accepted them.

## Pacing and retrying delivery

- [x] **NOTIFY-DELIVER-001**: When a destination refuses a delivery and names an interval to wait before retrying, the system shall wait at least that interval before its next request to that destination.
- [x] **NOTIFY-DELIVER-002**: The system shall wait at least the configured minimum interval between consecutive requests to the same destination.
- [x] **NOTIFY-DELIVER-003**: The system shall apply one configured minimum interval to every destination, rather than a value configured per store.
- [x] **NOTIFY-DELIVER-004**: Where no minimum interval is configured, the system shall apply a minimum interval of zero milliseconds between consecutive requests to a destination.
- [x] **NOTIFY-DELIVER-005**: Where the configured minimum interval is zero, the system shall issue requests to a destination as fast as undelivered events are available, subject only to the request budget and wait intervals that destination reports.
- [x] **NOTIFY-DELIVER-006**: If a delivery fails with a server error, a network error, or a timeout, then the system shall retry that delivery up to 3 further times, waiting 1 second before the first retry and doubling the wait before each subsequent one.
- [x] **NOTIFY-DELIVER-007**: If a destination refuses a delivery for a reason that retrying cannot change, and the destination remains usable, then the system shall discard that event and continue delivering the destination's remaining events.
- [x] **NOTIFY-DELIVER-008**: When a destination refuses a delivery for exceeding its rate limit, the system shall not count that refusal as a delivery failure.
- [x] **NOTIFY-DELIVER-009**: When a destination reports how many further requests it will accept and how long until that allowance renews, the system shall issue no further request to that destination once the reported allowance is exhausted, until the reported renewal time has passed.
- [x] **NOTIFY-DELIVER-010**: If a destination reports no request allowance with a delivery, then the system shall pace its next request to that destination by the configured minimum interval alone.

## Withdrawing from a failing destination

- [x] **NOTIFY-FAIL-001**: The system shall count consecutive delivery failures separately for each destination, and shall reset a destination's count when a delivery to it succeeds.
- [x] **NOTIFY-FAIL-002**: When a destination's consecutive delivery failures reach 10, the system shall disable that destination for the remainder of the process run.
- [x] **NOTIFY-FAIL-003**: When the system disables a destination, it shall report that destination and the reason once, at error level.
- [x] **NOTIFY-FAIL-004**: When the system disables a destination, it shall discard that destination's undelivered events.
- [x] **NOTIFY-FAIL-005**: While a destination is disabled, the system shall refuse events reported to it rather than holding them for later delivery.
- [x] **NOTIFY-FAIL-006**: While one of a store's destinations is disabled, the system shall continue delivering that store's events to its remaining destinations.
- [x] **NOTIFY-FAIL-007**: While a destination is disabled, the system shall continue crawling and detecting changes for every store that names it.

## What an alert conveys

These apply to an alert for any destination kind.

- [x] **NOTIFY-ALERT-001**: Every alert shall identify the store whose catalogue the event was detected in.
- [x] **NOTIFY-ALERT-002**: Every alert shall identify the product by its title and shall link to that product's page on the store the event came from.
- [x] **NOTIFY-ALERT-003**: Every alert shall identify the variant by its title and by its variant identifier.
- [x] **NOTIFY-ALERT-004**: Every alert shall distinguish a newly listed available variant from a variant that has restocked.
- [x] **NOTIFY-ALERT-005**: Every alert shall state the time the change was detected rather than the time the alert was delivered.
- [x] **NOTIFY-ALERT-006**: Where the product's catalogue entry lists at least one image, every alert shall carry the first of those images.
- [x] **NOTIFY-ALERT-007**: Where the product's catalogue entry lists no images, the system shall produce the alert without an image rather than withholding the alert.

## Delivering to Discord

- [x] **NOTIFY-DISCORD-001**: The system shall recognise a webhook URL whose host is `discord.com` or `discordapp.com` and whose path contains `/api/webhooks/` as a Discord destination.
- [x] **NOTIFY-DISCORD-002**: When delivering an event to a Discord destination, the system shall send one embed describing that event.
- [x] **NOTIFY-DISCORD-003**: When delivering an event to a Discord destination, the system shall set the embed's colour according to whether the variant is newly listed or restocked.
- [x] **NOTIFY-DISCORD-004**: If a value in a Discord embed would exceed the length Discord accepts for that value, then the system shall shorten it to fit rather than send the alert at full length.
- [x] **NOTIFY-DISCORD-005**: The system shall name a Discord destination in log output by its webhook identifier, excluding the webhook token.
- [x] **NOTIFY-DISCORD-006**: When Discord responds to a delivery, whether it accepts or refuses it, the system shall read the remaining request allowance and the time until that allowance renews from the response.
- [x] **NOTIFY-DISCORD-007**: If Discord refuses a delivery because the webhook does not exist or the request is not authorised, then the system shall count that refusal as a delivery failure for that destination.
- [x] **NOTIFY-DISCORD-008**: When Discord refuses a delivery for exceeding a rate limit, the system shall read the wait interval Discord names in that refusal in preference to any allowance it reported earlier.

## Shutting down

- [x] **NOTIFY-LIFE-001**: When the process begins a graceful shutdown, the system shall stop accepting newly reported events.
- [x] **NOTIFY-LIFE-002**: When the process begins a graceful shutdown, the system shall continue delivering already-accepted events for up to 5 seconds.
- [x] **NOTIFY-LIFE-003**: If undelivered events remain when the graceful shutdown period ends, then the system shall discard them and report how many were discarded.

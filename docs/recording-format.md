# Recording format (schema version 1)

A run is a folder, `runs/<UTC time>-s<seed>/`. The rules for changing this format are in [ADR 0007](adr/0007-event-schema.md). Times are integer Unix microseconds. A value that doesn't apply is `null`.

## `schedule.json`
Saved before the storm: `version`, `params` (`seed`, `rate_per_second`, `duration_us`, `bidders`, `retry_rate`, `conflict_rate`) and `entries`.

| Entry field | Meaning |
|---|---|
| `seq` | Position in the schedule; joins to events |
| `offset_us` | Intended send, from the start of the storm |
| `bidder` | Bidder number, from 0 (login `storm-<n>`) |
| `steps` | Minimum increments to bid above the lowest acceptable amount |
| `retry_of` | `seq` of the earlier bid whose idempotency key this reuses |
| `change_amount` | A retry that raises the amount, which the target should refuse |

Arrivals are Poisson at a constant mean rate. Amounts are decided at send time.

## `events.jsonl`
One JSON object per line, in the order exchanges **finished**. Sort by a time or `seq` field. Every line has `v` and `type`.

**`bid`**: one `POST` of a bid.

| Field | Meaning |
|---|---|
| `seq`, `bidder`, `retry_of` | As in the schedule |
| `intended_us` | When the schedule called for the send. Latency counts from here |
| `idempotency_key` | Auction id prefix and the `seq` of the key's first use |
| `amount` | Cents offered |
| `min_next_seen` | The lowest acceptable amount the bidder had seen |
| `price_age_us` | How old that reading was at the intended send (0 if it arrived later). It is the original's for a retry, and grows after the close |
| exchange fields | Below |

**`poll`**: one `GET` of a bid answered as pending. `seq` of that bid, `n` from 1, and the exchange fields. `bid_id` is the bid asked about.

**`skipped`**: a send never made. `seq`, `bidder`, `intended_us`, `at_us`, `why` (`in_flight_cap`).

**Exchange fields**

| Field | Meaning |
|---|---|
| `sent_us` | Just before the request was handed to the HTTP client, so connection setup counts as latency |
| `replied_us` | After the reply was read, or the failure |
| `http_status` | `null` if no response arrived |
| `instance` | Which target instance answered (Holdfast: `x-holdfast-instance`) |
| `bid_id`, `bid_status`, `reason` | From the reply's bid |
| `error_code` | The `error` field of an error reply |
| `transport_error` | `timeout`, `canceled`, `connection_refused`, `connection_reset`, `connection_closed`, `bad_response` or `other` |

`change_amount` in the schedule is what was intended, not what must happen. Under load a retry can reach the target before its original, or its original can have been skipped. The retry then takes the key (`201`), and whichever send with a different amount arrives later gets the `422`.

A `201` can carry a `rejected` or `lost_race` bid. A retried key returns the original bid as it is now, so count distinct `bid_id`s, not lines.

## `manifest.json`
Written when the run starts and again when it ends.

| Field | Meaning |
|---|---|
| `schema_version`, `run_id`, `started_us`, `ended_us` | |
| `complete` | The whole schedule ran and the events were saved |
| `valid`, `invalid_reasons` | Whether the generator kept up (spec §5). Not a verdict on the target |
| `schedule`, `schedule_from` | The schedule's `params`; `generated` or `file` |
| `settings` | Auction length, lag limit, in-flight cap, price, poll and timeout intervals |
| `target` | Name, URL, auction id, checkout commit and whether it was dirty, declared fake settings, instances seen |
| `machine` | OS, architecture, CPU count |
| `generator` | Go version, source revision and whether it was dirty |
| `results` | Counts and send lag, for a quick look; the events are the record. `bids_by_last_status` is what replies last showed, not final state: most outbid bids are never read again |

`invalid_reasons` on a recording whose generator was killed reads "the run did not finish".

## Not recorded
Cookies, session tokens, headers, raw bodies, error message text, hostnames, user names and file paths. The price reader's requests (counted only if they fail) and sign-ins. The close and the order of accepted bids arrive with the stream subscribers (slice 1b).

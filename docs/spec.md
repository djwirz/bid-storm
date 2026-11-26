# bid-storm: spec

## 1. Goal
Record a bid storm in enough detail to show that many bids land at once, they get one agreed order, and none counts without a hold. Publish the recordings as a data-deep static site.

## 2. Pieces
```
generator (Go) ──HTTP──▶ target (Holdfast) ──▶ recording proxy (Go) ──▶ target's Stripe fake
     ▲  └─ stream subscribers ◀──SSE──┘                 │
     └────────────── events (JSON lines) ◀──────────────┘
                          │
                 analysis (Python, DuckDB) ──▶ Parquet ──▶ site (TypeScript, D3, DuckDB-WASM)
```
A small per-target adapter covers what the contract lacks: creating an auction, and running the target's own checker. Holdfast has no endpoint for creating auctions, so its adapter runs Holdfast's own `seed-e2e.ts` from a local checkout, as `load/run.sh` does. That fixes the starting price and increment.

## 3. Recorded data
- **Generator:** intended send, actual send, reply, status, bid id, outcome and reason, polls after `202`, retried keys, and the price each bid was based on. One background reader refreshes that price, so no read delays a send. Format: `recording-format.md`.
- **Proxy:** each authorize, cancel and capture: bid id, start, end, result.
- **Subscribers (several):** accepted bids and the close, with `version` and arrival time.
- **End of run:** the fake's `/audit` and the target checker's verdict.
- **Manifest:** seed, rate, fake settings, target commit, instances, machine, generator version. The fake doesn't report its settings, so they are what the operator declares. The commit is the checkout's, assumed to be what is running. Instances are those seen answering.

## 4. What the data can claim
- **Many at once:** measured arrival times.
- **One order:** accepted bids by `version`, identical across subscribers. Rejected bids have no version; no order is implied for them. Don't assume versions are contiguous.
- **Hold first:** every accepted bid has a successful hold that finished before the stream announced it. Evidence from outside; the target's checker is the verdict.
- **Latency:** before, during and after the hold. Lock wait alone isn't visible from outside.
- **Replay:** the site replays a recording exactly. Re-sending a schedule repeats inputs, not outcomes (timing differs and the fake's faults aren't seeded).

## 5. Honest measurement
Sends follow the schedule whether or not replies have returned, and latency counts from the intended send. A run where the generator fell behind is invalid: by default, any send more than 50 ms late, or any send skipped because too many were still waiting for a reply. Repeat and show spread. One machine runs everything, and the site says so. The proxy's cost is measured by running with and without it.

## 6. Views
Price staircase with rejected bids beneath; race lanes; latency heatmap and per-bid waterfall; rule-check strip; stream agreement; hold ledger; sweep surfaces; fault view.

## 7. Slices
Each ends before the next starts.

| Slice | What | Finish line |
|---|---|---|
| 1a | Generator: schedule, sign-in, auction adapter, events | One command storms a local Holdfast and leaves a recording |
| 1b | Proxy and subscribers | Every accepted bid has its whole timeline |
| 1c | Conversion and checks, CI | A broken recording fails loudly, a clean one passes, CI runs a small storm |
| 2a | Site: anatomy of one storm | Live public page from a real recording, and a README image |
| 2b | Latency forensics | Click a slow bid and see where its time went |
| 2c | Failures and recovery | Each injected fault type has a visible story |
| 2d | Sweeps over rate, hold delay, decline rate | A sweep page with spread and a short finding, including what it doesn't show |

Version one ends at 2d. 2a is a fair stopping point.

In 2a, check Cloudflare Pages limits (25 MiB per file on the free plan when last read) against the DuckDB-WASM binary and the Parquet files (verify).

## 8. Later, not designed here
- **A Stripe fake and a deployed target.** First measure whether the current fake limits throughput. The better case for a new one is fidelity: speaking Stripe's API would load the target's real adapter. Keep it private, set a budget alarm, check host terms.
- **Holdfast against holdfast-ex.** Black boxes only, no tuning once runs begin, with and without a limit of about 25 requests a second. Holdfast treats only 5xx from the fake as transient, so a limit imposed by the proxy would answer `503` unless that changes.

## 9. Open questions
1. Proxy in Go (proposed, ADR 0003) or Python? Dan, before slice 1b.
2. Settled in slice 1a: the adapter runs the target's own seed script (§2).
3. Which recordings are small and interesting enough to commit? Slice 2a.

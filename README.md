# bid-storm

A load generator that records every stage of a bid storm against a real-time auction, and a data-deep static site built from those recordings. First target: [Holdfast](https://github.com/djwirz/holdfast), where a bid counts only once a card hold covers it.

The question the data should answer on sight: many bids land at once, they get one agreed order, and none counts without a hold.

**Status:** slice 1b. A storm against a local Holdfast is recorded end to end: each bid, its card hold and the live stream. Checks and the site come next. Design: [docs/spec.md](docs/spec.md) and [docs/adr/](docs/adr/). Synthetic users and auctions only, against local deployments.

## Run a storm

Needs Go 1.27, plus a [Holdfast](https://github.com/djwirz/holdfast) checkout already set up to run (Node 26, Docker, `.env.local`, dependencies installed, database migrated). By default it is looked for at `~/projects/holdfast`.

Start the stack, from this repo. The exported addresses keep the stack on this machine even if Holdfast's `.env.local` says otherwise. `FAKE_PAYMENTS_URL` sends the app's card calls through the recording proxy, which runs only while a storm does: between runs, the app logs that it can't reach the fake.

```bash
(cd ~/projects/holdfast && docker compose up -d --wait)
set -a; source targets/holdfast/fake.env; set +a
DATABASE_URL=postgres://holdfast:holdfast@localhost:5432/holdfast REDIS_URL=redis://localhost:6379 OTEL_EXPORTER_OTLP_ENDPOINT= FAKE_PAYMENTS_URL=http://127.0.0.1:4011 fnm exec --using=26 -- bash ~/projects/holdfast/load/stack.sh
```

Then, in another terminal:

```bash
fnm exec --using=26 -- make storm
```

The recording lands in `runs/<time>-s<seed>/`: `manifest.json`, `schedule.json` and `events.jsonl` ([format](docs/recording-format.md)). Flags go in `ARGS`, for example `make storm ARGS="-rate 100 -seed 2"`; `./bin/bidstorm -h` lists them. `-schedule runs/<run>/schedule.json` sends the same inputs again.

After the last bid it waits for the target to announce the close and to release or capture every hold. The command exits non-zero, keeping the recording, if the generator fell behind its schedule, the run stopped early, no bid was accepted or an accepted bid is missing a stage. To run without the proxy (to measure its cost), start the stack without `FAKE_PAYMENTS_URL` and pass `-proxy=false`. It refuses a target or database that isn't on this machine, and a Holdfast that is wired to Stripe.

`make test` runs `go vet` and the tests.

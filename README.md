# bid-storm

A load generator that records every stage of a bid storm against a real-time auction, and a data-deep static site built from those recordings. First target: [Holdfast](https://github.com/djwirz/holdfast), where a bid counts only once a card hold covers it.

The question the data should answer on sight: many bids land at once, they get one agreed order, and none counts without a hold.

![Price staircase of one recorded storm: accepted bids climb in steps, with the bids that did not count marked around them.](docs/img/staircase.png)

One recorded storm on one laptop, against a local Holdfast and its payment fake. Dig into it at [bid-storm.pages.dev](https://bid-storm.pages.dev).

**Status:** slice 2b. A storm against a local Holdfast is recorded end to end, converted, checked, and shown on a static page built from one published recording. CI tests it all and deploys the page from `main`. The page includes where each bid's time went, and the recording proxy's cost has been [measured](docs/proxy-cost.md). Next is failures and recovery. Design: [docs/spec.md](docs/spec.md) and [docs/adr/](docs/adr/). Synthetic users and auctions only, against local deployments.

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

## Check a recording

Needs [uv](https://docs.astral.sh/uv/), which fetches Python and DuckDB.

```bash
make check
```

This takes the newest run (or `RUN=runs/<run>`), writes it to Parquet under `parquet/` in the run folder and checks it: nothing lost, every timeline whole, every accepted bid held before any subscriber heard of it, subscribers told the same things, the fake's record in agreement and the target's own checker passing ([the checks](docs/recording-format.md#conversion-and-checks)). Any failure is listed and exits non-zero. These are evidence from outside the target; the target's checker is the verdict on it, and one run says nothing about how often it would pass.

## The site

Needs Node 26, pnpm and uv.

```bash
make site
```

This checks the published recording (`runs/curated/first-storm`), prepares its data and builds the page into `site/dist`. `cd site && pnpm preview` serves it; `pnpm test:e2e` runs the browser tests and `pnpm readme-image` redraws the picture above.

CI deploys `site/dist` to Cloudflare Pages ([bid-storm.pages.dev](https://bid-storm.pages.dev)) on pushes to `main`. In a fork it stays off until the repository has the variable `CLOUDFLARE_PROJECT` and the secrets `CLOUDFLARE_API_TOKEN` (permission: Account, Cloudflare Pages, Edit) and `CLOUDFLARE_ACCOUNT_ID`. The project is a direct-upload one, made once with `wrangler pages project create <name> --production-branch main`.

## Measure the proxy's cost

Stop your own stack first; Postgres and Redis stay up. It restarts the stack before each of twenty short storms and takes about ten minutes ([method and result](docs/proxy-cost.md)).

```bash
fnm exec --using=26 -- bash targets/holdfast/proxy-cost.sh
```

## Tests

`make test` and `make lint` cover the Go, the Python and the TypeScript. CI also scans for secrets and runs a small storm against a pinned Holdfast, checks it, then breaks a copy and requires the check to fail.

# Artifact spec: bid-storm

Filled from the kit's `templates/artifact-spec.md` before any agent builds code. Draft for PR #1.

## One line
Records every stage of a storm of simultaneous auction bids and shows, in data you can dig through, that they got one agreed order and none counted without a card hold.

## Market skills it proves
Python and SQL data work (DuckDB, Parquet), data visualization (TypeScript, D3), Go concurrency, load testing and latency analysis, CI/CD, static deployment. Not checked against `resume-score.json` yet.

## Problem and users
- **Who has the problem:** an engineer who has to believe a claim like "correct under load".
- **What they do today:** read a summary line and one chart, and take it on trust.
- **Why it matters:** the interesting behavior (races, slow holds, retries, failures) is invisible in totals.

The main reader is someone who enjoys the data. The README shows the few most compelling views; the site goes deep.

## Scope
- **Must have:** a seeded open-model generator; a recording proxy for hold events; live-stream recording; cross-checks plus the target's own checker; a static site covering one storm's anatomy, latency, failures and parameter sweeps.
- **Nice to have:** lock wait from the target's traces; instance-count sweeps.
- **Non-goals in version one:** a new Stripe fake, a deployed load target, the Holdfast and holdfast-ex comparison, a live dashboard, AI features, any change to a target.

## Success metric
A reader can see each of the three claims in recorded data, click into any slow or rejected bid, and find what the data can't show stated beside it.

## Constraints
- **Time:** about a week of focused sessions, as a guess. The site is the open-ended part.
- **Budget:** free tiers only in version one.
- **Data:** synthetic users and auctions, local targets.
- **Privacy:** no employer details; no secrets in recordings.

## Quality plan
- **Tests:** unit tests for the schedule, event writing and checks; an integration run against a local target; a broken recording must fail the checks.
- **Evals:** no AI features.
- **Observability:** the recording is the output; the generator also reports its own lag and caps hit.
- **Accessibility and performance:** readable without color alone, keyboard-usable controls, data loads in a few seconds on an ordinary connection.
- **Security basics:** no secrets in the repo or recordings, gitleaks in CI.
- **CI/CD:** lint and tests for Go, Python and TypeScript, a small storm, gitleaks; the site deploys from `main`.

## Depth target
Current rung: 0 · Target rung: 3 (engineered)

## Build stack (in order)
1. Go: generator and recording proxy.
2. Python and DuckDB: Parquet conversion, checks, summaries, sweeps.
3. TypeScript, D3 and DuckDB-WASM on Cloudflare Pages.

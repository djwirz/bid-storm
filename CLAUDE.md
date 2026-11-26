# bid-storm: instructions for Claude

A Go load generator and recording proxy that capture every stage of a bid storm, Python and DuckDB to check and summarize the recordings, and a static TypeScript and D3 site that goes deep on the data. First target: Holdfast. Use they/them for Dan.

## Status
Design accepted (PR #1). Slices 1a (generator), 1b (recording proxy, stream subscribers) 1c (Parquet conversion, checks, CI) and 2a (the site for one storm, `make site`) are built: `make storm` records a storm against a local Holdfast, `make check` judges the recording, `make test` tests the code (README). The page is live at https://bid-storm.pages.dev and deploys from `main`. Next is slice 2b: latency forensics. Format: `docs/recording-format.md`. Background: `~/projects/build-plan/research/bid-storm-scoping-2026-11-26.md`.

## How we work
- Pull requests only; never push to `main`. Merged PRs are the project's record.
- Spec first, then one thin end-to-end slice per PR. Start each slice in plan mode. Finish a slice before starting the next.
- Every decision that would be expensive to reverse gets an ADR in `docs/adr/` (format: ADR 0001). Don't decide silently in code.
- ADRs are written as `Accepted` in the PR that adds them; merging is the acceptance. Never rewrite a merged ADR's decision: add a new ADR that supersedes it.
- When the spec and the code disagree, stop and ask; update the spec in the same PR if the decision changes.
- Go is newer ground: explain the Go idea behind each new piece briefly in the PR description.
- Built with Claude Code in the open: the case study gets a section on how AI was used and where it was wrong, so note real mistakes in PR descriptions.

## Constraints
- Go stays small (generator and proxy). Python where it fits. TypeScript for the site only.
- Targets are black boxes. `~/projects/holdfast` and `~/projects/holdfast-ex` are read-only from here.
- Load only local or Dan's own throwaway deployments. Never the live demo or Stripe's API.
- Measure honestly: open-model load, spread over repeats, say what a run can't show.
- Never employer code, data or names beyond what's public. Synthetic data only.
- Recordings never contain cookies, keys or secrets.
- Free tiers first; a budget alarm before anything paid is deployed. No Terraform for its own sake.
- Check current library versions and APIs against their docs before pinning them.
- Target depth: deployed, tests and CI, ADRs, observability.
- Numbers in docs: honest estimates in words, never false precision.

# 9. The first views read prepared JSON; published recordings live in the repo

Date: 2026-11-26
Status: Accepted

Supersedes the part of [ADR 0006](0006-d3-duckdb-wasm-site.md) that has the browser query Parquet with DuckDB-WASM. The rest of ADR 0006 stands: a hand-built TypeScript and D3 site, deployed from CI to Cloudflare Pages.

## Context
Slice 2a was to check hosting limits. Cloudflare's free plan allows 25 MiB per file, for Pages and Workers static assets alike. The DuckDB-WASM binaries published on npm were between 32 and 40 MiB (read 2026-11-26). The first page shows one storm of a few hundred bids and asks nothing a reader would write a query for.

## Options considered
1. No DuckDB-WASM yet: Python and DuckDB prepare a small JSON file at build time.
2. DuckDB-WASM with the binary loaded from a public CDN.
3. Another host with a larger file limit.

For recordings: commit a chosen run's raw files; commit its Parquet; or keep recordings elsewhere and fetch them at build time.

## Decision
- Option 1. `bidsite` writes the JSON only from a recording that passes every check, and the site job builds it each time. It is not committed.
- A published recording is the raw files of one run (manifest, schedule, events, audit, check) in `runs/curated/<name>/`. Everything else is derived.
- Which run to publish is decided before looking at it, such as the first of a series that passes.

## Why
- The page loads about a hundred and fifty kilobytes of data instead of tens of megabytes of database, and has no third-party request at run time.
- The build can't publish a recording the checks reject.
- Raw files are the record; derived ones can be remade when the code improves.

## Consequences
- Readers can't query events in the browser. A view that needs that has to reopen this, with the file limit still in the way.
- Each view's data is a contract between `bidsite` and the site, kept by tests on both sides.
- Each published run adds under a megabyte to the repository for good. A sweep (slice 2d) will need summaries rather than every run's events.
- Published recordings need an allowlist entry in the secret scanner for their idempotency keys (`.gitleaks.toml`).

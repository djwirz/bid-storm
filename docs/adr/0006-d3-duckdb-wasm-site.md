# 6. Hand-built D3 site querying Parquet in the browser

Date: 2026-11-26
Status: Accepted

## Context
The site exists to go deep on the data and to be part of Dan's body of work.

## Options considered
1. TypeScript and D3 with DuckDB-WASM.
2. Observable Framework: quick, with Python loaders, but low-activity when checked on 2026-11-26.
3. Charts generated from Python (Altair, Vega-Lite): less bespoke.

## Decision
Option 1, deployed from CI to Cloudflare Pages.

## Why
Most freedom for unusual views, and readers can query real events with no server.

## Consequences
The most new code. File-size limits and load time need checking in slice 2a.

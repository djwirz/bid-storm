# 2. Record raw JSON lines, analyze as Parquet

Date: 2026-11-26
Status: Accepted

## Context
Several processes emit events during a storm; the analysis and the browser both need to query them.

## Options considered
1. JSON lines written raw, converted to Parquet afterwards.
2. Write Parquet or a database directly from Go: more code on the hot path, harder to inspect.
3. OpenTelemetry traces: needs the target instrumented a particular way, which breaks black-box use.

## Decision
Option 1, with one versioned event schema and a manifest per run.

## Why
Appending lines is cheap and readable, and DuckDB reads Parquet both locally and in the browser.

## Consequences
A conversion step, and schema changes must keep old curated recordings readable.

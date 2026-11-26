# 5. Go generates load, Python handles data

Date: 2026-11-26
Status: Accepted

## Context
Dan wants much more Python and only a small Go piece.

## Options considered
1. Go generator and proxy; Python and DuckDB for data.
2. All Python: asyncio timing under load is harder to trust, and no Go.
3. Mostly Go: against the preference.

## Decision
Option 1.

## Why
Go suits timed concurrent sends; Python suits analysis.

## Consequences
Two toolchains plus the site's. A Go Stripe fake later would need its own decision.

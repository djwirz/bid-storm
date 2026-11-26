# 7. Event schema: one line per exchange, microsecond times, listed fields only

Date: 2026-11-26
Status: Accepted

## Context
ADR 0002 chose JSON lines with one versioned schema. The proxy, the subscribers, the conversion, the checks and the site will all read it, and curated recordings must stay readable, so its shape is hard to change later.

## Options considered
**Granularity:** one line per HTTP exchange, written when it ends; or separate "sent" and "replied" lines joined later.

**Time:** integer Unix microseconds; Unix nanoseconds; offsets from the run's start; RFC 3339 text.

**Content:** a list of named fields; or captured requests and responses with secrets filtered out.

## Decision
- Every line has `v` (schema version) and `type`. A producer writes one line per exchange, when it ends. The generator's types are `bid`, `poll` and `skipped`.
- Times are integer Unix microseconds, field names ending `_us`: one wall-clock reading per process plus the monotonic time elapsed since.
- Within a version, fields and types may be added. Renaming, removing or changing the meaning of one raises `v`. Readers ignore what they don't know.
- Only named fields are written. Headers, cookies, raw bodies and error message text have no field. Absent values are `null`, not left out.

## Why
- One line per exchange needs no join to answer the common questions. A request still unanswered when the generator dies is lost, but such a run is incomplete anyway.
- Microseconds fit exactly in a JavaScript number, which nanoseconds don't, and map to DuckDB's timestamp. Absolute times let separate processes on one machine be joined without sharing an anchor.
- A list of fields can't leak what it has no place for. A filter has to anticipate each secret.

## Consequences
- Processes on different machines would need their clocks aligned (ADR 0003).
- Two processes' anchors can differ if the wall clock is adjusted between their starts; on one machine this should be small, and slice 1b can't claim finer ordering across processes than it has checked.
- Anything not recorded can't be recovered later; adding it means a new run.

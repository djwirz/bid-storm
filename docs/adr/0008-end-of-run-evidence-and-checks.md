# 8. End-of-run evidence, and what makes a recording fail

Date: 2026-11-26
Status: Accepted

## Context
Slice 1c has to say whether a recording supports the three claims (spec §4). The events show what was seen from outside. The target's database and the fake's own record can't be recovered after the stack moves on, so whatever the checks need has to be taken during the run.

## Options considered
**Evidence:** the generator saves the target checker's verdict and the fake's audit with the run; or the analysis calls the target and fake later.

**Where the judgment lives:** one command over the saved folder; or the generator refusing the run as it records.

**Format change:** add files and manifest fields within version 2; or raise the version.

## Decision
- The generator saves `check.json` and `audit.json` after the drain, with only named fields (ADR 0007). The checker runs through the target adapter while the proxy is still up; the audit is asked of the fake directly, filtered to the run's auction.
- `bidcheck` is the single gate. It converts the folder to Parquet and fails it on any check (`docs/recording-format.md` lists them), including a missing verdict or audit and a checker that exited non-zero.
- The manifest's `complete` and `valid` keep their meaning: whether the recording can be trusted, not whether the target behaved. A violation leaves a recording valid and fails `bidcheck`.
- Schema stays at version 2: only files and fields are added.
- A run that can't show a claim fails, such as one without the proxy, with fewer than two subscribers or with no accepted bid.

## Why
- A saved folder can be judged again, on another machine, after the target is gone.
- A finding about the target is the most interesting recording there is; marking it invalid would hide it.
- "Passed" should never include "not checked".

## Consequences
- A run takes longer: the checker's own wait comes after the drain.
- Recordings made before this slice fail the `audit` and `target_verdict` checks, though their events still load. None are committed.
- Runs made with `-proxy=false`, to measure the proxy's cost, can't pass.
- The checker's sentence about a violation is kept, clipped. It is the one free text from the target in a recording.
- A new target needs its adapter to turn its checker's result into the same fields.

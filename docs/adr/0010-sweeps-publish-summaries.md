# 10. A sweep publishes per-run summaries, not raw recordings

Date: 2026-11-27
Status: Accepted

## Context
ADR 0009 publishes a chosen run's raw files, under a megabyte each. Slice 2d's sweep is sixty runs, over a hundred megabytes on disk, and the page shows only a handful of numbers from each.

## Options considered
1. Commit one summary file with a line per run; keep the raw runs locally.
2. Commit every run's raw files.
3. Store the raw runs elsewhere (a release asset or object storage) and fetch them at build time.

## Decision
Option 1. `bidsweep` reads the run folders, runs the checks on each and writes `runs/curated/sweep/summary.json`: every run, valid or not, with its setting, check results, counts and times. That file is committed and is the page's data.

## Why
It keeps the repository small, and nothing new to host or pay for.

## Consequences
- A reader can't recheck a sweep from the repository, only rerun it (`targets/holdfast/sweep.sh`). The page says the raw recordings aren't published.
- A measure not in the summary needs the local raw runs, or a new sweep.
- The summary's check results are the author's word in a way a published recording's aren't: CI rechecks the first storm on every build, but not these.
- If a later view needs raw events from many runs, option 3 is the one to reopen.

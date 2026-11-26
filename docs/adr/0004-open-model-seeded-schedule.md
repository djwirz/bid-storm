# 4. Open-model load from a saved, seeded schedule

Date: 2026-11-26
Status: Accepted

## Context
A generator that waits for replies slows down when the target does, which flatters results. Runs should be repeatable.

## Options considered
1. Open model: a schedule generated from a seed, sent on time regardless of replies.
2. Closed model with a fixed set of looping bidders.
3. Reuse the k6 storm: it can pace by arrival rate, but its events don't join across holds and streams.

## Decision
Option 1. The schedule is saved with the run.

## Why
Latency stays honest and the same inputs can be sent again.

## Consequences
Bid amounts that depend on the current price are decided at send time, so only timing and intent are fixed. Generator lag and caps are recorded. k6 remains Holdfast's own storm.

# 3. Capture holds with a recording proxy

Date: 2026-11-26
Status: Accepted

## Context
A load generator sees only send and reply. Holdfast and holdfast-ex both call the same HTTP Stripe fake.

## Options considered
1. A pass-through proxy between the target and the fake.
2. Event logging added to the target: ties the tool to one codebase.
3. Client events only: loses the hold stage.

## Decision
Option 1, written in Go in the same module as the generator.

## Why
No target changes, and it works for any target that uses the fake. Go shares the event writer and keeps one clock on one machine.

## Consequences
The proxy adds a hop, which gets measured. It sees only fake-backed runs, and a deployed setup would need clock alignment. Choosing Go over Python grows the Go piece a little (spec §9 Q1).

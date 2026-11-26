# 1. Record architecture decisions

Date: 2026-11-26
Status: Accepted

## Context
Decisions made in chat get lost, and decisions made quietly in code are hard to review. The earlier projects used ADRs and it worked.

## Options considered
1. ADRs in the repo, reviewed in pull requests.
2. Decisions in PR descriptions only: scattered.
3. One design doc kept current: loses why things changed.

## Decision
Option 1. Each expensive-to-reverse decision gets a short ADR in `docs/adr/` with Context, Options considered, Decision, Why and Consequences.

## Why
Reviewers and later sessions find decisions in the same place in every project.

## Consequences
An ADR is `Accepted` in the PR that adds it. A merged one isn't rewritten; a new one supersedes it.

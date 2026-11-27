# 11. Publish the sweep's and the measurement's recordings, packed

Date: 2026-11-27
Status: Accepted

Supersedes [ADR 0010](0010-sweeps-publish-summaries.md).

## Context
ADR 0010 published only a summary of the sixty sweep runs, to keep the repository small, without measuring how small the runs would be packed. Packed with xz, all sixty are a little over 4 MB. The same applied to the proxy-cost runs, under 1 MB. The project exists to publish recordings (spec §1), and without them nobody can check the sweep or the measurement.

## Options considered
1. Commit each set as one packed file in the repository.
2. Attach the sets to a GitHub release and fetch them at build time.
3. Keep ADR 0010.

## Decision
Option 1. `runs/curated/sweep/runs.tar.xz` and `runs/curated/proxy-cost/runs.tar.xz` hold each run's raw files. CI unpacks them and requires that the sweep summary and `report.md` follow from them. The site serves both for download.

Because the secret scanner can't read packed files, `bidarchive` holds every piece of text in them to the shape its field can have, refusing unlisted fields, both when packing and in CI.

## Why
Small enough to live with the code, nothing new to host, and the published numbers become checkable.

## Consequences
- Each new sweep adds a few megabytes to the history for good.
- The shapes must grow with the recording format, or packing fails. That is the intent.
- The shape check stands in for the secret scanner on these files only. It limits what text can be there; it doesn't search for secrets.
- The archives are reproducible from the same runs on one Python version; another may compress to different bytes.
- The first proxy-cost attempt, which was wrong (`proxy-cost.md`), is not published.

"""bidsweep DIR OUT: one small summary of a sweep's runs (ADR 0010).

DIR holds run folders, each with the sweep.json that targets/holdfast/sweep.sh wrote. Every
run is listed, valid or not, with its checks' results. Times are in milliseconds.
"""

import argparse
import json
import sys
from pathlib import Path

from .checks import run_checks
from .convert import Run, Unreadable
from .site import storm

OUTCOMES = ("accepted", "too_low", "ended", "card", "other")


def outcome(bid: dict) -> str:
    if bid["status"] == "accepted":
        return "accepted"
    reasons = {
        "below_minimum": "too_low",
        "ended": "ended",
        "card_declined": "card",
        "processing_error": "card",
    }
    return reasons.get(bid["reason"], "other")


def quantiles(run: Run, values: list[int]) -> dict:
    """The typical and slow (9 in 10 faster) of some microsecond durations, in whole ms."""
    if not values:
        return {"typical": None, "slow": None}
    typical, slow = run.db.execute(
        "SELECT quantile_cont(v, 0.5), quantile_cont(v, 0.9) FROM unnest(?) AS t(v)", [values]
    ).fetchone()
    return {"typical": round(typical / 1000), "slow": round(slow / 1000)}


def summarize(folder: Path) -> dict:
    setting = json.loads((folder / "sweep.json").read_text())
    run = Run(folder)
    results = run_checks(run)
    data = storm(run, results)
    manifest = run.manifest
    bids = data["bids"]
    counts = dict.fromkeys(OUTCOMES, 0)
    for bid in bids:
        counts[outcome(bid)] += 1
    replies = [r[0] for r in run.db.execute("SELECT replied_us - intended_us FROM bids").fetchall()]
    return {
        **setting,
        "run_id": manifest["run_id"],
        "target_commit": manifest["target"]["commit"],
        "valid": manifest["valid"],
        "invalid_reasons": manifest["invalid_reasons"],
        "failed_checks": [name for name, problems in results if problems],
        "sends": len(replies),
        "skipped": manifest["results"]["skipped"],
        "outcomes": counts,
        "reply_ms": quantiles(run, replies),
        "outcome_ms": quantiles(
            run, [b["resolved"] - b["intended"] for b in bids if b["resolved"] is not None]
        ),
        "peak_in_flight": data["peak_in_flight"],
        "send_lag_max_ms": round(manifest["results"]["send_lag_max_us"] / 1000),
        "cpus": manifest["machine"]["cpus"],
    }


def main() -> None:
    parser = argparse.ArgumentParser(prog="bidsweep", description=__doc__)
    parser.add_argument("directory", type=Path)
    parser.add_argument("out", type=Path)
    args = parser.parse_args()
    try:
        runs = [summarize(f) for f in sorted(args.directory.iterdir()) if (f / "sweep.json").exists()]
    except (Unreadable, OSError, KeyError, TypeError, ValueError) as err:
        print(f"UNREADABLE: {err}", file=sys.stderr)
        sys.exit(2)
    if not runs:
        print("no sweep runs there", file=sys.stderr)
        sys.exit(2)
    runs.sort(key=lambda r: r["order"])
    args.out.parent.mkdir(parents=True, exist_ok=True)
    # One run per line, so a change to the file reads as a change to runs.
    lines = ",\n".join(json.dumps(r, separators=(",", ":")) for r in runs)
    args.out.write_text(f'{{"v":1,"runs":[\n{lines}\n]}}\n')
    invalid = sum(not r["valid"] for r in runs)
    failed = sum(r["valid"] and bool(r["failed_checks"]) for r in runs)
    print(f"{args.out}: {len(runs)} runs, {invalid} invalid, {failed} valid runs with a failed check")


if __name__ == "__main__":
    main()

"""bidcost DIR: reply latency of the runs in DIR, with and without the recording proxy.

Latency is each bid POST's reply, counted from its intended send (spec §5). Runs are grouped
by the fake's declared settings and paired in the order they started: one with the proxy and
one without. An invalid run is reported and its pair left out.
"""

import argparse
import statistics
import sys
from pathlib import Path

from .convert import Run, Unreadable

MEASURES = {"typical": 0.5, "slow": 0.9, "slowest 1%": 0.99}


def measure(folder: Path) -> dict:
    run = Run(folder)
    manifest = run.manifest
    quantiles = [f"quantile_cont(replied_us - intended_us, {q}) / 1000" for q in MEASURES.values()]
    values = run.db.execute(f"SELECT {', '.join(quantiles)} FROM bids").fetchone()
    declared = manifest["target"]["fake_settings_declared"]
    return {
        "id": manifest["run_id"],
        "started_us": manifest["started_us"],
        "setting": f"fake waits {declared.get('FAKE_LATENCY_MS', '?')} ms",
        "proxy": manifest["settings"]["proxy"],
        "valid": manifest["valid"],
        "latency_ms": dict(zip(MEASURES, values, strict=True)),
    }


def pairs(runs: list[dict]) -> list[tuple[dict, dict]]:
    """Consecutive runs, one with the proxy and one without, as (with, without). Both valid."""
    ordered = sorted(runs, key=lambda r: r["started_us"])
    out = []
    for first, second in zip(ordered[::2], ordered[1::2], strict=False):
        if first["proxy"] != second["proxy"] and first["valid"] and second["valid"]:
            out.append((first, second) if first["proxy"] else (second, first))
    return out


def report(runs: list[dict]) -> str:
    lines = []
    for setting in dict.fromkeys(r["setting"] for r in sorted(runs, key=lambda r: r["started_us"])):
        group = [r for r in runs if r["setting"] == setting]
        lines += [f"## {setting}", "", "| Run | Proxy | " + " | ".join(f"{m} ms" for m in MEASURES) + " |"]
        lines.append("|---|---|" + "---|" * len(MEASURES))
        for r in sorted(group, key=lambda r: r["started_us"]):
            note = "" if r["valid"] else " (invalid)"
            values = " | ".join(f"{r['latency_ms'][m]:.1f}" for m in MEASURES)
            lines.append(f"| {r['id']}{note} | {'on' if r['proxy'] else 'off'} | {values} |")
        usable = pairs(group)
        lines += ["", f"Proxy on minus off, over {len(usable)} usable pairs:", ""]
        for m in MEASURES:
            if not usable:
                break
            diff = [on["latency_ms"][m] - off["latency_ms"][m] for on, off in usable]
            lines.append(
                f"- {m}: median {statistics.median(diff):+.1f} ms, from {min(diff):+.1f} "
                f"to {max(diff):+.1f}; higher with the proxy in {sum(d > 0 for d in diff)} of {len(diff)}"
            )
        lines.append("")
    return "\n".join(lines)


def main() -> None:
    parser = argparse.ArgumentParser(prog="bidcost", description=__doc__)
    parser.add_argument("directory", type=Path, help="a folder of run folders")
    args = parser.parse_args()
    try:
        runs = [measure(f) for f in sorted(args.directory.iterdir()) if (f / "manifest.json").exists()]
    except (Unreadable, OSError, KeyError) as err:
        print(f"UNREADABLE: {err}", file=sys.stderr)
        sys.exit(2)
    if not runs:
        print("no runs there", file=sys.stderr)
        sys.exit(2)
    print(report(runs))


if __name__ == "__main__":
    main()

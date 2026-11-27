"""bidarchive: published sets of recordings, packed and checked (ADR 0011).

  bidarchive pack RUNS_DIR OUT.tar.xz   pack every run folder in RUNS_DIR
  bidarchive verify runs/curated        unpack each published set, check that every text value
                                        has an expected shape, and that the published sweep
                                        summary and proxy-cost report follow from the runs

A run is packed as its raw files only. The archive is reproducible and names no user.
"""

import argparse
import io
import json
import re
import sys
import tarfile
import tempfile
from pathlib import Path

from .convert import Unreadable
from .cost import measure, report
from .sweep import summarize

FILES = ("manifest.json", "schedule.json", "events.jsonl", "audit.json", "check.json", "sweep.json")

# Recordings never hold secrets because only named fields are written (ADR 0007). Packed files
# are opaque to the secret scanner, so before packing and again in CI every piece of text is
# held to the shape its field can have. Anything else, or any unlisted field, is refused.
UUID = r"[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}"
WORD = r"[a-z_.]{1,32}"
NUMBER = r"\d+(\.\d+)?(-\d+(\.\d+)?)?"
SENTENCE = r"[A-Za-z0-9 _.,:;'()?-]{0,200}"
COMMIT = r"[0-9a-f]{40}|unknown"
SHAPES = {
    "auction_id": UUID,
    "bid_id": UUID,
    "intent_id": r"pi_fake_[0-9a-f]{32}",
    "idempotency_key": rf"{UUID}|[0-9a-f]{{8}}-\d+|(cancel_hold|capture):{UUID}:\d+",
    "bidder": r"Bidder [0-9a-f]{1,8}",
    "server_at": r"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?Z",
    "run_id": r"\d{8}T\d{6}\.\d{3}Z-s\d+",
    "instance": r"\d{1,5}",
    "instances_seen": r"\d{1,5}",
    "url": r"http://(localhost|127\.0\.0\.1):\d+",
    "commit": COMMIT,
    "revision": COMMIT,
    "go_version": r"go[0-9a-z.]+",
    "os": WORD,
    "arch": r"[a-z0-9]{1,16}",
    "name": WORD,
    "hold_ms": NUMBER,
    "invalid_reasons": SENTENCE,
    "drain_waiting": SENTENCE,
    "message": SENTENCE,
    **dict.fromkeys(
        [
            "bid_status",
            "error_code",
            "event",
            "op",
            "reason",
            "schedule_from",
            "status",
            "type",
            "why",
            "transport_error",
        ],
        WORD,
    ),
    **{
        f"FAKE_{name}": NUMBER
        for name in [
            "CANCEL_FAULT_RATE",
            "DECLINE_RATE",
            "LATENCY_MS",
            "LOST_RESPONSE_RATE",
            "SERVER_ERROR_EVERY",
        ]
    },
}
# Names of counts, such as an HTTP status or "rejected:ended".
KEY = r"[A-Za-z0-9_:.]{1,64}"


def unexpected(value, field: str = "") -> str | None:
    """The first field whose text doesn't have its shape, or None."""
    if isinstance(value, dict):
        for name, inner in value.items():
            if not re.fullmatch(KEY, name):
                return field or "a field name"
            if found := unexpected(inner, name):
                return found
    elif isinstance(value, list):
        for inner in value:
            if found := unexpected(inner, field):
                return found
    elif isinstance(value, str) and not (field in SHAPES and re.fullmatch(SHAPES[field], value)):
        return field
    return None


def check_run(folder: Path) -> None:
    """Raises Unreadable, naming the field but never its value."""
    for path in sorted(folder.iterdir()):
        if path.name not in FILES:
            raise Unreadable(f"{folder.name}: unexpected file {path.name}")
        text = path.read_text()
        documents = text.splitlines() if path.suffix == ".jsonl" else [text]
        for document in documents:
            if field := unexpected(json.loads(document)):
                raise Unreadable(f"{folder.name}/{path.name}: unexpected text in {field}")


def runs_in(directory: Path) -> list[Path]:
    return sorted(f for f in directory.iterdir() if (f / "manifest.json").exists())


def pack(source: Path, out: Path) -> int:
    """Writes the runs in source to out. The same runs give the same bytes."""
    runs = runs_in(source)
    if not runs:
        raise Unreadable(f"{source}: no runs there")
    out.parent.mkdir(parents=True, exist_ok=True)
    with tarfile.open(out, "w:xz", format=tarfile.USTAR_FORMAT) as archive:
        for run in runs:
            with tempfile.TemporaryDirectory() as staged:
                copy = Path(staged) / run.name
                copy.mkdir()
                for name in FILES:
                    if (run / name).exists():
                        (copy / name).write_bytes((run / name).read_bytes())
                check_run(copy)
                for path in sorted(copy.iterdir()):
                    data = path.read_bytes()
                    # A fresh header: no times, owners or user names from this machine.
                    entry = tarfile.TarInfo(f"{run.name}/{path.name}")
                    entry.size, entry.mode = len(data), 0o644
                    archive.addfile(entry, io.BytesIO(data))
    return len(runs)


def unpack(archive: Path, into: Path) -> list[Path]:
    with tarfile.open(archive, "r:xz") as opened:
        opened.extractall(into, filter="data")
    runs = runs_in(into)
    for run in runs:
        check_run(run)
    return runs


def verify(curated: Path) -> list[str]:
    """What doesn't follow from the published runs. Empty means all of it does."""
    problems = []
    with tempfile.TemporaryDirectory() as work:
        sweep = curated / "sweep"
        runs = unpack(sweep / "runs.tar.xz", Path(work) / "sweep")
        published = json.loads((sweep / "summary.json").read_text())["runs"]
        rebuilt = sorted((summarize(run) for run in runs), key=lambda r: r["order"])
        if len(rebuilt) != len(published):
            problems.append(f"sweep: {len(rebuilt)} runs packed, {len(published)} in the summary")
        for new, old in zip(rebuilt, published, strict=False):
            if new != old:
                differs = sorted(k for k in new.keys() | old.keys() if new.get(k) != old.get(k))
                problems.append(f"sweep: {old['run_id']} differs in {', '.join(differs)}")
        cost = curated / "proxy-cost"
        runs = unpack(cost / "runs.tar.xz", Path(work) / "proxy-cost")
        if report([measure(run) for run in runs]) != (cost / "report.md").read_text():
            problems.append("proxy-cost: report.md doesn't follow from the runs")
    return problems


def main() -> None:
    parser = argparse.ArgumentParser(prog="bidarchive", description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    packing = commands.add_parser("pack")
    packing.add_argument("source", type=Path)
    packing.add_argument("out", type=Path)
    commands.add_parser("verify").add_argument("curated", type=Path)
    args = parser.parse_args()
    try:
        if args.command == "pack":
            count = pack(args.source, args.out)
            print(f"{args.out}: {count} runs, {args.out.stat().st_size // 1024} KiB")
            return
        problems = verify(args.curated)
    except (Unreadable, OSError, KeyError, ValueError, tarfile.TarError) as err:
        print(f"UNREADABLE: {err}", file=sys.stderr)
        sys.exit(2)
    for problem in problems:
        print(f"FAIL  {problem}")
    if problems:
        sys.exit(1)
    print("the published summary and report follow from the published runs")


if __name__ == "__main__":
    main()

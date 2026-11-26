"""bidcheck RUN: convert a run folder to Parquet and check it.

Exits 0 if every check passes, 1 if one fails, 2 if the folder can't be read.
"""

import argparse
import json
import sys
from pathlib import Path

from .checks import run_checks
from .convert import Run, Unreadable

SHOWN = 10


def check(folder: Path) -> int:
    # An earlier result must not outlive a run that can no longer be read.
    (folder / "checks.json").unlink(missing_ok=True)
    try:
        run = Run(folder)
        results = run_checks(run)
        parquet = run.write_parquet()
    except (Unreadable, KeyError, TypeError) as err:
        print(f"UNREADABLE {folder}: {err}", file=sys.stderr)
        return 2

    for name, problems in results:
        print(f"{'FAIL' if problems else 'ok  '}  {name}")
        for problem in problems[:SHOWN]:
            print(f"        {problem}")
        if len(problems) > SHOWN:
            print(f"        ... and {len(problems) - SHOWN} more (checks.json)")
    failed = sum(bool(problems) for _, problems in results)
    report = {
        "run_id": run.manifest["run_id"],
        "passed": not failed,
        "checks": [{"name": name, "passed": not p, "problems": p} for name, p in results],
    }
    (folder / "checks.json").write_text(json.dumps(report, indent=2) + "\n")
    print(f"{f'FAILED {failed} checks' if failed else 'PASSED'}: {folder} (Parquet in {parquet})")
    return 1 if failed else 0


def main() -> None:
    parser = argparse.ArgumentParser(prog="bidcheck", description=__doc__)
    parser.add_argument("run", type=Path, help="a run folder, runs/<time>-s<seed>")
    sys.exit(check(parser.parse_args().run))


if __name__ == "__main__":
    main()

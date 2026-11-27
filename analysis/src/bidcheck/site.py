"""bidsite RUN OUT: the site's data for one storm, from a recording that passes every check.

One small JSON file (ADR 0009). Times are microseconds from the first intended send, and bids
are numbered instead of carrying the target's ids. Writes nothing if a check fails.
"""

import argparse
import json
import sys
from pathlib import Path

from .checks import EVER_ACCEPTED, run_checks
from .convert import Run, Unreadable


def records(run: Run, sql: str, *params) -> list[dict]:
    cursor = run.db.execute(sql, list(params))
    names = [column[0] for column in cursor.description]
    return [dict(zip(names, row, strict=True)) for row in cursor.fetchall()]


def peak_in_flight(run: Run) -> int:
    """The most bid POSTs awaiting a reply at one moment."""
    return run.db.execute(
        """
        WITH edges AS (SELECT sent_us AS t, 1 AS step FROM bids
                       UNION ALL SELECT replied_us, -1 FROM bids),
        running AS (SELECT sum(step) OVER (ORDER BY t, step ROWS UNBOUNDED PRECEDING) AS in_flight FROM edges)
        SELECT max(in_flight) FROM running
        """
    ).fetchone()[0]


def storm(run: Run, results: list[tuple[str, list[str]]]) -> dict:
    t0 = run.db.execute("SELECT min(intended_us) FROM bids").fetchone()[0]
    manifest = run.manifest

    # One entry per bid, from the POST that first named it. Its status is the most telling one
    # any reply showed: ever accepted wins, else the resolved status over "pending".
    bids = records(
        run,
        f"""
        WITH said AS (
            SELECT bid_id, bid_status, reason, sent_us, replied_us FROM bids WHERE bid_id IS NOT NULL
            UNION ALL
            SELECT bid_id, bid_status, reason, sent_us, replied_us FROM polls
            WHERE bid_id IS NOT NULL AND bid_status IS NOT NULL),
        status AS (
            SELECT bid_id,
                CASE WHEN bool_or(bid_status IN {EVER_ACCEPTED}) THEN 'accepted'
                     ELSE max_by(bid_status, replied_us) FILTER (bid_status <> 'pending') END AS status,
                max_by(reason, replied_us) FILTER (reason IS NOT NULL) AS reason,
                min(replied_us) FILTER (bid_status <> 'pending') AS resolved_us
            FROM said GROUP BY bid_id),
        -- The outcome came after the last request still answered "pending" was sent.
        pending AS (
            SELECT said.bid_id, max(said.sent_us) AS pending_us
            FROM said JOIN status USING (bid_id)
            WHERE said.bid_status = 'pending' AND said.sent_us < status.resolved_us
            GROUP BY said.bid_id),
        first_send AS (
            SELECT * FROM bids WHERE bid_id IS NOT NULL
            QUALIFY row_number() OVER (PARTITION BY bid_id ORDER BY sent_us, seq) = 1)
        SELECT f.bid_id, f.seq, f.bidder, f.amount, s.status,
            CASE WHEN s.status = 'accepted' THEN NULL ELSE s.reason END AS reason,
            f.intended_us - $t0 AS intended, f.sent_us - $t0 AS sent,
            f.replied_us - $t0 AS replied, s.resolved_us - $t0 AS resolved,
            f.http_status, (SELECT count(*) FROM polls p WHERE p.seq = f.seq) AS polls,
            p.pending_us - $t0 AS pending_until
        FROM first_send f JOIN status s USING (bid_id) LEFT JOIN pending p USING (bid_id)
        ORDER BY f.sent_us, f.seq
        """.replace("$t0", str(int(t0))),
    )
    holds = records(
        run,
        """
        SELECT bid_id, op, received_us - ? AS received, replied_us - ? AS replied, ok, reason, http_status
        FROM holds WHERE bid_id IS NOT NULL ORDER BY received_us, replied_us
        """,
        t0,
        t0,
    )
    heard = records(
        run,
        """
        SELECT event, subscriber, version, amount, arrived_us - ? AS arrived FROM stream
        WHERE event <> 'snapshot' ORDER BY version, subscriber
        """,
        t0,
    )

    subscribers = manifest["settings"]["subscribers"]
    announcements: dict[int, dict] = {}
    close = None
    for h in heard:
        if h["event"] == "bid.accepted":
            entry = announcements.setdefault(
                h["amount"], {"version": h["version"], "heard": [None] * subscribers}
            )
        else:
            close = close or {"version": h["version"], "amount": h["amount"], "heard": [None] * subscribers}
            entry = close
        entry["heard"][h["subscriber"]] = h["arrived"]

    by_id = {}
    for bid in bids:
        by_id[bid.pop("bid_id")] = bid
        announced = announcements.get(bid["amount"]) if bid["status"] == "accepted" else None
        bid["version"] = announced["version"] if announced else None
        bid["heard"] = announced["heard"] if announced else None
        bid["holds"] = []
    for hold in holds:
        bid = by_id.get(hold.pop("bid_id"))
        if bid is not None:
            bid["holds"].append(hold)

    # Sends that made no new bid: retries answered with the original, and refusals.
    # `bid` is the seq of the bid a send got back, if any; `changed`, whether the schedule
    # meant it to offer a different amount from the send whose key it reused.
    first_seqs = {bid["seq"] for bid in bids}
    seq_of = {bid_id: bid["seq"] for bid_id, bid in by_id.items()}
    others = records(
        run,
        """
        SELECT b.seq, b.retry_of, b.amount, b.http_status, b.error_code, b.bid_id,
            s.change_amount AS changed, b.sent_us - ? AS sent, b.replied_us - ? AS replied
        FROM bids b JOIN schedule s USING (seq) ORDER BY b.sent_us, b.seq
        """,
        t0,
        t0,
    )
    others = [send for send in others if send["seq"] not in first_seqs]
    for send in others:
        send["bid"] = seq_of.get(send.pop("bid_id"))
    # What the fake itself says became of its holds.
    ended = [intent["status"] for intent in (run.audit or {}).get("intents", [])]
    fake = {
        "placed": len(ended),
        "released": ended.count("canceled"),
        "captured": ended.count("succeeded"),
        "held": ended.count("requires_capture"),
    }

    return {
        "run": {
            "id": manifest["run_id"],
            "started_us": manifest["started_us"],
            "target": manifest["target"]["name"],
            "target_commit": manifest["target"]["commit"],
            "instances": len(manifest["target"]["instances_seen"]),
            "fake_settings_declared": manifest["target"]["fake_settings_declared"],
            "seed": manifest["schedule"]["seed"],
            "rate_per_second": manifest["schedule"]["rate_per_second"],
            "duration_us": manifest["schedule"]["duration_us"],
            "auction_seconds": manifest["settings"]["auction_seconds"],
            "bidders": manifest["schedule"]["bidders"],
            "retry_rate": manifest["schedule"]["retry_rate"],
            "conflict_rate": manifest["schedule"]["conflict_rate"],
            "subscribers": subscribers,
            "cpus": manifest["machine"]["cpus"],
            "send_lag_max_us": manifest["results"]["send_lag_max_us"],
        },
        "sends": len(bids) + len(others),
        "peak_in_flight": peak_in_flight(run),
        "checks": [{"name": name, "passed": not problems} for name, problems in results],
        "bids": bids,
        "other_sends": others,
        "fake": fake,
        "close": close,
    }


def build(folder: Path, out: Path) -> int:
    # A file from an earlier build must not stand in for a recording that now fails.
    out.unlink(missing_ok=True)
    try:
        run = Run(folder)
        results = run_checks(run)
        failed = [name for name, problems in results if problems]
        if failed:
            print(f"not published: {folder} fails {', '.join(failed)}", file=sys.stderr)
            return 1
        data = storm(run, results)
    except (Unreadable, KeyError, TypeError) as err:
        print(f"UNREADABLE {folder}: {err}", file=sys.stderr)
        return 2
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(data, separators=(",", ":")) + "\n")
    print(f"{out}: {len(data['bids'])} bids, {out.stat().st_size // 1024} KiB")
    return 0


def main() -> None:
    parser = argparse.ArgumentParser(prog="bidsite", description=__doc__)
    parser.add_argument("run", type=Path, help="a run folder")
    parser.add_argument("out", type=Path, help="where to write the JSON")
    args = parser.parse_args()
    sys.exit(build(args.run, args.out))

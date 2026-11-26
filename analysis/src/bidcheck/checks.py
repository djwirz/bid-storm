"""What a recording has to show before its claims can be believed (spec §4, ADR 0008).

Each check returns its problems as sentences; none means it passed. The checks read the
tables convert.Run loads. Evidence from outside the target: its own checker is the verdict
on it, and one of these checks requires that verdict.
"""

from .convert import Run

# A bid seen with any of these was accepted at some point.
EVER_ACCEPTED = "('accepted', 'outbid', 'won')"

VIEWS = f"""
-- Every reply that named a bid, from POSTs and polls. A poll has no amount: its bid's POST does.
CREATE OR REPLACE VIEW replies AS
    SELECT bid_id, bid_status, amount FROM bids WHERE bid_id IS NOT NULL
    UNION ALL
    SELECT p.bid_id, p.bid_status, b.amount FROM polls p JOIN bids b USING (seq) WHERE p.bid_id IS NOT NULL;

CREATE OR REPLACE VIEW accepted AS
    SELECT bid_id, any_value(amount) AS amount FROM replies
    WHERE bid_status IN {EVER_ACCEPTED} GROUP BY bid_id;

-- Stream events name no bid. Accepted amounts strictly increase, so the amount identifies one.
CREATE OR REPLACE VIEW announced AS
    SELECT subscriber, amount, min(arrived_us) AS arrived_us FROM stream
    WHERE event = 'bid.accepted' GROUP BY subscriber, amount;
"""


def rows(run: Run, sql: str, *params) -> list[tuple]:
    return run.db.execute(sql, list(params)).fetchall()


def usable(run: Run):
    m = run.manifest
    if not m["complete"]:
        yield "the manifest says the run is not complete"
    if not m["valid"]:
        yield "the manifest says the recording is not valid: " + "; ".join(m["invalid_reasons"])
    if not m["settings"]["proxy"]:
        yield "holds weren't recorded (-proxy=false), so hold-first can't be checked"
    if m["settings"]["subscribers"] < 2:
        yield "fewer than 2 subscribers, so agreement between them can't be checked"
    if rows(run, "SELECT count(*) FROM accepted")[0][0] == 0:
        yield "no bid was accepted: passing checks over nothing would prove nothing"


def nothing_lost(run: Run):
    results = run.manifest["results"]
    holds = results["hold_calls"] or {}
    counted = [
        ("schedule entries", "SELECT count(*) FROM schedule", results["scheduled"]),
        ("bid lines", "SELECT count(*) FROM bids", results["sent"]),
        ("skipped lines", "SELECT count(*) FROM skipped", results["skipped"]),
        ("poll lines", "SELECT count(*) FROM polls", results["polls"]),
        ("stream lines", "SELECT count(*) FROM stream", results["stream_events"]),
        ("accepted bids", "SELECT count(*) FROM accepted", results["accepted_bids"]),
    ]
    counted += [
        (f"{op} lines", f"SELECT count(*) FROM holds WHERE op = '{op}'", holds.get(op, 0))
        for op in ("authorize", "cancel", "capture")
    ]
    for what, sql, expected in counted:
        got = rows(run, sql)[0][0]
        if got != expected:
            yield f"{what}: the recording has {got}, the manifest counted {expected}"


def timelines(run: Run):
    for seq, sends in rows(
        run,
        """
        WITH sends AS (SELECT seq FROM bids UNION ALL SELECT seq FROM skipped)
        SELECT seq, count(sends.seq) FROM schedule LEFT JOIN sends USING (seq)
        GROUP BY seq HAVING count(sends.seq) <> 1 ORDER BY seq
        """,
    ):
        yield f"seq {seq} has {sends} bid or skipped lines; want exactly 1"
    for (seq,) in rows(
        run,
        """
        SELECT b.seq FROM bids b LEFT JOIN schedule s USING (seq)
        WHERE s.seq IS NULL OR s.bidder <> b.bidder OR s.retry_of IS DISTINCT FROM b.retry_of ORDER BY 1
        """,
    ):
        yield f"seq {seq}: the bid line doesn't match the schedule"
    for seq, n in rows(
        run,
        """
        SELECT p.seq, p.n FROM polls p LEFT JOIN bids b USING (seq)
        WHERE b.seq IS NULL OR p.bid_id IS DISTINCT FROM b.bid_id ORDER BY 1, 2
        """,
    ):
        yield f"poll {n} of seq {seq} has no bid line for the same bid"
    for (bid_id,) in rows(
        run,
        """
        SELECT bid_id FROM replies GROUP BY bid_id
        HAVING count(*) FILTER (bid_status <> 'pending') = 0 ORDER BY 1
        """,
    ):
        yield f"bid {bid_id} was never seen to resolve"
    for what, seq in rows(
        run,
        """
        SELECT 'bid', seq FROM bids WHERE NOT (intended_us <= sent_us AND sent_us <= replied_us)
        UNION ALL SELECT 'poll', seq FROM polls WHERE sent_us > replied_us
        ORDER BY 1, 2
        """,
    ):
        yield f"{what} seq {seq}: its times are out of order"
    late = rows(run, "SELECT count(*) FROM holds WHERE received_us > replied_us")[0][0]
    if late:
        yield f"{late} hold lines were answered before they arrived"


def keys(run: Run):
    # A retry may overtake its original and take the key, so a changed amount need not get a
    # 422. What must hold: a key makes at most one bid, and a bid has one key and one amount.
    for key, n in rows(
        run,
        """
        SELECT idempotency_key, count(DISTINCT bid_id) FROM bids
        GROUP BY idempotency_key HAVING count(DISTINCT bid_id) > 1 ORDER BY 1
        """,
    ):
        yield f"idempotency key {key} produced {n} different bids"
    for (bid_id,) in rows(
        run,
        """
        SELECT bid_id FROM bids WHERE bid_id IS NOT NULL GROUP BY bid_id
        HAVING count(DISTINCT idempotency_key) > 1 OR count(DISTINCT amount) > 1 ORDER BY 1
        """,
    ):
        yield f"bid {bid_id} answers more than one idempotency key or amount"


def hold_first(run: Run):
    auction = run.manifest["target"]["auction_id"]
    for bid_id, amount, held_us, heard_us in rows(
        run,
        """
        WITH held AS (
            SELECT a.bid_id, min(h.replied_us) AS held_us
            FROM accepted a JOIN holds h
              ON h.bid_id = a.bid_id AND h.amount = a.amount AND h.op = 'authorize' AND h.ok
                 AND h.auction_id = ?
            GROUP BY a.bid_id),
        heard AS (SELECT amount, min(arrived_us) AS heard_us FROM announced GROUP BY amount)
        SELECT a.bid_id, a.amount, held_us, heard_us
        FROM accepted a LEFT JOIN held USING (bid_id) LEFT JOIN heard USING (amount)
        WHERE held_us IS NULL OR held_us >= heard_us
        ORDER BY a.amount
        """,
        auction,
    ):
        if held_us is None:
            yield f"accepted bid {bid_id} ({amount}) has no successful authorize for its amount"
        else:
            yield (
                f"accepted bid {bid_id} ({amount}) was announced {held_us - heard_us} us "
                "before its hold finished"
            )


def amounts(run: Run):
    for amount, n in rows(
        run, "SELECT amount, count(*) FROM accepted GROUP BY amount HAVING count(*) > 1 ORDER BY 1"
    ):
        yield f"{n} accepted bids share the amount {amount}"
    for (amount,) in rows(
        run,
        """
        SELECT DISTINCT n.amount FROM announced n LEFT JOIN accepted a USING (amount)
        WHERE a.bid_id IS NULL ORDER BY 1
        """,
    ):
        yield f"the stream announced {amount}, which no recorded accepted bid offered"
    for bid_id, amount, subscriber in rows(
        run,
        """
        SELECT a.bid_id, a.amount, s.subscriber
        FROM accepted a CROSS JOIN range(?) AS s(subscriber)
        LEFT JOIN announced n ON n.subscriber = s.subscriber AND n.amount = a.amount
        WHERE n.amount IS NULL ORDER BY a.amount, s.subscriber
        """,
        run.manifest["settings"]["subscribers"],
    ):
        yield f"subscriber {subscriber} never heard of accepted bid {bid_id} ({amount})"


def streams(run: Run):
    # Snapshots are left out: they say where a connection started, not what happened.
    heard = [
        rows(
            run,
            """
            SELECT event, version, amount, bidder, server_at, status FROM stream
            WHERE subscriber = ? AND event <> 'snapshot' ORDER BY arrived_us, version
            """,
            subscriber,
        )
        for subscriber in range(run.manifest["settings"]["subscribers"])
    ]
    if not heard:
        return
    for subscriber, events in enumerate(heard[1:], start=1):
        if events != heard[0]:
            at = next(
                (i for i, (a, b) in enumerate(zip(heard[0], events, strict=False)) if a != b),
                min(len(heard[0]), len(events)),
            )
            yield (
                f"subscribers 0 and {subscriber} differ from event {at} "
                f"({len(heard[0])} and {len(events)} events)"
            )
    for subscriber, events in enumerate(heard):
        # Versions must increase but need not be contiguous.
        if any(a[1] >= b[1] for a, b in zip(events, events[1:], strict=False)):
            yield f"subscriber {subscriber}: versions don't strictly increase"
        raised = [e[2] for e in events if e[0] == "bid.accepted"]
        if any(a is None or b is None or a >= b for a, b in zip(raised, raised[1:], strict=False)):
            yield f"subscriber {subscriber}: accepted amounts don't strictly increase"
        closes = [i for i, e in enumerate(events) if e[0] == "auction.closed"]
        if closes != [len(events) - 1]:
            yield f"subscriber {subscriber}: want exactly one auction.closed, last"
        elif raised and events[-1][2] != raised[-1]:
            yield f"subscriber {subscriber}: the close names {events[-1][2]}, not the last accepted amount"


def audit(run: Run):
    if run.audit is None:
        yield "audit.json is missing: what the fake holds is unknown"
        return
    if run.audit["auction_id"] != run.manifest["target"]["auction_id"]:
        yield "audit.json is for a different auction"
        return
    for bid_id, amount, n, held in rows(
        run,
        """
        SELECT a.bid_id, a.amount, count(i.intent_id), any_value(i.amount)
        FROM accepted a LEFT JOIN audit i USING (bid_id)
        GROUP BY a.bid_id, a.amount
        HAVING count(i.intent_id) <> 1 OR any_value(i.amount) <> a.amount ORDER BY a.amount
        """,
    ):
        yield f"accepted bid {bid_id} ({amount}): the fake has {n} intents for it, amount {held}"
    for bid_id, n in rows(
        run, "SELECT bid_id, count(*) FROM audit GROUP BY bid_id HAVING count(*) > 1 ORDER BY 1"
    ):
        yield f"bid {bid_id} has {n} intents"
    # After the close only the winner, the highest accepted bid, keeps its hold.
    for intent_id, bid_id, status, is_winner in rows(
        run,
        """
        WITH winner AS (SELECT bid_id FROM accepted ORDER BY amount DESC LIMIT 1)
        SELECT intent_id, bid_id, status, bid_id IN (SELECT bid_id FROM winner) AS is_winner FROM audit
        WHERE (is_winner AND status NOT IN ('requires_capture', 'succeeded'))
           OR (NOT is_winner AND status <> 'canceled')
        ORDER BY intent_id
        """,
    ):
        whose = "the winner's" if is_winner else "a losing"
        yield f"{whose} intent {intent_id} (bid {bid_id}) is {status}"
    for (intent_id,) in rows(
        run,
        """
        SELECT DISTINCT h.intent_id FROM holds h LEFT JOIN audit i USING (intent_id)
        WHERE h.op = 'authorize' AND h.ok
          AND (i.intent_id IS NULL OR i.bid_id IS DISTINCT FROM h.bid_id
               OR i.amount IS DISTINCT FROM h.amount)
        ORDER BY 1
        """,
    ):
        yield f"the proxy saw intent {intent_id} authorized, but the fake's record differs or lacks it"
    for (intent_id,) in rows(
        run,
        """
        SELECT i.intent_id FROM audit i
        WHERE NOT EXISTS (SELECT 1 FROM holds h WHERE h.op = 'authorize' AND h.bid_id = i.bid_id)
        ORDER BY 1
        """,
    ):
        yield f"the fake has intent {intent_id}, but the proxy saw no authorize for its bid"
    for op, intent_id, status in rows(
        run,
        """
        SELECT DISTINCT h.op, h.intent_id, i.status FROM holds h LEFT JOIN audit i USING (intent_id)
        WHERE h.ok AND ((h.op = 'cancel' AND i.status IS DISTINCT FROM 'canceled')
                     OR (h.op = 'capture' AND i.status IS DISTINCT FROM 'succeeded'))
        ORDER BY 1, 2
        """,
    ):
        yield f"the proxy saw a successful {op} of {intent_id}, but the fake says {status}"


def target_verdict(run: Run):
    c = run.check
    if c is None:
        yield "check.json is missing: the target's checker gave no verdict"
        return
    if c["auction_id"] != run.manifest["target"]["auction_id"]:
        yield "check.json is for a different auction"
        return
    if c["exit_code"] != 0:
        yield f"the target's checker exited {c['exit_code']}"
    if not c["quiet"]:
        yield "the target had not settled when its checker gave up waiting"
    if c["outbox_failed"]:
        yield f"{c['outbox_failed']} of the target's side effects gave up"
    for v in c["violations"]:
        yield f"invariant {v['invariant']}: {v['message']}"
    ours = rows(run, "SELECT count(*) FROM accepted")[0][0]
    if c["bids_accepted"] != ours:
        yield f"the target counts {c['bids_accepted']} accepted bids, the recording {ours}"


CHECKS = [usable, nothing_lost, timelines, keys, hold_first, amounts, streams, audit, target_verdict]


def run_checks(run: Run) -> list[tuple[str, list[str]]]:
    run.db.execute(VIEWS)
    return [(check.__name__, list(check(run))) for check in CHECKS]

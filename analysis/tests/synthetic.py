"""A small made-up clean run that tests break one thing at a time.

Bid 0 is accepted and later outbid. Bid 1 is answered pending, then wins. Seq 2 retries bid 0's
key. Seq 3 is refused. Stream versions skip from 1 to 3: they need not be contiguous.
"""

import json
from dataclasses import dataclass
from pathlib import Path

AUCTION = "auction-1"
T = 1_000_000_000_000


def bid(seq, bid_id, status, amount, retry_of=None, key=None, http=201):
    sent = T + seq * 1000
    return {
        "v": 2,
        "type": "bid",
        "seq": seq,
        "bidder": seq if retry_of is None else retry_of,
        "retry_of": retry_of,
        "intended_us": sent - 10,
        "sent_us": sent,
        "replied_us": sent + 500,
        "idempotency_key": key or f"k-{seq if retry_of is None else retry_of}",
        "amount": amount,
        "min_next_seen": 1000,
        "price_age_us": 5,
        "http_status": http,
        "bid_id": bid_id,
        "bid_status": status,
    }


def hold(op, bid_id, intent, at, amount=None, ok=True):
    return {
        "v": 2,
        "type": "hold",
        "op": op,
        "received_us": at - 100,
        "replied_us": at,
        "bid_id": bid_id,
        "auction_id": AUCTION,
        "intent_id": intent,
        "amount": amount,
        "ok": ok,
        "http_status": 200,
    }


def heard(subscriber, event, version, amount, at, status=None):
    return {
        "v": 2,
        "type": "stream",
        "subscriber": subscriber,
        "arrived_us": at + subscriber,
        "event": event,
        "version": version,
        "amount": amount,
        "bidder": None if amount is None else f"Bidder {amount}",
        "status": status,
    }


@dataclass
class Synthetic:
    manifest: dict
    schedule: list
    events: list
    audit: dict | None
    check: dict | None

    def write(self, folder: Path) -> Path:
        """Writes the run. The manifest's counts are of the run as built, not as since broken."""
        documents = {
            "manifest.json": self.manifest,
            "schedule.json": {"version": 1, "entries": self.schedule},
            "audit.json": self.audit,
            "check.json": self.check,
        }
        for name, document in documents.items():
            if document is not None:
                (folder / name).write_text(json.dumps(document))
        (folder / "events.jsonl").write_text("".join(json.dumps(e) + "\n" for e in self.events))
        return folder

    def find(self, **want) -> dict:
        return next(e for e in self.events if all(e.get(k) == v for k, v in want.items()))

    def drop(self, **want) -> None:
        self.events.remove(self.find(**want))


def synthetic() -> Synthetic:
    events = [
        heard(0, "snapshot", 0, None, T - 100, "open"),
        heard(1, "snapshot", 0, None, T - 100, "open"),
        bid(0, "b0", "accepted", 1000),
        bid(1, "b1", "pending", 1100, http=202),
        {
            "v": 2,
            "type": "poll",
            "seq": 1,
            "n": 1,
            "sent_us": T + 4000,
            "replied_us": T + 4100,
            "bid_id": "b1",
            "bid_status": "accepted",
        },
        bid(2, "b0", "accepted", 1000, retry_of=0),
        bid(3, "b3", "rejected", 1100, http=201),
        hold("authorize", "b0", "pi_0", T + 200, 1000),
        hold("authorize", "b1", "pi_1", T + 2000, 1100),
        hold("cancel", "b0", "pi_0", T + 5000),
        hold("capture", "b1", "pi_1", T + 6000, 1100),
    ]
    for s in (0, 1):
        events += [
            heard(s, "bid.accepted", 1, 1000, T + 300),
            heard(s, "bid.accepted", 3, 1100, T + 3000),
            heard(s, "auction.closed", 4, 1100, T + 7000, "settled"),
        ]
    schedule = [
        {"seq": i, "offset_us": i * 1000, "bidder": b, "steps": 0, "retry_of": r, "change_amount": False}
        for i, (b, r) in enumerate([(0, None), (1, None), (0, 0), (3, None)])
    ]
    manifest = {
        "schema_version": 2,
        "run_id": "synthetic",
        "complete": True,
        "valid": True,
        "invalid_reasons": [],
        "settings": {"proxy": True, "subscribers": 2},
        "target": {"auction_id": AUCTION},
        "results": {
            "scheduled": 4,
            "sent": 4,
            "skipped": 0,
            "polls": 1,
            "stream_events": 8,
            "accepted_bids": 2,
            "hold_calls": {"authorize": 2, "cancel": 1, "capture": 1},
        },
    }
    intents = [("pi_0", "b0", 1000, "canceled"), ("pi_1", "b1", 1100, "succeeded")]
    audit = {
        "v": 2,
        "auction_id": AUCTION,
        "intents": [
            {"intent_id": i, "bid_id": b, "amount": a, "amount_capturable": 0, "status": s}
            for i, b, a, s in intents
        ],
    }
    check = {
        "v": 2,
        "auction_id": AUCTION,
        "exit_code": 0,
        "quiet": True,
        "bids_accepted": 2,
        "outbox_failed": 0,
        "violations": [],
    }
    return Synthetic(manifest, schedule, events, audit, check)

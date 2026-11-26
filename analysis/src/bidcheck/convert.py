"""Loads a run folder into DuckDB and writes it out as Parquet (docs/recording-format.md).

Columns and their types are listed here, so an empty kind of event still gets a typed file, a
value of the wrong type fails, and fields added later are ignored (ADR 0007).
"""

import json
from pathlib import Path

import duckdb

SCHEMA_VERSION = 2

EXCHANGE = {
    "sent_us": "BIGINT",
    "replied_us": "BIGINT",
    "http_status": "INTEGER",
    "instance": "VARCHAR",
    "bid_id": "VARCHAR",
    "bid_status": "VARCHAR",
    "reason": "VARCHAR",
    "error_code": "VARCHAR",
    "transport_error": "VARCHAR",
}

# Per event type: the Parquet file's name, its columns, and which of them can never be null.
EVENTS = {
    "bid": (
        "bids",
        {
            "seq": "BIGINT",
            "bidder": "BIGINT",
            "intended_us": "BIGINT",
            "idempotency_key": "VARCHAR",
            "amount": "BIGINT",
            "min_next_seen": "BIGINT",
            "price_age_us": "BIGINT",
            "retry_of": "BIGINT",
            **EXCHANGE,
        },
        ["seq", "bidder", "intended_us", "idempotency_key", "amount", "sent_us", "replied_us"],
    ),
    "poll": ("polls", {"seq": "BIGINT", "n": "BIGINT", **EXCHANGE}, ["seq", "n", "sent_us", "replied_us"]),
    "skipped": (
        "skipped",
        {"seq": "BIGINT", "bidder": "BIGINT", "intended_us": "BIGINT", "at_us": "BIGINT", "why": "VARCHAR"},
        ["seq", "bidder", "intended_us"],
    ),
    "hold": (
        "holds",
        {
            "op": "VARCHAR",
            "received_us": "BIGINT",
            "replied_us": "BIGINT",
            "bid_id": "VARCHAR",
            "auction_id": "VARCHAR",
            "intent_id": "VARCHAR",
            "amount": "BIGINT",
            "idempotency_key": "VARCHAR",
            "http_status": "INTEGER",
            "ok": "BOOLEAN",
            "reason": "VARCHAR",
            "retryable": "BOOLEAN",
            "error_code": "VARCHAR",
            "transport_error": "VARCHAR",
        },
        ["op", "received_us", "replied_us"],
    ),
    "stream": (
        "stream",
        {
            "subscriber": "BIGINT",
            "arrived_us": "BIGINT",
            "event": "VARCHAR",
            "version": "BIGINT",
            "amount": "BIGINT",
            "bidder": "VARCHAR",
            "server_at": "VARCHAR",
            "status": "VARCHAR",
        },
        ["subscriber", "arrived_us", "event", "version"],
    ),
    "stream_conn": (
        "stream_conns",
        {
            "subscriber": "BIGINT",
            "n": "BIGINT",
            "event": "VARCHAR",
            "at_us": "BIGINT",
            "last_event_id": "BIGINT",
            "instance": "VARCHAR",
            "http_status": "INTEGER",
            "transport_error": "VARCHAR",
        },
        ["subscriber", "n", "event", "at_us"],
    ),
}

SCHEDULE = {
    "seq": "BIGINT",
    "offset_us": "BIGINT",
    "bidder": "BIGINT",
    "steps": "BIGINT",
    "retry_of": "BIGINT",
    "change_amount": "BOOLEAN",
}

AUDIT = {
    "intent_id": "VARCHAR",
    "bid_id": "VARCHAR",
    "amount": "BIGINT",
    "amount_capturable": "BIGINT",
    "status": "VARCHAR",
}


class Unreadable(Exception):
    """The folder isn't a recording this code can read."""


class Run:
    """One run folder: its JSON documents, and its rows in an in-memory DuckDB."""

    def __init__(self, folder: Path):
        self.folder = Path(folder)
        self.manifest = self._document("manifest.json")
        version = self.manifest.get("schema_version")
        if version != SCHEMA_VERSION:
            raise Unreadable(f"schema version {version!r}; this reads version {SCHEMA_VERSION}")
        # Absent evidence is for the checks to report, not a reason to stop.
        self.audit = self._document("audit.json", optional=True)
        self.check = self._document("check.json", optional=True)
        self.db = duckdb.connect()
        try:
            self._load_events()
            self._table("schedule", SCHEDULE, self._document("schedule.json")["entries"])
            self._table("audit", AUDIT, (self.audit or {}).get("intents", []))
        except (duckdb.Error, KeyError, TypeError) as err:
            raise Unreadable(str(err)) from err

    def _document(self, name: str, optional: bool = False):
        path = self.folder / name
        if optional and not path.exists():
            return None
        try:
            document = json.loads(path.read_text())
        except (OSError, ValueError) as err:
            raise Unreadable(f"{name}: {err}") from err
        if not isinstance(document, dict):
            raise Unreadable(f"{name}: not an object")
        return document

    def _load_events(self):
        path = self.folder / "events.jsonl"
        if not path.is_file():
            raise Unreadable("events.jsonl is missing")
        columns = {"v": "BIGINT", "type": "VARCHAR"}
        # A name can mean different types on different events (`bidder` is a number on a bid and
        # an alias on a stream event). Those are read as JSON and typed per event below.
        mixed = set()
        for _, fields, _ in EVENTS.values():
            for name, kind in fields.items():
                if columns.setdefault(name, kind) != kind:
                    mixed.add(name)
        columns.update(dict.fromkeys(mixed, "JSON"))
        # A line that doesn't parse stops the load: ignore_errors stays off.
        self.db.read_json(str(path), format="newline_delimited", columns=columns).create("events")

        bad = self.db.execute(
            "SELECT count(*) FROM events WHERE v IS DISTINCT FROM ? OR type IS NULL", [SCHEMA_VERSION]
        ).fetchone()[0]
        if bad:
            raise Unreadable(f"events.jsonl: {bad} lines lack a type or aren't version {SCHEMA_VERSION}")
        # Types this code doesn't know stay in `events` and are otherwise ignored.
        for kind, (table, fields, required) in EVENTS.items():
            names = ", ".join(
                f"""CAST("{name}" ->> '$' AS {sql_type}) AS "{name}\"""" if name in mixed else f'"{name}"'
                for name, sql_type in fields.items()
            )
            self.db.execute(f"CREATE TABLE {table} AS SELECT {names} FROM events WHERE type = '{kind}'")
            missing = " OR ".join(f'"{name}" IS NULL' for name in required)
            bad = self.db.execute(f"SELECT count(*) FROM {table} WHERE {missing}").fetchone()[0]
            if bad:
                raise Unreadable(f"events.jsonl: {bad} {kind} lines lack one of {', '.join(required)}")

    def _table(self, name: str, columns: dict, rows: list):
        declared = ", ".join(f'"{column}" {kind}' for column, kind in columns.items())
        self.db.execute(f"CREATE TABLE {name} ({declared})")
        if rows:
            marks = ", ".join("?" * len(columns))
            self.db.executemany(
                f"INSERT INTO {name} VALUES ({marks})", [[row.get(c) for c in columns] for row in rows]
            )

    def write_parquet(self) -> Path:
        """Writes one file per table into <run>/parquet/."""
        out = self.folder / "parquet"
        out.mkdir(exist_ok=True)
        for table in [name for name, _, _ in EVENTS.values()] + ["schedule", "audit"]:
            self.db.table(table).write_parquet(str(out / f"{table}.parquet"))
        return out

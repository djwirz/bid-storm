import json
from pathlib import Path

import pytest
from synthetic import synthetic

from bidcheck.site import build

CURATED = Path(__file__).parents[2] / "runs" / "curated" / "first-storm"


def complete(run):
    """What the data step needs beyond what the checks read."""
    run.manifest.update(
        started_us=0,
        schedule={
            "seed": 1,
            "rate_per_second": 4,
            "duration_us": 1000,
            "bidders": 4,
            "retry_rate": 0.1,
            "conflict_rate": 0.3,
        },
        machine={"cpus": 2},
    )
    run.manifest["settings"]["auction_seconds"] = 1
    run.manifest["target"].update(name="t", commit="c", instances_seen=["1"], fake_settings_declared={})
    run.manifest["results"]["send_lag_max_us"] = 1
    return run


def test_bids_holds_and_announcements_are_joined(tmp_path):
    complete(synthetic()).write(tmp_path)
    out = tmp_path / "storm.json"
    assert build(tmp_path, out) == 0
    data = json.loads(out.read_text())
    assert [(b["seq"], b["status"], b["version"]) for b in data["bids"]] == [
        (0, "accepted", 1),
        (1, "accepted", 3),
        (3, "rejected", None),
    ]
    first = data["bids"][0]
    assert [h["op"] for h in first["holds"]] == ["authorize", "cancel"]
    assert len(first["heard"]) == 2 and first["holds"][0]["replied"] < min(first["heard"])
    # Seq 2 retried seq 0's key: a send, but no new bid.
    assert [s["seq"] for s in data["other_sends"]] == [2] and data["sends"] == 4
    assert (data["other_sends"][0]["bid"], data["other_sends"][0]["changed"]) == (0, False)
    assert data["fake"] == {"placed": 2, "released": 1, "captured": 1, "held": 0}
    assert data["close"]["version"] == 4
    # Bid 1 was told "pending" and polled once.
    assert (first["http_status"], first["polls"], first["pending_until"]) == (201, 0, None)
    second = data["bids"][1]
    assert (second["http_status"], second["polls"]) == (202, 1)
    assert second["pending_until"] == second["sent"] < second["resolved"]
    assert "b0" not in out.read_text()


def test_a_run_that_fails_a_check_is_not_published(tmp_path):
    run = complete(synthetic())
    run.drop(type="hold", op="authorize", bid_id="b0")
    run.write(tmp_path)
    out = tmp_path / "storm.json"
    out.write_text("from an earlier build")
    assert build(tmp_path, out) == 1
    assert not out.exists()


@pytest.mark.skipif(not CURATED.exists(), reason="no curated recording")
def test_the_published_recording_passes_and_builds(tmp_path):
    out = tmp_path / "storm.json"
    assert build(CURATED, out) == 0
    data = json.loads(out.read_text())
    accepted = [b for b in data["bids"] if b["status"] == "accepted"]
    assert len(accepted) == 38 and all(b["version"] and b["heard"] for b in accepted)
    assert all(check["passed"] for check in data["checks"])

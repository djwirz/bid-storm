import json

import duckdb
import pytest
from synthetic import AUCTION, T, bid, hold, synthetic

from bidcheck.__main__ import check
from bidcheck.checks import run_checks
from bidcheck.convert import Run


def failures(run, folder) -> dict[str, list[str]]:
    return {name: problems for name, problems in run_checks(Run(run.write(folder))) if problems}


def test_a_clean_run_passes_and_is_converted(tmp_path):
    synthetic().write(tmp_path)
    assert check(tmp_path) == 0
    assert json.loads((tmp_path / "checks.json").read_text())["passed"]
    got = duckdb.sql(f"SELECT seq, amount FROM '{tmp_path}/parquet/bids.parquet' ORDER BY seq").fetchall()
    assert got == [(0, 1000), (1, 1100), (2, 1000), (3, 1100)]
    # A kind with no lines still has a typed file.
    assert duckdb.sql(f"SELECT why FROM '{tmp_path}/parquet/skipped.parquet'").fetchall() == []


def test_unknown_fields_and_types_are_ignored(tmp_path):
    run = synthetic()
    run.events[2]["added_later"] = {"deep": 1}
    run.events.append({"v": 2, "type": "added_later"})
    assert failures(run, tmp_path) == {}


def test_a_retry_may_overtake_its_original(tmp_path):
    # Seq 5 retries seq 4 with a higher amount but arrives first: it takes the key, and the
    # original is the one refused.
    run = synthetic()
    run.events += [
        bid(4, None, None, 1200, http=422),
        bid(5, "b5", "rejected", 1300, retry_of=4),
    ]
    run.schedule += [
        {"seq": 4, "offset_us": 4000, "bidder": 4, "steps": 0, "retry_of": None, "change_amount": False},
        {"seq": 5, "offset_us": 5000, "bidder": 4, "steps": 0, "retry_of": 4, "change_amount": True},
    ]
    run.manifest["results"].update(scheduled=6, sent=6)
    assert failures(run, tmp_path) == {}


def drop_hold(run):
    run.drop(type="hold", op="authorize", bid_id="b0")


def hold_after_announcement(run):
    run.find(type="hold", op="authorize", bid_id="b1")["replied_us"] = T + 3500


def refused_hold(run):
    run.find(type="hold", op="authorize", bid_id="b0")["ok"] = False


def subscriber_misses_an_event(run):
    run.drop(type="stream", subscriber=1, event="bid.accepted", amount=1000)


def subscriber_told_something_else(run):
    run.find(type="stream", subscriber=1, event="bid.accepted", amount=1100)["bidder"] = "someone else"


def subscriber_told_in_another_order(run):
    first = run.find(type="stream", subscriber=1, event="bid.accepted", amount=1000)
    second = run.find(type="stream", subscriber=1, event="bid.accepted", amount=1100)
    first["arrived_us"], second["arrived_us"] = second["arrived_us"], first["arrived_us"]


def bid_line_lost(run):
    run.drop(type="bid", seq=3)


def never_resolved(run):
    run.drop(type="poll")


def key_made_two_bids(run):
    run.find(type="bid", seq=2)["bid_id"] = "b-other"


def two_accepted_at_one_amount(run):
    run.find(type="bid", seq=3).update(bid_status="accepted", amount=1100)


def losing_hold_still_live(run):
    run.audit["intents"][0]["status"] = "requires_capture"


def second_hold_for_a_bid(run):
    run.audit["intents"].append({**run.audit["intents"][0], "intent_id": "pi_extra"})


def hold_the_proxy_never_saw(run):
    run.audit["intents"].append(
        {"intent_id": "pi_x", "bid_id": "b-x", "amount": 1, "amount_capturable": 0, "status": "canceled"}
    )


def capture_the_fake_denies(run):
    run.audit["intents"][1]["status"] = "requires_capture"


def checker_found_a_violation(run):
    run.check.update(exit_code=1, violations=[{"invariant": 4, "bid_id": "b0", "message": "still open"}])


def no_verdict(run):
    run.check = None


def no_audit(run):
    run.audit = None


def invalid_run(run):
    run.manifest.update(valid=False, invalid_reasons=["generator fell behind"])


def proxy_off(run):
    run.manifest["settings"]["proxy"] = False


def hold_for_another_auction(run):
    run.find(type="hold", op="authorize", bid_id="b0")["auction_id"] = AUCTION + "-other"


def extra_hold_line(run):
    run.events.append(hold("cancel", "b0", "pi_0", T + 5500))


@pytest.mark.parametrize(
    "break_it, expected",
    [
        (drop_hold, "hold_first"),
        (hold_after_announcement, "hold_first"),
        (refused_hold, "hold_first"),
        (hold_for_another_auction, "hold_first"),
        (subscriber_misses_an_event, "streams"),
        (subscriber_told_something_else, "streams"),
        (subscriber_told_in_another_order, "streams"),
        (bid_line_lost, "nothing_lost"),
        (bid_line_lost, "timelines"),
        (extra_hold_line, "nothing_lost"),
        (never_resolved, "timelines"),
        (key_made_two_bids, "keys"),
        (two_accepted_at_one_amount, "amounts"),
        (losing_hold_still_live, "audit"),
        (second_hold_for_a_bid, "audit"),
        (hold_the_proxy_never_saw, "audit"),
        (capture_the_fake_denies, "audit"),
        (no_audit, "audit"),
        (checker_found_a_violation, "target_verdict"),
        (no_verdict, "target_verdict"),
        (invalid_run, "usable"),
        (proxy_off, "usable"),
    ],
)
def test_a_broken_run_fails(tmp_path, break_it, expected):
    run = synthetic()
    break_it(run)
    assert expected in failures(run, tmp_path)
    assert check(tmp_path) == 1


def truncated(text):
    return text[:-20]


def not_json(text):
    return text + "not json\n"


def wrong_type(text):
    return text.replace('"amount": 1000', '"amount": "lots"', 1)


def missing_field(text):
    return text.replace('"intended_us"', '"renamed"', 1)


def other_version(text):
    return text.replace('"v": 2', '"v": 3', 1)


@pytest.mark.parametrize("damage", [truncated, not_json, wrong_type, missing_field, other_version])
def test_a_damaged_file_is_unreadable(tmp_path, damage):
    synthetic().write(tmp_path)
    assert check(tmp_path) == 0
    path = tmp_path / "events.jsonl"
    path.write_text(damage(path.read_text()))
    assert check(tmp_path) == 2
    assert not (tmp_path / "checks.json").exists()

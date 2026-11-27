import json

from synthetic import synthetic
from test_site import complete

from bidcheck.sweep import outcome, summarize


def write(run, folder):
    complete(run).manifest["results"]["skipped"] = 0
    run.write(folder)
    (folder / "sweep.json").write_text(json.dumps({"order": 1, "rate": 4, "hold_ms": "0", "decline": 0}))
    return folder


def test_a_run_is_summarized_with_its_setting(tmp_path):
    got = summarize(write(synthetic(), tmp_path))
    assert (got["rate"], got["valid"], got["failed_checks"]) == (4, True, [])
    assert got["outcomes"] == {"accepted": 2, "too_low": 0, "ended": 0, "card": 0, "other": 1}
    assert got["sends"] == 4 and got["reply_ms"] == {"typical": 1, "slow": 1}


def test_an_invalid_run_is_listed_with_what_failed(tmp_path):
    run = synthetic()
    run.manifest.update(valid=False, invalid_reasons=["generator fell behind"])
    run.audit = None
    got = summarize(write(run, tmp_path))
    assert not got["valid"] and got["invalid_reasons"] == ["generator fell behind"]
    assert "audit" in got["failed_checks"] and "usable" in got["failed_checks"]


def test_outcomes_are_grouped_as_on_the_page():
    assert outcome({"status": "lost_race", "reason": "below_minimum"}) == "too_low"
    assert outcome({"status": "rejected", "reason": "processing_error"}) == "card"

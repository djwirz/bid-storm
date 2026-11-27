from synthetic import synthetic

from bidcheck.cost import measure, pairs, report


def run(started, proxy, typical, problem=None, setting="s"):
    return {
        "id": f"r{started}",
        "started_us": started,
        "setting": setting,
        "proxy": proxy,
        "problem": problem,
        "latency_ms": {"typical": typical, "slow": typical, "slowest 1%": typical},
    }


def test_latency_counts_from_the_intended_send(tmp_path):
    made = synthetic()
    made.manifest.update(started_us=1)
    made.manifest["target"]["fake_settings_declared"] = {"FAKE_LATENCY_MS": "0"}
    made.write(tmp_path)
    # Every synthetic bid replies 510 us after it was due.
    got = measure(tmp_path)
    assert got["latency_ms"]["typical"] == 0.51 and got["proxy"] is True


def test_pairs_are_on_minus_off_whatever_the_order():
    runs = [run(1, True, 12), run(2, False, 10), run(3, False, 20), run(4, True, 19)]
    assert [(on["latency_ms"]["typical"], off["latency_ms"]["typical"]) for on, off in pairs(runs)] == [
        (12, 10),
        (19, 20),
    ]
    text = report(runs)
    assert "over 2 usable pairs" in text
    assert "typical: median +0.5 ms, from -1.0 to +2.0; higher with the proxy in 1 of 2" in text


def test_a_pair_with_an_invalid_run_is_left_out_and_shown():
    runs = [run(1, True, 12, problem="invalid"), run(2, False, 10), run(3, False, 20), run(4, True, 19)]
    assert len(pairs(runs)) == 1
    assert "r1 (invalid)" in report(runs)


def test_a_run_that_never_reached_the_shared_fake_is_not_compared(tmp_path):
    made = synthetic()
    made.manifest.update(started_us=1)
    made.manifest["target"]["fake_settings_declared"] = {}
    made.audit["intents"] = []
    made.write(tmp_path)
    assert measure(tmp_path)["problem"] == "shared fake not used"

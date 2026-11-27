import tarfile

import pytest
from synthetic import synthetic

from bidcheck.archive import pack, unpack
from bidcheck.convert import Unreadable

AUCTION = "0f8c621a-d2a2-4565-a781-1a808fe28ecc"


def recording(tmp_path, **extra):
    """The synthetic run with ids of the real shapes, which is all the packer accepts."""
    run = synthetic()
    folder = tmp_path / "runs" / "20260101T000000.000Z-s1"
    folder.mkdir(parents=True)
    run.manifest = {"schema_version": 2, "target": {"auction_id": AUCTION}}
    run.events = [{"v": 2, "type": "bid", "bid_id": AUCTION, **extra}]
    run.audit = run.check = None
    run.schedule = []
    run.write(folder)
    return folder


def test_the_same_runs_pack_to_the_same_bytes_and_name_no_user(tmp_path):
    recording(tmp_path)
    (tmp_path / "runs" / "20260101T000000.000Z-s1" / "checks.json").write_text("derived")
    first, second = tmp_path / "a.tar.xz", tmp_path / "b.tar.xz"
    pack(tmp_path / "runs", first)
    pack(tmp_path / "runs", second)
    assert first.read_bytes() == second.read_bytes()
    with tarfile.open(first) as archive:
        members = archive.getmembers()
    assert [m.name.split("/")[1] for m in members] == ["events.jsonl", "manifest.json", "schedule.json"]
    assert all((m.uid, m.gid, m.uname, m.gname, m.mtime) == (0, 0, "", "", 0) for m in members)
    assert len(unpack(first, tmp_path / "out")) == 1


@pytest.mark.parametrize(
    "extra",
    [
        {"cookie": "sid=abc"},  # a field nobody listed
        {"reason": "Bearer abc.def"},  # a listed field with something else in it
        {"idempotency_key": "sk_" + "test_51H8notARealKey"},
        {"bidder": "someone@example.com"},
    ],
)
def test_text_of_an_unexpected_shape_is_not_packed(tmp_path, extra):
    recording(tmp_path, **extra)
    with pytest.raises(Unreadable) as refused:
        pack(tmp_path / "runs", tmp_path / "a.tar.xz")
    # The refusal names the field, never the value.
    assert list(extra.values())[0] not in str(refused.value)

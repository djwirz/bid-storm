import { describe, expect, it } from 'vitest';
import { acceptedBids, delays, examples, groupOf, holdOf, latencyOf, ledger, millis, pathOf, raceWindow, stages, type Bid, type Hold, type Send, type Storm } from './data.ts';
import { cells, rowOf } from './latency.ts';

function bid(seq: number, over: Partial<Bid> = {}): Bid {
  return {
    seq,
    bidder: seq,
    amount: 1000 + seq * 100,
    status: 'rejected',
    reason: 'below_minimum',
    intended: seq * 1000,
    sent: seq * 1000,
    replied: seq * 1000 + 500,
    resolved: seq * 1000 + 500,
    http_status: 201,
    polls: 0,
    pending_until: null,
    version: null,
    heard: null,
    holds: [],
    ...over,
  };
}

const won = (seq: number, version: number, over: Partial<Bid> = {}) =>
  bid(seq, { status: 'accepted', reason: null, version, heard: [seq * 1000 + 450, seq * 1000 + 400], ...over });

const storm = { bids: [won(2, 3), won(1, 1), bid(0), bid(9)] } as Storm;

describe('derived data', () => {
  it('orders accepted bids by version, which need not be contiguous', () => {
    expect(acceptedBids(storm).map((b) => [b.version, b.firstHeard])).toEqual([
      [1, 1400],
      [3, 2400],
    ]);
  });

  it('keeps the bids in flight while the selected one was decided', () => {
    const [first] = acceptedBids(storm);
    const window = raceWindow(
      { bids: [bid(0, { replied: 900 }), bid(7, { sent: 1200, replied: 5000 }), won(1, 1), bid(9)] } as Storm,
      first!,
    );
    expect(window.bids.map((b) => b.seq)).toEqual([7, 1]);
    // From the earliest send to the latest reply, with a margin.
    expect([window.from, window.to]).toEqual([1000 - 20_000, 5000 + 20_000]);
  });

  it('finds the hold that covered a bid, not one that failed', () => {
    const hold = { op: 'authorize' as const, received: 1, replied: 2, reason: null, http_status: 503 };
    const covered = bid(1, { holds: [{ ...hold, ok: null }, { ...hold, ok: true, replied: 9 }] });
    expect(holdOf(covered)?.replied).toBe(9);
    expect(holdOf(bid(2))).toBeUndefined();
  });

  it('groups outcomes and measures delay from the first to hear', () => {
    expect(groupOf(bid(0, { status: 'lost_race' }))).toBe('price');
    expect(groupOf(bid(0, { reason: 'card_declined' }))).toBe('card');
    expect(delays([105, 100, null])).toEqual([5, 0, null]);
  });
});

const call = (received: number, replied: number, ok: boolean | null): Hold => ({
  op: 'authorize',
  received,
  replied,
  ok,
  reason: null,
  http_status: ok === null ? 503 : 200,
});

describe('where the time went', () => {
  it('writes long times in seconds', () => {
    expect([millis(400), millis(354_000), millis(33_932_000)]).toEqual(['0.4 ms', '354 ms', '33.9 s']);
  });

  const lost = bid(0, {
    intended: 0,
    sent: 10,
    replied: 400,
    resolved: 31_000,
    http_status: 202,
    polls: 60,
    pending_until: 30_600,
    holds: [call(50, 300, null), call(30_100, 30_400, true), call(31_500, 31_600, true)],
  });

  it('splits a lost answer into stages that follow on and add up to the latency', () => {
    const parts = stages(lost);
    const main = parts.filter((s) => s.kind !== 'later');
    expect(main.map((s) => [s.kind, s.to - s.from])).toEqual([
      ['lag', 10],
      ['work', 40],
      ['unanswered', 250],
      ['waiting', 29_800],
      ['held', 300],
      ['after', 200],
      ['polling', 400],
    ]);
    expect(main.reduce((sum, s) => sum + (s.to - s.from), 0)).toBe(latencyOf(lost));
    expect(parts.at(-1)).toMatchObject({ kind: 'later', from: 31_500, to: 31_600 });
    expect(pathOf(lost)).toBe('lost');
  });

  it('shows a call made alongside another at its real length, outside the total', () => {
    const twice = bid(0, {
      intended: 0,
      sent: 0,
      resolved: 1_000,
      http_status: 202,
      pending_until: 100,
      holds: [call(10, 200, null), call(500, 700, true), call(501, 650, true)],
    });
    const parts = stages(twice);
    expect(parts.find((s) => s.alongside)).toMatchObject({ kind: 'held', from: 501, to: 650 });
    expect(parts.filter((s) => !s.alongside).reduce((sum, s) => sum + (s.to - s.from), 0)).toBe(latencyOf(twice));
    // The last "pending" answer was sent before the hold succeeded, so there is no "still pending" stage.
    expect(parts.map((s) => s.kind)).not.toContain('after');
  });

  it('handles a bid decided without a hold', () => {
    expect(stages(bid(1)).map((s) => s.kind)).toEqual(['lag', 'work']);
    expect(pathOf(bid(1))).toBe('none');
    expect(pathOf(bid(1, { holds: [call(1, 2, false)] }))).toBe('refused');
  });

  it('puts each bid in one heatmap cell, slowest first', () => {
    expect(rowOf(9_999)).toBe(0);
    expect(rowOf(10_000)).toBe(1);
    const found = cells([bid(1, { resolved: 1_200 }), bid(2, { resolved: 2_900 }), lost]);
    expect(found.flatMap((c) => c.bids)).toHaveLength(3);
    expect(found.find((c) => c.bids.length === 2)?.bids.map((b) => b.seq)).toEqual([2, 1]);
  });
});

describe('what went wrong', () => {
  const hold = (op: Hold['op'], replied: number, ok: boolean | null, reason: string | null = null): Hold => ({
    op,
    received: replied - 5,
    replied,
    ok,
    reason,
    http_status: ok === null ? 503 : 200,
  });
  const send = (seq: number, over: Partial<Send>): Send => ({
    seq,
    retry_of: 1,
    amount: 1100,
    http_status: 201,
    error_code: null,
    changed: false,
    bid: 1,
    sent: 2000,
    replied: 2100,
    ...over,
  });
  const storm = {
    bids: [
      bid(1, { holds: [hold('authorize', 100, true), hold('cancel', 200, false, 'rate_limited'), hold('cancel', 900, true)] }),
      bid(2, { holds: [hold('authorize', 50, false, 'card_declined')] }),
      bid(3, { holds: [hold('authorize', 150, null), hold('authorize', 400, true), hold('cancel', 500, true)] }),
      won(4, 1, { holds: [hold('authorize', 300, true), hold('capture', 1000, true)] }),
    ],
    other_sends: [
      send(5, {}),
      send(6, { bid: null, changed: true, http_status: 422, error_code: 'idempotency_conflict' }),
    ],
  } as Storm;

  it('finds the cases of each fault', () => {
    const found = (['declined', 'provider', 'lost', 'release', 'retried', 'conflict'] as const).map(
      (f) => examples(storm, f).length,
    );
    expect(found).toEqual([1, 0, 1, 1, 1, 1]);
    const [conflict] = examples(storm, 'conflict');
    expect(conflict!.steps.map((s) => s.at)).toEqual([...conflict!.steps.map((s) => s.at)].sort((a, b) => a - b));
    expect(conflict!.steps.some((s) => s.what.includes('idempotency_conflict'))).toBe(true);
  });

  it('keeps a ledger that marks a hold as possible until the proxy sees it placed', () => {
    const { levels, ...totals } = ledger(storm);
    expect(totals).toEqual({ placed: 3, released: 2, captured: 1 });
    expect(levels.map((l) => [l.at, l.held, l.maybe])).toEqual([
      [0, 0, 0],
      [100, 1, 0],
      [150, 1, 1],
      [300, 2, 1],
      [400, 3, 0],
      [500, 2, 0],
      [900, 1, 0],
      [1000, 0, 0],
    ]);
  });
});

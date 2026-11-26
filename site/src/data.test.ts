import { describe, expect, it } from 'vitest';
import { acceptedBids, delays, groupOf, holdOf, raceWindow, type Bid, type Storm } from './data.ts';

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

// The shape of data/storm.json (analysis/src/bidcheck/site.py) and what the views derive from it.
// Times are microseconds from the first intended send.

export interface Hold {
  op: 'authorize' | 'cancel' | 'capture';
  received: number;
  replied: number;
  ok: boolean | null;
  reason: string | null;
  http_status: number | null;
}

export interface Bid {
  seq: number;
  bidder: number;
  amount: number; // cents
  status: 'accepted' | 'rejected' | 'lost_race';
  reason: string | null;
  intended: number;
  sent: number;
  replied: number;
  resolved: number | null;
  version: number | null; // accepted bids only
  heard: (number | null)[] | null; // when each subscriber heard it
  holds: Hold[];
}

export interface Storm {
  run: {
    id: string;
    started_us: number;
    target: string;
    target_commit: string;
    instances: number;
    fake_settings_declared: Record<string, string>;
    seed: number;
    rate_per_second: number;
    duration_us: number;
    auction_seconds: number;
    bidders: number;
    subscribers: number;
    cpus: number;
    send_lag_max_us: number;
  };
  sends: number;
  peak_in_flight: number;
  checks: { name: string; passed: boolean }[];
  bids: Bid[];
  other_sends: { seq: number; retry_of: number | null; amount: number; sent: number; replied: number }[];
  close: { version: number; amount: number | null; heard: (number | null)[] };
}

export interface Accepted extends Bid {
  version: number;
  heard: (number | null)[];
  firstHeard: number;
}

/** Why a bid did or didn't count, folded into the few groups the charts tell apart. */
export type Group = 'accepted' | 'price' | 'ended' | 'card' | 'other';

export function groupOf(bid: Bid): Group {
  if (bid.status === 'accepted') return 'accepted';
  if (bid.reason === 'below_minimum') return 'price';
  if (bid.reason === 'ended') return 'ended';
  if (bid.reason === 'card_declined' || bid.reason === 'processing_error') return 'card';
  return 'other';
}

export const GROUP_NAMES: Record<Group, string> = {
  accepted: 'Accepted',
  price: 'Too low',
  ended: 'Auction ended',
  card: 'Card hold failed',
  other: 'Other',
};

/** Accepted bids in the one agreed order. */
export function acceptedBids(storm: Storm): Accepted[] {
  return storm.bids
    .flatMap((bid) => {
      if (bid.status !== 'accepted' || bid.version === null || bid.heard === null) return [];
      const times = bid.heard.filter((t): t is number => t !== null);
      return [{ ...bid, version: bid.version, heard: bid.heard, firstHeard: Math.min(...times) }];
    })
    .sort((a, b) => a.version - b.version);
}

/** The successful authorization that covered a bid. */
export function holdOf(bid: Bid): Hold | undefined {
  return bid.holds.find((h) => h.op === 'authorize' && h.ok === true);
}

export interface Window {
  from: number;
  to: number;
  bids: Bid[];
}

/** The selected bid and every bid awaiting its reply while it was being decided. */
export function raceWindow(storm: Storm, selected: Accepted): Window {
  const bids = storm.bids.filter(
    (b) => b.seq === selected.seq || (b.sent <= selected.firstHeard && b.replied >= selected.sent),
  );
  // Wide enough for every lane from send to reply, so no bar is cut off.
  const from = Math.min(...bids.map((b) => b.sent));
  const to = Math.max(selected.firstHeard, ...bids.map((b) => b.replied));
  return { from: from - 20_000, to: to + 20_000, bids };
}

/** How long after the first subscriber each one heard a version. */
export function delays(heard: (number | null)[]): (number | null)[] {
  const first = Math.min(...heard.filter((t): t is number => t !== null));
  return heard.map((t) => (t === null ? null : t - first));
}

export const money = (cents: number): string => `$${(cents / 100).toFixed(2)}`;
export const seconds = (us: number): string => `${(us / 1e6).toFixed(2)} s`;
export const millis = (us: number): string => `${(us / 1e3).toFixed(us < 10_000 ? 1 : 0)} ms`;

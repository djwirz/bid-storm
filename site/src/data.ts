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
  resolved: number | null; // when a reply first showed the outcome
  http_status: number | null; // of the reply to the POST: 202 means "pending"
  polls: number;
  pending_until: number | null; // when the last request answered "pending" was sent
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
export const millis = (us: number): string =>
  us < 1e6 ? `${(us / 1e3).toFixed(us < 10_000 ? 1 : 0)} ms` : `${(us / 1e6).toFixed(1)} s`;

// Where a bid's time went (latency counts from the intended send, spec §5).

export const latencyOf = (bid: Bid): number => (bid.resolved ?? bid.replied) - bid.intended;

/** What happened to the card hold, which decides most of where the time goes. */
export type Path = 'none' | 'held' | 'refused' | 'lost';

export const PATH_NAMES: Record<Path, string> = {
  none: 'Decided without a card hold',
  held: 'Card hold succeeded',
  refused: 'Card hold refused',
  lost: 'A card hold’s answer was lost',
};

const authorizations = (bid: Bid) =>
  bid.holds.filter((h) => h.op === 'authorize').sort((a, b) => a.received - b.received);

export function pathOf(bid: Bid): Path {
  const calls = authorizations(bid);
  if (calls.length === 0) return 'none';
  if (calls.some((c) => c.ok === null)) return 'lost';
  return calls.at(-1)!.ok ? 'held' : 'refused';
}

export interface Stage {
  kind: 'lag' | 'work' | 'held' | 'refused' | 'unanswered' | 'waiting' | 'after' | 'polling' | 'later';
  label: string;
  from: number;
  to: number;
  /** Ran at the same time as an earlier stage, so it adds nothing to the total. */
  alongside?: boolean;
}

const callStage = (call: Hold): Pick<Stage, 'kind' | 'label'> => {
  if (call.ok === true) return { kind: 'held', label: 'Card hold call: held' };
  if (call.ok === false) return { kind: 'refused', label: `Card hold call: refused (${call.reason ?? 'no reason'})` };
  return { kind: 'unanswered', label: `Card hold call: no answer (HTTP ${call.http_status ?? 'none'})` };
};

/**
 * The bid's time from when it was due until its outcome was known, in stages that follow on
 * from each other. A card call that overlapped an earlier one is listed "alongside" with its
 * own times, and calls made after the outcome was known are listed last, as "later".
 */
export function stages(bid: Bid): Stage[] {
  const end = bid.intended + latencyOf(bid);
  const calls = authorizations(bid);
  const inTime = calls.filter((c) => c.received < end);
  const out: Stage[] = [{ kind: 'lag', label: 'Generator running late', from: bid.intended, to: bid.sent }];
  let at = bid.sent;
  const add = (kind: Stage['kind'], label: string, to: number) => {
    const until = Math.min(end, Math.max(at, to));
    out.push({ kind, label, from: at, to: until });
    at = until;
  };
  if (inTime.length === 0) {
    add('work', 'The target deciding, and the reply', end);
  } else {
    add('work', 'Target work before the hold, any lock wait included', inTime[0]!.received);
    inTime.forEach((call, i) => {
      const { kind, label } = callStage(call);
      if (i > 0 && call.received < at) {
        out.push({ kind, label: `${label}, at the same time`, from: call.received, to: call.replied, alongside: true });
        at = Math.min(end, Math.max(at, call.replied));
        return;
      }
      if (i > 0 && call.received > at) add('waiting', 'Waiting before the target asked again', call.received);
      add(kind, label, call.replied);
    });
    if (bid.http_status === 202) {
      if ((bid.pending_until ?? at) > at) add('after', 'Still pending', bid.pending_until!);
      add('polling', 'Settled somewhere in here: a poll then saw it', end);
    } else {
      add('after', 'Target work after the hold, and the reply', end);
    }
  }
  for (const call of calls.filter((c) => c.received >= end)) {
    out.push({ kind: 'later', label: `After the outcome was known. ${callStage(call).label}`, from: call.received, to: call.replied });
  }
  return out;
}

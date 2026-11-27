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

/** A send that made no new bid: a repeat answered with the original, or one refused. */
export interface Send {
  seq: number;
  retry_of: number | null;
  amount: number;
  http_status: number | null;
  error_code: string | null;
  changed: boolean;
  bid: number | null; // seq of the bid it got back
  sent: number;
  replied: number;
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
    retry_rate: number;
    conflict_rate: number;
    subscribers: number;
    cpus: number;
    send_lag_max_us: number;
  };
  sends: number;
  peak_in_flight: number;
  checks: { name: string; passed: boolean }[];
  bids: Bid[];
  other_sends: Send[];
  fake: { placed: number; released: number; captured: number; held: number };
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

// What went wrong, and what became of the holds.

export type Fault = 'declined' | 'provider' | 'lost' | 'release' | 'retried' | 'conflict';

export interface Step {
  at: number;
  what: string;
}

export interface Example {
  title: string;
  steps: Step[];
  bid: Bid | null;
}

const HOLD_VERBS = { authorize: 'to hold the card', cancel: 'to release the hold', capture: 'to take the money' };

function holdSteps(hold: Hold): Step[] {
  const answer =
    hold.ok === true
      ? 'Yes'
      : hold.ok === false
        ? `No: ${hold.reason ?? 'no reason given'}`
        : `No answer (HTTP ${hold.http_status ?? 'none'})`;
  return [
    { at: hold.received, what: `The target asks the fake ${HOLD_VERBS[hold.op]}` },
    { at: hold.replied, what: answer },
  ];
}

const REASONS: Record<string, string> = {
  below_minimum: 'too low',
  ended: 'the auction had ended',
  card_declined: 'the card was declined',
  processing_error: 'a processing error',
};

/** Everything recorded about one bid, in time order. */
export function story(bid: Bid): Step[] {
  const steps: Step[] = [{ at: bid.sent, what: `Bidder ${bid.bidder} sends ${money(bid.amount)}` }];
  steps.push(...bid.holds.flatMap(holdSteps));
  if (bid.http_status === 202) steps.push({ at: bid.replied, what: 'The bidder is told “pending”' });
  const outcome =
    bid.status === 'accepted'
      ? `accepted as version ${bid.version}`
      : `not accepted, ${REASONS[bid.reason ?? ''] ?? bid.reason ?? 'no reason given'}`;
  steps.push({ at: bid.resolved ?? bid.replied, what: `The bidder learns the outcome: ${outcome}` });
  return steps.sort((a, b) => a.at - b.at);
}

const bidExample = (bid: Bid): Example => ({
  title: `${money(bid.amount)} by bidder ${bid.bidder}`,
  steps: story(bid),
  bid,
});

const answered = (bid: Bid, reason: string) => bid.holds.some((h) => h.op === 'authorize' && h.reason === reason);

function sendExample(storm: Storm, send: Send): Example {
  // The bid whose key was reused: the one returned, or the one the refused send retried.
  const original = storm.bids.find((b) => b.seq === (send.bid ?? send.retry_of));
  const result =
    send.bid !== null
      ? 'It gets back the original bid'
      : `It is refused (HTTP ${send.http_status ?? 'none'}${send.error_code ? `, ${send.error_code}` : ''})`;
  const steps = [
    ...(original ? story(original) : []),
    { at: send.sent, what: `The same key is sent again, offering ${money(send.amount)}` },
    { at: send.replied, what: result },
  ].sort((a, b) => a.at - b.at);
  return { title: `${money(send.amount)}, reusing a key`, steps, bid: original ?? null };
}

/** The recorded cases of one kind of fault, in the order they were sent. */
export function examples(storm: Storm, fault: Fault): Example[] {
  const bids = (test: (b: Bid) => boolean) => storm.bids.filter(test).map(bidExample);
  switch (fault) {
    case 'declined':
      return bids((b) => answered(b, 'card_declined'));
    case 'provider':
      return bids((b) => answered(b, 'processing_error'));
    case 'lost':
      return bids((b) => pathOf(b) === 'lost');
    case 'release':
      return bids((b) => b.holds.some((h) => h.op === 'cancel' && h.ok === false));
    case 'retried':
      return storm.other_sends.filter((s) => s.bid !== null).map((s) => sendExample(storm, s));
    case 'conflict':
      return storm.other_sends.filter((s) => s.http_status === 422).map((s) => sendExample(storm, s));
  }
}

export interface Held {
  /** A call's answer was lost: from then, a hold may exist that the target doesn't know of. */
  maybeFrom: number | null;
  placed: number | null;
  released: number | null;
  captured: number | null;
}

/** When a bid's hold was placed and what ended it, as the proxy saw. */
export function heldFor(bid: Bid): Held {
  const first = (op: Hold['op'], ok: boolean | null) =>
    bid.holds
      .filter((h) => h.op === op && h.ok === ok)
      .map((h) => h.replied)
      .sort((a, b) => a - b)[0] ?? null;
  return {
    maybeFrom: first('authorize', null),
    placed: first('authorize', true),
    released: first('cancel', true),
    captured: first('capture', true),
  };
}

export interface Level {
  at: number;
  held: number; // holds the proxy has seen placed and not yet ended
  maybe: number; // possibly held: the answer was lost and not yet repeated
}

export interface Ledger {
  levels: Level[];
  placed: number;
  released: number;
  captured: number;
}

export function ledger(storm: Storm): Ledger {
  const changes: { at: number; held: number; maybe: number }[] = [];
  const totals = { placed: 0, released: 0, captured: 0 };
  for (const bid of storm.bids) {
    const h = heldFor(bid);
    if (h.placed === null) continue;
    totals.placed++;
    if (h.maybeFrom !== null && h.maybeFrom < h.placed) {
      changes.push({ at: h.maybeFrom, held: 0, maybe: 1 }, { at: h.placed, held: 1, maybe: -1 });
    } else {
      changes.push({ at: h.placed, held: 1, maybe: 0 });
    }
    const ended = h.released ?? h.captured;
    if (h.released !== null) totals.released++;
    else if (h.captured !== null) totals.captured++;
    if (ended !== null) changes.push({ at: ended, held: -1, maybe: 0 });
  }
  const levels: Level[] = [{ at: 0, held: 0, maybe: 0 }];
  for (const change of changes.sort((a, b) => a.at - b.at)) {
    const last = levels.at(-1)!;
    const next = { at: change.at, held: last.held + change.held, maybe: last.maybe + change.maybe };
    if (last.at === next.at) levels[levels.length - 1] = next;
    else levels.push(next);
  }
  return { levels, ...totals };
}

// The page's text about this particular storm and about what each check does and doesn't show.

import { acceptedBids, money, type Storm } from './data.ts';

export const CHECKS: Record<string, { title: string; limit: string }> = {
  usable: {
    title: 'The recording can be used',
    limit: 'The generator kept to its schedule on this machine. It says nothing about another.',
  },
  nothing_lost: {
    title: 'No lines went missing',
    limit: 'Compared with the recorder’s own counts, not with an independent one.',
  },
  timelines: {
    title: 'Every bid’s story is whole',
    limit: 'Whole as seen from outside. What happened inside the target isn’t recorded.',
  },
  keys: {
    title: 'A retried key never made a second bid',
    limit: 'Only the retries this schedule happened to send.',
  },
  hold_first: {
    title: 'Every accepted bid was held before anyone heard of it',
    limit: 'The hold as the proxy saw it, against the stream as subscribers got it.',
  },
  amounts: {
    title: 'Announcements match accepted bids one for one',
    limit: 'Stream events name no bid, so they are matched by amount.',
  },
  streams: {
    title: 'Every subscriber heard the same events in the same order',
    limit: 'A handful of subscribers on one machine, none of which had to reconnect here.',
  },
  audit: {
    title: 'The payment fake’s own record agrees',
    limit: 'A stand-in for Stripe, not Stripe.',
  },
  target_verdict: {
    title: 'The target’s own checker found no violation',
    limit: 'One run’s verdict, not how often it would pass.',
  },
};

export function summary(storm: Storm): string {
  const { run } = storm;
  const accepted = acceptedBids(storm);
  const price = storm.close.amount === null ? '' : ` It closed at ${money(storm.close.amount)}.`;
  return (
    `${run.bidders} synthetic bidders sent ${storm.sends} bid requests over about ` +
    `${Math.round(run.duration_us / 1e6)} seconds at an auction that lasted ${run.auction_seconds}, with up to ` +
    `${storm.peak_in_flight} waiting for a reply at once. ${accepted.length} bids were accepted, ` +
    `in one agreed order.${price}`
  );
}

export function limits(storm: Storm): string[] {
  const { run } = storm;
  const latency = run.fake_settings_declared['FAKE_LATENCY_MS'];
  return [
    'This is one run. It is the first of three recorded in a row, all of which passed these checks; ' +
      'three passes say little about how often a run would fail.',
    `Load generator, target (${run.instances} instances), payment fake, database and subscribers all shared one ` +
      `${run.cpus}-core laptop. Nothing here is a network or capacity measurement.`,
    `Card holds go to a fake that waits ${latency ?? 'an unstated'} ms before answering, as declared by whoever ` +
      'started it. That wait is most of the latency you see.',
    'Everything is observed from outside the target. Time spent waiting on a database lock can’t be told apart.',
  ];
}

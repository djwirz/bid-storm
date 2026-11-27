import './style.css';
import { agreement } from './agreement.ts';
import { acceptedBids, GROUP_NAMES, groupOf, holdOf, money, seconds, type Group, type Storm } from './data.ts';
import { element, node, table } from './dom.ts';
import { laneLegend, lanes } from './lanes.ts';
import { latency } from './latency.ts';
import { staircase, swatch } from './staircase.ts';
import { CHECKS, limits, summary } from './words.ts';

function render(storm: Storm): void {
  const accepted = acceptedBids(storm);
  element('summary').textContent = summary(storm);

  element('checks').replaceChildren(
    ...storm.checks.map(({ name, passed }) => {
      const words = CHECKS[name] ?? { title: name, limit: '' };
      const item = node('li', '', passed ? 'passed' : 'failed');
      item.append(node('strong', words.title), node('span', passed ? '✓ passed' : '✗ failed', 'verdict'), node('small', words.limit));
      return item;
    }),
  );

  const counts = new Map<Group, number>();
  for (const bid of storm.bids) counts.set(groupOf(bid), (counts.get(groupOf(bid)) ?? 0) + 1);
  element('legend').replaceChildren(
    ...(Object.keys(GROUP_NAMES) as Group[])
      .filter((group) => counts.has(group))
      .map((group) => {
        const item = node('li');
        item.append(swatch(group), `${GROUP_NAMES[group]}: ${counts.get(group)}`);
        return item;
      }),
  );

  let current = 0;
  let highlight: (index: number) => void = () => {};
  const select = (index: number) => {
    current = Math.max(0, Math.min(accepted.length - 1, index));
    const bid = accepted[current]!;
    element('selected').textContent =
      `${current + 1} of ${accepted.length}: ${money(bid.amount)} by bidder ${bid.bidder}, version ${bid.version}, heard at ${seconds(bid.firstHeard)}`;
    highlight(current);
    lanes(element('lanes'), element('lane-table'), storm, bid);
  };
  element('previous').addEventListener('click', () => select(current - 1));
  element('next').addEventListener('click', () => select(current + 1));
  laneLegend(element('lane-legend'));

  let timed: number | null = null; // the bid whose waterfall is showing
  const draw = () => {
    element('staircase').replaceChildren();
    element('agreement').replaceChildren();
    highlight = staircase(element('staircase'), element('tooltip'), storm, select);
    agreement(element('agreement'), element('agreement-words'), storm);
    select(current);
    latency(storm, timed, (seq) => (timed = seq));
  };
  // Start where the most bids were competing.
  const busiest = accepted.map((a) => storm.bids.filter((b) => b.sent <= a.firstHeard && b.replied >= a.sent).length);
  current = busiest.indexOf(Math.max(...busiest));
  draw();

  let width = window.innerWidth;
  window.addEventListener('resize', () => {
    // Phones resize on scroll as their toolbars move; only a new width needs a redraw.
    if (window.innerWidth !== width) {
      width = window.innerWidth;
      draw();
    }
  });

  element('accepted-table').replaceChildren(
    table(
      'Accepted bids in the agreed order',
      ['Version', 'Amount', 'Bidder', 'Sent', 'Hold finished', 'First heard'],
      accepted.map((a) => {
        const held = holdOf(a);
        return [String(a.version), money(a.amount), String(a.bidder), seconds(a.sent), held ? seconds(held.replied) : 'none', seconds(a.firstHeard)];
      }),
    ),
  );
  element('limits').replaceChildren(...limits(storm).map((text) => node('li', text)));
}

async function load(): Promise<void> {
  const response = await fetch('./data/storm.json');
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  render((await response.json()) as Storm);
}

load().catch((error: unknown) => {
  console.error(error);
  element('summary').textContent = 'The recording could not be loaded.';
});

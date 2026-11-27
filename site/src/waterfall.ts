// One bid's time, stage by stage, on a true linear scale: a long wait looks long.

import { latencyOf, millis, money, pathOf, PATH_NAMES, seconds, stages, type Bid } from './data.ts';
import { node } from './dom.ts';

export function waterfall(host: HTMLElement, bid: Bid): void {
  const parts = stages(bid);
  const from = bid.intended;
  const to = Math.max(...parts.map((s) => s.to), ...(bid.heard ?? []).map((t) => t ?? 0));
  const share = (us: number) => `${(100 * Math.max(0, us)) / Math.max(1, to - from)}%`;

  const made = node('table', '', 'waterfall');
  made.append(
    node(
      'caption',
      `${money(bid.amount)} by bidder ${bid.bidder}, due at ${seconds(bid.intended)}. ${PATH_NAMES[pathOf(bid)]}. ` +
        `Outcome known after ${millis(latencyOf(bid))}` +
        (bid.http_status === 202 ? `, after ${bid.polls} polls.` : '.'),
    ),
  );
  const head = made.createTHead().insertRow();
  for (const name of ['Stage', 'Took', 'When']) {
    const cell = node('th', name);
    cell.scope = 'col';
    head.append(cell);
  }
  const body = made.createTBody();
  for (const stage of parts) {
    const row = body.insertRow();
    row.dataset['kind'] = stage.kind;
    row.insertCell().textContent = stage.label;
    row.insertCell().textContent = millis(stage.to - stage.from);
    const track = row.insertCell();
    track.className = 'track';
    track.setAttribute('aria-label', `${seconds(stage.from)} to ${seconds(stage.to)}`);
    const bar = node('span', '', `bar ${stage.kind}`);
    // Kept inside the track, so a short stage at the very end still shows.
    bar.style.left = `min(${share(stage.from - from)}, calc(100% - 2px))`;
    bar.style.width = share(stage.to - stage.from);
    track.append(bar);
  }
  const notes = [];
  if (bid.http_status === 202) {
    notes.push(`The bidder was first told "pending", ${millis(bid.replied - bid.intended)} after the bid was due.`);
  }
  const heard = (bid.heard ?? []).filter((t): t is number => t !== null);
  if (heard.length) notes.push(`The first subscriber heard it ${millis(Math.min(...heard) - bid.intended)} after it was due.`);
  host.replaceChildren(made, ...notes.map((text) => node('p', text, 'note')));
}

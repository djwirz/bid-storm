import * as d3 from 'd3';
import { GROUP_NAMES, groupOf, holdOf, money, raceWindow, seconds, type Accepted, type Bid, type Storm } from './data.ts';
import { node, table } from './dom.ts';

const outcome = (b: Bid) =>
  b.status === 'accepted' ? `accepted v${b.version}` : GROUP_NAMES[groupOf(b)].toLowerCase();

export function lanes(host: HTMLElement, tableHost: HTMLElement, storm: Storm, selected: Accepted): void {
  const view = raceWindow(storm, selected);
  const width = Math.max(560, host.clientWidth);
  const row = 20;
  const margin = { top: 26, right: 12, bottom: 30, left: 170 };
  const height = margin.top + view.bids.length * row + margin.bottom;
  const x = d3.scaleLinear().domain([view.from, view.to]).range([margin.left, width - margin.right]);
  const clamp = (t: number) => Math.max(margin.left, Math.min(width - margin.right, x(t)));

  host.replaceChildren();
  const svg = d3.select(host).append('svg').attr('width', width).attr('height', height)
    .attr('viewBox', `0 0 ${width} ${height}`).attr('role', 'img')
    .attr('aria-label', `${view.bids.length} bids in flight around the accepted bid of ${money(selected.amount)}. The table below lists them.`);
  svg.append('g').attr('class', 'axis').attr('transform', `translate(0,${height - margin.bottom})`)
    .call(d3.axisBottom(x).ticks(6).tickFormat((v) => `${(Number(v) / 1e6).toFixed(2)}s`));

  const lane = svg.append('g').selectAll('g').data(view.bids).join('g')
    .attr('class', (b) => (b.seq === selected.seq ? 'lane selected' : 'lane'))
    .attr('transform', (_, i) => `translate(0,${margin.top + i * row + row / 2})`);
  lane.append('text').attr('class', 'lane-label').attr('x', margin.left - 8).attr('dy', '0.32em')
    .attr('text-anchor', 'end').text((b) => `${money(b.amount)} ${outcome(b)}`);
  lane.append('line').attr('class', 'waiting').attr('x1', (b) => clamp(b.sent)).attr('x2', (b) => clamp(b.replied));
  lane.each(function (b) {
    const g = d3.select(this);
    for (const hold of b.holds.filter((h) => h.op === 'authorize')) {
      if (hold.replied < view.from || hold.received > view.to) continue;
      g.append('rect').attr('class', hold.ok ? 'hold ok' : 'hold failed').attr('y', -5).attr('height', 10).attr('rx', 2)
        .attr('x', clamp(hold.received)).attr('width', Math.max(2, clamp(hold.replied) - clamp(hold.received)))
        .append('title').text(`${hold.ok ? 'held' : (hold.reason ?? 'no answer')}: ${seconds(hold.received)} to ${seconds(hold.replied)}`);
    }
    for (const heard of b.heard ?? []) {
      if (heard !== null && heard >= view.from && heard <= view.to) {
        g.append('path').attr('class', 'mark accepted').attr('d', d3.symbol(d3.symbolDiamond, 40)())
          .attr('transform', `translate(${x(heard)},0)`);
      }
    }
    if (b.replied <= view.to) g.append('line').attr('class', 'reply').attr('x1', x(b.replied)).attr('x2', x(b.replied)).attr('y1', -6).attr('y2', 6);
  });

  svg.append('line').attr('class', 'announced').attr('x1', x(selected.firstHeard)).attr('x2', x(selected.firstHeard))
    .attr('y1', margin.top - 6).attr('y2', height - margin.bottom);
  svg.append('text').attr('class', 'axis-label').attr('x', x(selected.firstHeard)).attr('y', 12)
    .attr('text-anchor', 'middle').text('first subscriber hears it');

  tableHost.replaceChildren(
    table(
      `Bids in flight around ${money(selected.amount)}`,
      ['Offered', 'Bidder', 'Outcome', 'Sent', 'Hold finished', 'Replied'],
      view.bids.map((b) => {
        const held = holdOf(b);
        return [money(b.amount), String(b.bidder), outcome(b), seconds(b.sent), held ? seconds(held.replied) : 'none', seconds(b.replied)];
      }),
    ),
  );
}

export function laneLegend(host: HTMLElement): void {
  const items: [string, string][] = [
    ['key waiting', 'sent, waiting for a reply'],
    ['key hold-ok', 'card hold succeeded'],
    ['key hold-failed', 'card hold failed or unanswered'],
    ['key reply', 'reply received'],
    ['key heard', 'a subscriber heard it'],
  ];
  host.replaceChildren(
    ...items.map(([className, text]) => {
      const item = node('li');
      const key = node('span', '', className);
      key.setAttribute('aria-hidden', 'true');
      item.append(key, text);
      return item;
    }),
  );
}

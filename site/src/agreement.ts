import * as d3 from 'd3';
import { acceptedBids, delays, millis, money, type Storm } from './data.ts';

export function agreement(host: HTMLElement, words: HTMLElement, storm: Storm): void {
  const events = [
    ...acceptedBids(storm).map((a) => ({ version: a.version, what: money(a.amount), heard: a.heard })),
    { version: storm.close.version, what: 'close', heard: storm.close.heard },
  ];
  const cells = events.flatMap((e) =>
    delays(e.heard).map((delay, subscriber) => ({ ...e, subscriber, delay })),
  );
  const spread = events.map((e) => d3.max(delays(e.heard), (d) => d ?? 0) ?? 0).sort(d3.ascending);
  const passed = storm.checks.find((c) => c.name === 'streams')?.passed;
  words.textContent =
    `${passed ? 'Yes' : 'No'}: ${storm.run.subscribers} subscribers, ${events.length} events each. ` +
    `Between the first and the last to hear an event there was typically ${millis(d3.median(spread) ?? 0)}, ` +
    `at most ${millis(spread.at(-1) ?? 0)}.`;

  const left = 94;
  const cell = Math.max(10, Math.min(22, (Math.max(320, host.clientWidth) - left) / events.length));
  const width = left + cell * events.length;
  const row = 22;
  const height = row * storm.run.subscribers + 34;
  const shade = d3.scaleLinear<number>().domain([0, d3.max(cells, (c) => c.delay ?? 0) || 1]).range([0.2, 1]);

  const svg = d3.select(host).append('svg').attr('width', width).attr('height', height)
    .attr('viewBox', `0 0 ${width} ${height}`).attr('role', 'img')
    .attr('aria-label', words.textContent);
  svg.append('g').selectAll('text').data(d3.range(storm.run.subscribers)).join('text').attr('class', 'lane-label')
    .attr('x', left - 8).attr('y', (s) => s * row + row / 2).attr('dy', '0.32em').attr('text-anchor', 'end')
    .text((s) => `subscriber ${s + 1}`);
  svg.append('g').selectAll('rect').data(cells).join('rect')
    .attr('class', (c) => (c.delay === null ? 'cell missing' : 'cell'))
    .attr('x', (c) => left + events.findIndex((e) => e.version === c.version) * cell + 1)
    .attr('y', (c) => c.subscriber * row + 2).attr('width', cell - 2).attr('height', row - 4).attr('rx', 2)
    .attr('fill-opacity', (c) => (c.delay === null ? 0 : shade(c.delay)))
    .append('title')
    .text((c) => `version ${c.version} (${c.what}), subscriber ${c.subscriber + 1}: ${c.delay === null ? 'never heard' : `${millis(c.delay)} after the first`}`);
  svg.append('text').attr('class', 'axis-label').attr('x', left).attr('y', height - 10)
    .text(`versions ${events[0]?.version} to ${events.at(-1)?.version}, in order →`);
}

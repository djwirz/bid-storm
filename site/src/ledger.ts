// Holds open over time, and the totals beside the fake's own record.

import * as d3 from 'd3';
import { ledger, seconds, type Level, type Storm } from './data.ts';
import { element, table } from './dom.ts';

export function holdLedger(storm: Storm): void {
  const { levels, placed, released, captured } = ledger(storm);
  const open = placed - released - captured;
  element('ledger-totals').replaceChildren(
    table(
      'Two records of the holds',
      ['Record', 'Placed', 'Released', 'Captured', 'Left'],
      [
        ['Proxy', String(placed), String(released), String(captured), String(open)],
        ['Fake’s record', ...(['placed', 'released', 'captured', 'held'] as const).map((k) => String(storm.fake[k]))],
      ],
    ),
  );

  const host = element('ledger');
  host.replaceChildren();
  const width = Math.max(320, host.clientWidth);
  const height = 240;
  const margin = { top: 12, right: 12, bottom: 38, left: 36 };
  const x = d3.scaleLinear().domain([0, levels.at(-1)!.at]).range([margin.left, width - margin.right]);
  const y = d3.scaleLinear().domain([0, d3.max(levels, (l) => l.held + l.maybe) ?? 1]).nice().range([height - margin.bottom, margin.top]);
  const peak = d3.greatest(levels, (l) => l.held + l.maybe)!;

  const svg = d3.select(host).append('svg').attr('viewBox', `0 0 ${width} ${height}`).attr('width', width).attr('height', height)
    .attr('role', 'img')
    .attr('aria-label', `Holds open over time: at most ${peak.held + peak.maybe} at once, ${open} at the end. The tables give the numbers.`);
  svg.append('g').attr('class', 'grid').attr('transform', `translate(${margin.left},0)`)
    .call(d3.axisLeft(y).ticks(5).tickSize(-(width - margin.left - margin.right)));
  svg.append('g').attr('class', 'axis').attr('transform', `translate(0,${height - margin.bottom})`)
    .call(d3.axisBottom(x).ticks(width < 600 ? 5 : 10).tickFormat((v) => `${Number(v) / 1e6}s`));
  svg.append('text').attr('class', 'axis-label').attr('x', width - margin.right).attr('y', height - 4)
    .attr('text-anchor', 'end').text('time since the first bid was due');
  const area = (low: (l: Level) => number, high: (l: Level) => number) =>
    d3.area<Level>().curve(d3.curveStepAfter).x((l) => x(l.at)).y0((l) => y(low(l))).y1((l) => y(high(l)));
  svg.append('path').attr('class', 'held-area').attr('d', area(() => 0, (l) => l.held)(levels));
  svg.append('path').attr('class', 'maybe-area').attr('d', area((l) => l.held, (l) => l.held + l.maybe)(levels));

  // Sampled each second, for reading without the chart.
  const at = d3.bisector((l: Level) => l.at).right;
  const samples = d3.range(0, Math.ceil(levels.at(-1)!.at / 1e6) + 1).map((s) => levels[Math.max(0, at(levels, s * 1e6) - 1)]!);
  element('ledger-table').replaceChildren(
    table(
      'Holds open at each second',
      ['Time', 'Held', 'Possibly held'],
      samples.map((l, s) => [seconds(s * 1e6), String(l.held), String(l.maybe)]),
    ),
  );
}

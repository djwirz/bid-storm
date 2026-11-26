import * as d3 from 'd3';
import { acceptedBids, GROUP_NAMES, groupOf, money, seconds, type Bid, type Group, type Storm } from './data.ts';

// Shape and colour both say why: neither is the only cue.
export const MARKS: Record<Group, d3.SymbolType> = {
  accepted: d3.symbolDiamond,
  price: d3.symbolCircle,
  ended: d3.symbolSquare,
  card: d3.symbolTriangle,
  other: d3.symbolStar,
};

export function swatch(group: Group): SVGSVGElement {
  const svg = d3.create('svg').attr('viewBox', '-7 -7 14 14').attr('width', 14).attr('height', 14);
  svg.attr('aria-hidden', 'true');
  svg.append('path').attr('class', `mark ${group}`).attr('d', d3.symbol(MARKS[group], 60)());
  return svg.node()!;
}

/** Draws the staircase and returns a function that highlights one accepted bid. */
export function staircase(
  host: HTMLElement,
  tooltip: HTMLElement,
  storm: Storm,
  select: (index: number) => void,
): (index: number) => void {
  const accepted = acceptedBids(storm);
  const width = Math.max(320, host.clientWidth);
  const height = width < 600 ? 320 : 440;
  const margin = { top: 20, right: 12, bottom: 38, left: 52 };
  const closed = storm.close.heard.filter((t): t is number => t !== null);
  const closeAt = closed.length ? Math.min(...closed) : null;

  const x = d3
    .scaleLinear()
    .domain([0, Math.max(d3.max(storm.bids, (b) => b.sent) ?? 0, closeAt ?? 0)])
    .range([margin.left, width - margin.right]);
  const y = d3
    .scaleLinear()
    .domain(d3.extent(storm.bids, (b) => b.amount) as [number, number])
    .nice()
    .range([height - margin.bottom, margin.top]);

  const svg = d3
    .select(host)
    .append('svg')
    .attr('viewBox', `0 0 ${width} ${height}`)
    .attr('role', 'img')
    .attr(
      'aria-label',
      `Price staircase: ${accepted.length} accepted bids and ${storm.bids.length - accepted.length} that did not count, over time. Use the previous and next buttons, or the tables below.`,
    );

  if (closeAt !== null) {
    svg.append('rect').attr('class', 'after-close').attr('x', x(closeAt)).attr('y', margin.top)
      .attr('width', Math.max(0, width - margin.right - x(closeAt))).attr('height', height - margin.top - margin.bottom);
    svg.append('text').attr('class', 'axis-label').attr('x', x(closeAt) + 4).attr('y', margin.top - 6).text('closed');
  }
  svg.append('g').attr('class', 'grid').attr('transform', `translate(${margin.left},0)`)
    .call(d3.axisLeft(y).ticks(6).tickSize(-(width - margin.left - margin.right)).tickFormat((v) => `$${Number(v) / 100}`));
  svg.append('g').attr('class', 'axis').attr('transform', `translate(0,${height - margin.bottom})`)
    .call(d3.axisBottom(x).ticks(width < 600 ? 5 : 10).tickFormat((v) => `${Number(v) / 1e6}s`));
  svg.append('text').attr('class', 'axis-label').attr('x', width - margin.right).attr('y', height - 4)
    .attr('text-anchor', 'end').text('time since the first bid was due');

  const at = (b: Bid) => {
    const step = b.status === 'accepted' ? accepted.find((a) => a.seq === b.seq) : undefined;
    return [x(step ? step.firstHeard : b.sent), y(b.amount)] as const;
  };

  svg.append('g').selectAll('path')
    .data(storm.bids.filter((b) => b.status !== 'accepted'))
    .join('path')
    .attr('class', (b) => `mark ${groupOf(b)}`)
    .attr('d', (b) => d3.symbol(MARKS[groupOf(b)], 22)())
    .attr('transform', (b) => `translate(${at(b).join(',')})`);

  const end = closeAt ?? x.domain()[1]!;
  const line = d3.line<[number, number]>().curve(d3.curveStepAfter).x((d) => x(d[0])).y((d) => y(d[1]));
  const last = accepted.at(-1);
  svg.append('path').attr('class', 'stairs').attr(
    'd',
    line([...accepted.map((a): [number, number] => [a.firstHeard, a.amount]), ...(last ? [[end, last.amount] as [number, number]] : [])]),
  );
  svg.append('g').selectAll('path').data(accepted).join('path').attr('class', 'mark accepted step')
    .attr('d', d3.symbol(MARKS.accepted, 55)()).attr('transform', (a) => `translate(${at(a).join(',')})`);

  const ring = svg.append('circle').attr('class', 'ring').attr('r', 9);

  // The nearest mark to the pointer, so small marks don't need to be hit exactly.
  const points = d3.Delaunay.from(storm.bids, (b) => at(b)[0], (b) => at(b)[1]);
  const nearest = (event: PointerEvent): Bid | undefined => {
    const [px, py] = d3.pointer(event);
    const bid = storm.bids[points.find(px, py)];
    return bid && Math.hypot(at(bid)[0] - px, at(bid)[1] - py) < 24 ? bid : undefined;
  };
  svg.on('pointermove', (event: PointerEvent) => {
    const bid = nearest(event);
    tooltip.hidden = !bid;
    if (!bid) return;
    const why = bid.status === 'accepted' ? `accepted, version ${bid.version}` : `${GROUP_NAMES[groupOf(bid)].toLowerCase()} (${bid.reason})`;
    tooltip.textContent = `${money(bid.amount)} · bidder ${bid.bidder} · ${why} · sent at ${seconds(bid.sent)}`;
    tooltip.style.left = `${event.pageX}px`;
    tooltip.style.top = `${event.pageY}px`;
  });
  svg.on('pointerleave', () => (tooltip.hidden = true));
  svg.on('click', (event: PointerEvent) => {
    const bid = nearest(event);
    const index = accepted.findIndex((a) => a.seq === bid?.seq);
    if (index >= 0) select(index);
  });

  return (index) => {
    const bid = accepted[index];
    if (bid) ring.attr('cx', at(bid)[0]).attr('cy', at(bid)[1]);
  };
}

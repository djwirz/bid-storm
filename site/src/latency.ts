// Where the time went: a table by path, a heatmap to pick a bid from, and the pick's waterfall.

import * as d3 from 'd3';
import { latencyOf, millis, money, pathOf, PATH_NAMES, seconds, type Bid, type Path, type Storm } from './data.ts';
import { element, node, table } from './dom.ts';
import { waterfall } from './waterfall.ts';

// Row edges in milliseconds: finer where most bids fall, so not equal steps.
export const ROWS = [0, 10, 20, 30, 50, 70, 100, 150, 200, 300, 400, 500, 700, 1000, 2000, 5000, 10_000, 20_000, 30_000, 40_000];
const COLUMN_US = 250_000;

const ms = (edge: number) => (edge < 1000 ? `${edge} ms` : `${edge / 1000} s`);
const rowName = (row: number) => (row === ROWS.length - 1 ? `over ${ms(ROWS[row]!)}` : `${ms(ROWS[row]!)} to ${ms(ROWS[row + 1]!)}`);
export const rowOf = (us: number) => d3.bisectRight(ROWS, us / 1000) - 1;

export interface Cell {
  column: number;
  row: number;
  bids: Bid[]; // slowest first
}

export function cells(bids: Bid[]): Cell[] {
  const found = new Map<string, Cell>();
  for (const bid of [...bids].sort((a, b) => latencyOf(b) - latencyOf(a))) {
    const column = Math.max(0, Math.floor(bid.intended / COLUMN_US));
    const row = rowOf(latencyOf(bid));
    const key = `${column}:${row}`;
    const cell = found.get(key) ?? { column, row, bids: [] };
    cell.bids.push(bid);
    found.set(key, cell);
  }
  return [...found.values()].sort((a, b) => a.column - b.column || a.row - b.row);
}

const describe = (bid: Bid) => `${money(bid.amount)} by bidder ${bid.bidder}, due at ${seconds(bid.intended)}: ${millis(latencyOf(bid))}`;

function pathTable(storm: Storm): HTMLTableElement {
  const rows = (Object.keys(PATH_NAMES) as Path[]).flatMap((path) => {
    const times = storm.bids.filter((b) => pathOf(b) === path).map(latencyOf).sort(d3.ascending);
    if (times.length === 0) return [];
    return [[PATH_NAMES[path], String(times.length), millis(d3.median(times)!), millis(times.at(-1)!)]];
  });
  return table('Time until the outcome was known, by what happened to the card hold', ['Path', 'Bids', 'Typical', 'Slowest'], rows);
}

function bidButton(bid: Bid, pick: (bid: Bid) => void): HTMLButtonElement {
  const button = node('button', describe(bid));
  button.type = 'button';
  button.dataset['seq'] = String(bid.seq);
  button.addEventListener('click', () => pick(bid));
  return button;
}

/** Draws the section's heatmap; call again when the width changes. */
export function latency(storm: Storm, selectedSeq: number | null, onPick: (seq: number) => void): void {
  element('path-table').replaceChildren(pathTable(storm));
  const slowest = [...storm.bids].sort((a, b) => latencyOf(b) - latencyOf(a));

  const pick = (bid: Bid) => {
    onPick(bid.seq);
    for (const button of document.querySelectorAll<HTMLButtonElement>('button[data-seq]')) {
      button.setAttribute('aria-pressed', String(button.dataset['seq'] === String(bid.seq)));
    }
    waterfall(element('waterfall'), bid);
  };
  const list = (host: string, bids: Bid[]) => element(host).replaceChildren(...bids.map((b) => bidButton(b, pick)));
  list('slowest', slowest.slice(0, 8));

  const host = element('heatmap');
  host.replaceChildren();
  const found = cells(storm.bids);
  const columns = (d3.max(found, (c) => c.column) ?? 0) + 1;
  const left = 96;
  const cellWidth = Math.max(10, (Math.max(320, host.clientWidth) - left - 4) / columns);
  const cellHeight = 16;
  const width = left + columns * cellWidth + 4;
  const height = ROWS.length * cellHeight + 34;
  const y = (row: number) => (ROWS.length - 1 - row) * cellHeight;
  const shade = d3.scaleSqrt().domain([0, d3.max(found, (c) => c.bids.length) ?? 1]).range([0.15, 1]);

  const svg = d3.select(host).append('svg').attr('width', width).attr('height', height)
    .attr('viewBox', `0 0 ${width} ${height}`).attr('role', 'group')
    .attr('aria-label', 'Latency heatmap. Arrow keys move between cells; Enter lists the bids in one.');
  svg.append('g').selectAll('text').data(d3.range(ROWS.length)).join('text').attr('class', 'lane-label')
    .attr('x', left - 6).attr('y', (r) => y(r) + cellHeight / 2).attr('dy', '0.32em').attr('text-anchor', 'end')
    .text((r) => `from ${ms(ROWS[r]!)}`);
  for (let column = 0; column < columns; column += 4) {
    svg.append('text').attr('class', 'axis-label').attr('x', left + column * cellWidth).attr('y', height - 20)
      .text(`${(column * COLUMN_US) / 1e6}s`);
  }
  svg.append('text').attr('class', 'axis-label').attr('x', width - 4).attr('y', height - 4).attr('text-anchor', 'end')
    .text('when the bid was due');

  const label = (c: Cell) =>
    `${c.bids.length} bids due at ${seconds(c.column * COLUMN_US)} to ${seconds((c.column + 1) * COLUMN_US)}, ${rowName(c.row)}`;
  const open = (cell: Cell) => {
    element('cell-title').textContent = label(cell);
    list('cell-bids', cell.bids);
    pick(cell.bids[0]!);
  };
  const rects = svg.append('g').selectAll<SVGRectElement, Cell>('rect').data(found).join('rect')
    .attr('class', 'heat').attr('x', (c) => left + c.column * cellWidth + 1).attr('y', (c) => y(c.row) + 1)
    .attr('width', cellWidth - 2).attr('height', cellHeight - 2).attr('rx', 2)
    .attr('fill-opacity', (c) => shade(c.bids.length))
    .attr('role', 'button').attr('aria-label', label)
    // One tab stop for the whole map; arrows move within it.
    .attr('tabindex', (_, i) => (i === 0 ? 0 : -1))
    .on('click', (_, c) => open(c));
  rects.append('title').text(label);
  rects.on('keydown', function (event: KeyboardEvent, from: Cell) {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      open(from);
      return;
    }
    const step = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, 1], ArrowDown: [0, -1] }[event.key];
    if (!step) return;
    event.preventDefault();
    const [dx, dy] = step as [number, number];
    // The nearest non-empty cell in that direction.
    const next = d3.least(
      found.filter((c) => (c.column - from.column) * dx + (c.row - from.row) * dy > 0),
      (c) => Math.abs(c.column - from.column) * (dx ? 1 : 3) + Math.abs(c.row - from.row) * (dy ? 1 : 3),
    );
    if (!next) return;
    this.setAttribute('tabindex', '-1');
    const target = rects.filter((c) => c === next).node()!;
    target.setAttribute('tabindex', '0');
    target.focus();
  });

  pick(storm.bids.find((b) => b.seq === selectedSeq) ?? slowest[0]!);
}

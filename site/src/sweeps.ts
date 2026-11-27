import './style.css';
import * as d3 from 'd3';
import { element, focusableScrollBoxes, node, table } from './dom.ts';
import { swatch, MARKS } from './staircase.ts';
import { middle, share, sweepOf, VARIABLES, type Outcome, type SweepRun, type Variable } from './sweepdata.ts';
import type { Group } from './data.ts';

interface Series {
  name: string;
  group: Group; // picks the mark's shape and colour
  value: (r: SweepRun) => number | null;
}

interface Measure {
  title: string;
  unit: string;
  series: Series[];
}

const OUTCOMES: [Outcome, Group, string][] = [
  ['accepted', 'accepted', 'Accepted'],
  ['too_low', 'price', 'Too low'],
  ['ended', 'ended', 'Auction ended'],
  ['card', 'card', 'Card hold failed'],
];

const MEASURES: Measure[] = [
  { title: 'Accepted bids', unit: '', series: [{ name: 'Accepted bids', group: 'accepted', value: (r) => r.outcomes.accepted }] },
  { title: 'Typical reply time', unit: ' ms', series: [{ name: 'Typical reply', group: 'accepted', value: (r) => r.reply_ms.typical }] },
  { title: 'Slow reply time (9 in 10 were faster)', unit: ' ms', series: [{ name: 'Slow reply', group: 'accepted', value: (r) => r.reply_ms.slow }] },
  {
    title: 'What became of the bids',
    unit: '%',
    series: OUTCOMES.map(([outcome, group, name]) => ({ name, group, value: (r) => share(r, outcome) })),
  },
];

const round = (v: number | null) => (v === null ? 'none valid' : String(Math.round(v)));

/** Every run as a mark, invalid ones hollow, with a bar at the middle of the valid ones. */
function chart(host: HTMLElement, runs: SweepRun[], variable: Variable, measure: Measure): void {
  const settings = sweepOf(runs, variable);
  const { label, title } = VARIABLES[variable];
  const width = Math.max(300, host.clientWidth);
  const height = 220;
  const margin = { top: 10, right: 8, bottom: 40, left: 46 };
  const x = d3.scaleBand<string>().domain(settings.map((s) => String(s.value))).range([margin.left, width - margin.right]);
  const top = d3.max(runs, (r) => d3.max(measure.series, (s) => (sweepRuns(settings).has(r) ? s.value(r) : 0) ?? 0)) ?? 1;
  const y = d3.scaleLinear().domain([0, top]).nice().range([height - margin.bottom, margin.top]);

  const svg = d3.select(host).append('svg').attr('width', width).attr('height', height).attr('viewBox', `0 0 ${width} ${height}`)
    .attr('role', 'img').attr('aria-label', `${measure.title} against ${title.toLowerCase()}, every run. The table gives the numbers.`);
  svg.append('g').attr('class', 'grid').attr('transform', `translate(${margin.left},0)`)
    .call(d3.axisLeft(y).ticks(5).tickSize(-(width - margin.left - margin.right)));
  svg.append('g').attr('class', 'axis').attr('transform', `translate(0,${height - margin.bottom})`)
    .call(d3.axisBottom(x).tickFormat((v) => label(settings.find((s) => String(s.value) === v)!.value)));
  svg.append('text').attr('class', 'axis-label').attr('x', width - margin.right).attr('y', height - 4).attr('text-anchor', 'end').text(title);

  const lane = x.bandwidth() / measure.series.length;
  measure.series.forEach((series, i) => {
    for (const setting of settings) {
      const centre = x(String(setting.value))! + lane * (i + 0.5);
      const spread = Math.min(5, lane / 6);
      const mid = middle(setting.runs, series.value);
      if (mid !== null) {
        svg.append('line').attr('class', 'middle').attr('x1', centre - lane * 0.4).attr('x2', centre + lane * 0.4).attr('y1', y(mid)).attr('y2', y(mid));
      }
      setting.runs.forEach((run, j) => {
        const value = series.value(run);
        if (value === null) return;
        svg.append('path').attr('class', `run mark ${series.group}${run.valid ? '' : ' invalid'}`)
          .attr('d', d3.symbol(MARKS[series.group], 36)())
          .attr('transform', `translate(${centre + (j - (setting.runs.length - 1) / 2) * spread},${y(value)})`)
          .append('title').text(`${series.name}: ${round(value)}${measure.unit}, run ${j + 1}${run.valid ? '' : ' (invalid)'}`);
      });
    }
  });
}

const sweepRuns = (settings: { runs: SweepRun[] }[]) => new Set(settings.flatMap((s) => s.runs));

function numbers(runs: SweepRun[], variable: Variable): HTMLTableElement {
  const { title, label } = VARIABLES[variable];
  const cell = (setting: SweepRun[], value: (r: SweepRun) => number | null) => {
    const values = setting.map((r) => `${round(value(r))}${r.valid ? '' : '*'}`).join(', ');
    return `${round(middle(setting, value))} (${values})`;
  };
  return table(
    `${title}: middle of the valid runs, then each run. * marks an invalid run.`,
    [title, 'Accepted bids', 'Typical reply ms', 'Slow reply ms', 'Accepted %', 'Too low %', 'Ended %', 'Card failed %'],
    sweepOf(runs, variable).map((s) => [
      label(s.value),
      cell(s.runs, (r) => r.outcomes.accepted),
      cell(s.runs, (r) => r.reply_ms.typical),
      cell(s.runs, (r) => r.reply_ms.slow),
      ...OUTCOMES.map(([outcome]) => round(middle(s.runs, (r) => share(r, outcome)))),
    ]),
  );
}

function render(runs: SweepRun[]): void {
  element('legend').replaceChildren(
    ...OUTCOMES.map(([, group, name]) => {
      const item = node('li');
      item.append(swatch(group), name);
      return item;
    }),
  );
  const draw = () => {
    element('sweeps').replaceChildren(
      ...(Object.keys(VARIABLES) as Variable[]).map((variable) => {
        const section = node('section');
        section.id = `sweep-${variable}`;
        const grid = node('div', '', 'charts');
        section.append(node('h2', VARIABLES[variable].title), grid);
        const details = node('details');
        const scroll = node('div', '', 'scroll');
        scroll.append(numbers(runs, variable));
        details.append(node('summary', 'The numbers'), scroll);
        section.append(details);
        for (const measure of MEASURES) {
          const figure = node('figure');
          const host = node('div', '', 'chart scroll');
          figure.append(node('figcaption', measure.title), host);
          grid.append(figure);
        }
        return section;
      }),
    );
    // Drawn once in the page, so each chart can measure its box.
    for (const variable of Object.keys(VARIABLES) as Variable[]) {
      document.querySelectorAll<HTMLElement>(`#sweep-${variable} .chart`).forEach((host, i) => chart(host, runs, variable, MEASURES[i]!));
    }
    focusableScrollBoxes();
  };
  draw();
  let width = window.innerWidth;
  window.addEventListener('resize', () => {
    if (window.innerWidth !== width) {
      width = window.innerWidth;
      draw();
    }
  });
}

async function load(): Promise<void> {
  const response = await fetch('./data/sweep.json');
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  render(((await response.json()) as { runs: SweepRun[] }).runs);
}

load().catch((error: unknown) => {
  console.error(error);
  element('sweeps').textContent = 'The sweep could not be loaded.';
});

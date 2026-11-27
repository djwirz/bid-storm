import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { middle, sweepOf, VARIABLES, type SweepRun, type Variable } from './sweepdata.ts';

const published = (
  JSON.parse(readFileSync(new URL('../../runs/curated/sweep/summary.json', import.meta.url), 'utf8')) as {
    runs: SweepRun[];
  }
).runs;

const accepted = (variable: Variable) =>
  sweepOf(published, variable).map((s) => middle(s.runs, (r) => r.outcomes.accepted)!);

describe('grouping', () => {
  it('puts the base setting in every sweep and leaves invalid runs out of the middle', () => {
    for (const variable of Object.keys(VARIABLES) as Variable[]) {
      for (const setting of sweepOf(published, variable)) expect(setting.runs, `${variable} ${setting.value}`).toHaveLength(5);
    }
    const run = (valid: boolean, n: number) => ({ valid, sends: n }) as SweepRun;
    expect(middle([run(true, 1), run(true, 3), run(false, 100)], (r) => r.sends)).toBe(2);
    expect(middle([run(false, 1)], (r) => r.sends)).toBeNull();
  });
});

// The sentences on the page (sweeps.html) must stay true of the published runs.
describe('the finding', () => {
  it('slower card holds mean far fewer accepted bids', () => {
    const [instant, quick, usual, slow] = accepted('hold_ms');
    expect([instant! > 100, quick! > 55 && quick! < 75, usual! > 30 && usual! < 45, slow! > 14 && slow! < 22]).toEqual([
      true,
      true,
      true,
      true,
    ]);
  });

  it('accepted bids rise with rate up to 50, vary widely at 100 and collapse at 200 and 300', () => {
    const [at10, at25, at50, , at200, at300] = accepted('rate');
    expect(at10! < at25! && at25! < at50!).toBe(true);
    expect(Math.max(at200!, at300!)).toBeLessThan(15);
    const at100 = sweepOf(published, 'rate')[3]!.runs.map((r) => r.outcomes.accepted);
    expect([Math.min(...at100) < 25, Math.max(...at100) > 40]).toEqual([true, true]);
  });

  it('declines move the middle down less than the runs overlap', () => {
    const settings = sweepOf(published, 'decline');
    expect(accepted('decline')[0]!).toBeGreaterThan(accepted('decline')[3]!);
    const range = (i: number) => settings[i]!.runs.map((r) => r.outcomes.accepted);
    expect(Math.max(...range(3))).toBeGreaterThanOrEqual(Math.min(...range(0)));
  });

  it('every valid run passed every check, and two of sixty were invalid', () => {
    expect(published).toHaveLength(60);
    expect(published.filter((r) => !r.valid)).toHaveLength(2);
    expect(published.filter((r) => r.valid && r.failed_checks.length > 0)).toEqual([]);
  });
});

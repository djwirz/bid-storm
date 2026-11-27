import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { sweepOf, VARIABLES, type SweepRun, type Variable } from '../src/sweepdata.ts';

const runs = (JSON.parse(readFileSync(new URL('../public/data/sweep.json', import.meta.url), 'utf8')) as { runs: SweepRun[] }).runs;

test('draws every run, invalid ones hollow, with no errors or accessibility violations', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  page.on('console', (m) => m.type() === 'error' && errors.push(m.text()));
  await page.goto('/');
  await page.getByRole('link', { name: /Sixty storms/ }).click();
  // The recordings behind the marks are served with the page.
  const download = await page.request.get('./data/sweep-runs.tar.xz');
  expect([download.status(), (await download.body()).subarray(0, 6).toString('hex')]).toEqual([200, 'fd377a585a00']);

  for (const variable of Object.keys(VARIABLES) as Variable[]) {
    const here = sweepOf(runs, variable).flatMap((s) => s.runs);
    const charts = page.locator(`#sweep-${variable} svg`);
    await expect(charts).toHaveCount(4);
    await expect(charts.nth(0).locator('path.run')).toHaveCount(here.length);
    await expect(charts.nth(0).locator('path.run.invalid')).toHaveCount(here.filter((r) => !r.valid).length);
    await expect(charts.nth(3).locator('path.run')).toHaveCount(here.length * 4);
    await page.locator(`#sweep-${variable} summary`).click();
  }
  for (const scheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
  }
  expect(errors).toEqual([]);

  await page.setViewportSize({ width: 375, height: 800 });
  await page.reload();
  await expect(page.locator('#sweeps svg')).toHaveCount(12);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(375);
  // Narrow screens are where tables overflow, so scan there too.
  for (const summary of await page.locator('details summary').all()) await summary.click();
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
});

import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { acceptedBids, examples, latencyOf, ledger, stages, type Storm } from '../src/data.ts';

const storm = JSON.parse(readFileSync(new URL('../public/data/storm.json', import.meta.url), 'utf8')) as Storm;
const accepted = acceptedBids(storm);

test('shows the whole recording with no errors and no accessibility violations', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  page.on('console', (m) => m.type() === 'error' && errors.push(m.text()));
  await page.goto('/');

  await expect(page.locator('#checks li.passed')).toHaveCount(storm.checks.length);
  await expect(page.locator('#staircase path.step')).toHaveCount(accepted.length);
  await expect(page.locator('#staircase path.mark:not(.step)')).toHaveCount(storm.bids.length - accepted.length);
  await expect(page.locator('#agreement rect.cell')).toHaveCount((accepted.length + 1) * storm.run.subscribers);
  await expect(page.locator('#accepted-table tbody tr')).toHaveCount(accepted.length);

  for (const details of await page.locator('details').all()) await details.locator('summary').click();
  for (const scheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
  }
  expect(errors).toEqual([]);
});

test('picks the slowest bid and shows where its time went', async ({ page }) => {
  await page.goto('/');
  const slowest = [...storm.bids].sort((a, b) => latencyOf(b) - latencyOf(a))[0]!;
  await page.locator('#slowest button').first().click();
  await expect(page.locator('#waterfall caption')).toContainText('answer was lost');
  // Most of its half a minute was waiting for the target to ask again.
  const waiting = stages(slowest).find((s) => s.kind === 'waiting')!;
  expect(waiting.to - waiting.from).toBeGreaterThan(latencyOf(slowest) * 0.9);
  await expect(page.locator('#waterfall tr[data-kind="waiting"]')).toHaveCount(1);
  // Even the short calls at the far end have a bar inside their track.
  const hidden = await page.locator('#waterfall .track').evaluateAll((tracks) =>
    tracks.filter((t) => t.firstElementChild!.getBoundingClientRect().left >= t.getBoundingClientRect().right).length,
  );
  expect(hidden).toBe(0);

  // The same from the keyboard: into the map, over a cell, open it.
  await page.locator('#heatmap rect[tabindex="0"]').focus();
  await page.keyboard.press('ArrowRight');
  await page.keyboard.press('Enter');
  await expect(page.locator('#cell-title')).toContainText('bids due at');
  await expect(page.locator('#cell-bids button').first()).toHaveAttribute('aria-pressed', 'true');
});

test('gives every injected fault a story, and a ledger that agrees with the fake', async ({ page }) => {
  await page.goto('/');
  const kinds = ['declined', 'provider', 'lost', 'release', 'retried', 'conflict'] as const;
  for (const fault of kinds) {
    const card = page.locator(`#fault-${fault}`);
    const cases = examples(storm, fault);
    expect(cases.length, fault).toBeGreaterThan(0);
    await expect(card.locator('.seen')).toHaveText(`Seen ${cases.length} times.`);
    await card.locator('summary').click();
    await expect(card.locator('tbody tr')).toHaveCount(cases[0]!.steps.length);
    await card.getByRole('button', { name: /^Next/ }).click();
    await expect(card.locator('output')).toHaveText(`Example 2 of ${cases.length}`);
  }

  const totals = ledger(storm);
  expect([totals.placed, totals.released, totals.captured]).toEqual([storm.fake.placed, storm.fake.released, storm.fake.captured]);
  expect(totals.levels.at(-1)).toMatchObject({ held: storm.fake.held, maybe: 0 });
  const rows = page.locator('#ledger-totals tbody tr');
  const numbers = [storm.fake.placed, storm.fake.released, storm.fake.captured, storm.fake.held].map(String);
  await expect(rows.nth(0).locator('td')).toHaveText(['Proxy', ...numbers]);
  await expect(rows.nth(1).locator('td')).toHaveText(['Fake’s record', ...numbers]);

  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
});

test('steps through accepted bids from the keyboard, and fits a phone', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 800 });
  await page.goto('/');
  const selected = page.locator('#selected');
  await expect(selected).toContainText(`of ${accepted.length}`);
  const before = await selected.textContent();
  await page.getByRole('button', { name: /Next accepted/ }).focus();
  await page.keyboard.press('Enter');
  await expect(selected).not.toHaveText(before ?? '');
  await expect(page.locator('#lanes .lane.selected')).toHaveCount(1);
  // The page itself never scrolls sideways; wide charts scroll in their own box.
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(375);
  // Narrow screens are where tables overflow, so scan there too.
  for (const details of await page.locator('details').all()) await details.locator('summary').click();
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
});

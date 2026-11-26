import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { acceptedBids, type Storm } from '../src/data.ts';

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
});

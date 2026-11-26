// Screenshots the staircase for the README. Run `pnpm build` first, then `pnpm readme-image`.
import { chromium } from '@playwright/test';
import { preview } from 'vite';

const server = await preview({ preview: { port: 4174, strictPort: true, host: '127.0.0.1' } });
const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1000, height: 900 }, colorScheme: 'light', deviceScaleFactor: 2 });
  await page.goto('http://127.0.0.1:4174/');
  await page.locator('#staircase svg').waitFor();
  await page.addStyleTag({ content: '.controls, .note, .ring { display: none }' });
  await page.locator('section[aria-labelledby="stairs-title"]').screenshot({ path: '../docs/img/staircase.png' });
} finally {
  await browser.close();
  await server.close();
}

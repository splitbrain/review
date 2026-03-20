const { chromium } = require('playwright');

(async () => {
  const browser = await chromium.launch({
    executablePath: '/root/.cache/ms-playwright/chromium-1194/chrome-linux/chrome',
    headless: true,
    args: ['--no-sandbox'],
  });
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  await page.goto('http://127.0.0.1:7070', { waitUntil: 'networkidle', timeout: 10000 });
  await page.screenshot({ path: '/home/user/review/screenshot.png', fullPage: true });
  console.log('Screenshot saved to /home/user/review/screenshot.png');
  await browser.close();
})();

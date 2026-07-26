// toolchain-owner: wails diagnostic smoke; marker=diag-wails
// Ownership summary:
//   Diagnostic script: connect to a running Wails/Chromium debug port and inspect the UI.
//
// File map for maintainers:
//   1) Remote browser connection.
//   2) Page selection and event log collection.
//   3) UI probes and output.
//
const puppeteer = require('puppeteer-core');

async function main() {
  const browser = await puppeteer.connect({
    browserURL: 'http://127.0.0.1:9333',
    defaultViewport: null
  });

  const pages = await browser.pages();
  const page = pages.find((p) => p.url().includes('index.html') || p.url().startsWith('file:///')) || pages[0];

  if (!page) {
    console.log('No page found');
    await browser.disconnect();
    return;
  }

  console.log('Page URL:', page.url());

  const errors = [];
  page.on('pageerror', (err) => errors.push(`PAGE ERROR: ${err.message}\n${err.stack || ''}`));
  page.on('console', (msg) => console.log(`CONSOLE ${msg.type()}: ${msg.text()}`));

  await page.bringToFront();
  await new Promise((resolve) => setTimeout(resolve, 2000));

  const basePlaceholder = await page.evaluate(() => {
    const el = document.querySelector('#base');
    return el ? el.placeholder : 'missing #base';
  });

  const taskTemplateLabel = await page.evaluate(() => {
    const el = document.querySelector('[data-ui-text="fields.taskTemplate"]');
    return el ? el.textContent : 'missing label';
  });

  const completedBox = await page.evaluate(() => {
    const el = document.querySelector('#completed-box');
    return el ? el.outerHTML.slice(0, 250) : 'missing completed-box';
  });

  const deps = await page.evaluate(() => {
    return {
      hasDesktopApi: !!(window.desktopPlatformBridge && window.desktopPlatformBridge.createDesktopApi),
      hasUiText: !!window.desktopUiText,
      hasRendererElements: !!window.desktopRendererElements,
      hasStateController: !!window.desktopStateController,
      hasFormController: !!window.desktopFormController
    };
  });

  console.log('--- deps ---');
  console.log(JSON.stringify(deps, null, 2));
  console.log('--- base placeholder ---');
  console.log(basePlaceholder);
  console.log('--- taskTemplate label ---');
  console.log(taskTemplateLabel);
  console.log('--- completed box ---');
  console.log(completedBox);
  console.log('--- errors ---');
  console.log(errors.join('\n') || 'none');

  await browser.disconnect();
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});

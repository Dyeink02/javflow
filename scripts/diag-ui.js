// toolchain-owner: ui diagnostic smoke; marker=diag-ui
// Ownership summary:
//   Diagnostic script: render the generated desktop UI in Puppeteer and sample element text.
//
// File map for maintainers:
//   1) Browser launch and page navigation.
//   2) Page-error / console log collection.
//   3) Element probes and report output.
//
const puppeteer = require('puppeteer-core');
const path = require('path');

async function main() {
  const browser = await puppeteer.launch({
    executablePath: 'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe',
    headless: true,
    args: ['--no-sandbox']
  });

  const page = await browser.newPage();
  const errors = [];
  const consoleLogs = [];

  page.on('pageerror', (err) => errors.push(`PAGE ERROR: ${err.message}`));
  page.on('console', (msg) => consoleLogs.push(`${msg.type()}: ${msg.text()}`));

  const filePath = path.resolve(__dirname, '..', 'desktop', 'renderer', '.generated', 'index.html');
  await page.goto(`file://${filePath}`, { waitUntil: 'networkidle0', timeout: 30000 });

  await new Promise((resolve) => setTimeout(resolve, 2000));

  const baseText = await page.evaluate(() => {
    const el = document.querySelector('#base');
    return el ? el.placeholder : 'missing #base';
  });

  const taskTemplateLabel = await page.evaluate(() => {
    const el = document.querySelector('[data-ui-text="fields.taskTemplate"]');
    return el ? el.textContent : 'missing label';
  });

  const completedBox = await page.evaluate(() => {
    const el = document.querySelector('#completed-box');
    return el ? el.outerHTML.slice(0, 200) : 'missing completed-box';
  });

  console.log('--- base placeholder ---');
  console.log(baseText);
  console.log('--- taskTemplate label ---');
  console.log(taskTemplateLabel);
  console.log('--- completed box ---');
  console.log(completedBox);
  console.log('--- page errors ---');
  console.log(errors.join('\n') || 'none');
  console.log('--- console logs ---');
  console.log(consoleLogs.join('\n') || 'none');

  await browser.close();
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});

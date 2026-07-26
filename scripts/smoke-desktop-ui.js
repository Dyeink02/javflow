#!/usr/bin/env node

// toolchain-owner: active packaged desktop UI smoke; marker=active-toolchain-smoke-desktop-ui
// Packaged desktop UI smoke for the current Wails EXE.
// This is the highest-signal automation when the app launches but panels,
// buttons, or shell text behave incorrectly.
//
// Ownership summary:
// 1) launch the packaged Wails EXE and assert core UI surfaces render
// 2) keep packaged desktop smoke verification separate from runtime code
// 3) provide the highest-signal automation for shell/panel regressions
//
// Boundary rule:
// smoke-test helper only; it should not be imported by runtime code.
//
// File map for maintainers:
// 1) packaged EXE launch/browser attach helpers
// 2) shell/UI assertion helpers
// 3) smoke orchestration and cleanup flow

const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');
const { spawn } = require('child_process');
const puppeteer = require('puppeteer-core');
const { resolveJavFlowExe } = require('./wails-paths.js');
const { APP_INFO } = require('../desktop/common/text/appInfo.js');
const { UI_TEXT_SOURCE } = require('../desktop/common/text/uiTextSource.js');

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function parseArgs(argv) {
  const options = {};

  for (let index = 0; index < argv.length; index += 1) {
    const token = argv[index];
    if (!token.startsWith('--')) {
      continue;
    }

    const [rawKey, inlineValue] = token.slice(2).split('=');
    const nextValue = inlineValue ?? argv[index + 1];
    const isNextValueToken = inlineValue == null && String(nextValue || '').startsWith('--');
    const value = inlineValue != null ? inlineValue : isNextValueToken ? 'true' : nextValue;

    options[rawKey] = value;

    if (inlineValue == null && !isNextValueToken) {
      index += 1;
    }
  }

  return options;
}

async function waitForBrowser(browserURL, timeoutMs) {
  const startedAt = Date.now();
  let lastError = null;

  while (Date.now() - startedAt < timeoutMs) {
    try {
      return await puppeteer.connect({
        browserURL,
        defaultViewport: null
      });
    } catch (error) {
      lastError = error;
      await sleep(250);
    }
  }

  throw lastError || new Error(`Failed to connect to ${browserURL}`);
}

async function waitForAppPage(browser, timeoutMs) {
  const startedAt = Date.now();

  while (Date.now() - startedAt < timeoutMs) {
    const pages = await browser.pages();
    const page = pages.find((candidate) => {
      const url = candidate.url();
      return (
        url.includes('/desktop/renderer/.generated/index.html') ||
        url.includes('/desktop/renderer/index.html') ||
        url.startsWith('file:///')
      );
    });

    if (page) {
      return page;
    }

    await sleep(200);
  }

  throw new Error('Desktop window did not appear in time.');
}

async function setInputValue(page, selector, value) {
  await page.$eval(
    selector,
    (element, nextValue) => {
      element.value = String(nextValue);
      element.dispatchEvent(new Event('input', { bubbles: true }));
      element.dispatchEvent(new Event('change', { bubbles: true }));
    },
    value
  );
}

async function waitForText(page, selector, expectedText, timeoutMs = 5000) {
  await page.waitForFunction(
    (targetSelector, targetText) => {
      const element = document.querySelector(targetSelector);
      return element && element.textContent.includes(targetText);
    },
    { timeout: timeoutMs, polling: 100 },
    selector,
    expectedText
  );
}

async function waitForLog(page, expectedText, timeoutMs = 10000) {
  await page.waitForFunction(
    (targetText) => {
      const logView = document.querySelector('#log-view');
      return logView && logView.textContent.includes(targetText);
    },
    { timeout: timeoutMs, polling: 100 },
    expectedText
  );
}

async function triggerClick(page, selector) {
  await page.$eval(selector, (element) => {
    element.click();
  });
}

async function closeWindow(page) {
  try {
    await page.evaluate(() => window.close());
  } catch {
    // Ignore cleanup failures here.
  }
}

async function main() {
  const options = parseArgs(process.argv.slice(2));
  const appPath = path.resolve(options.app || resolveJavFlowExe());
  const port = Number(options.port || 9333);
  const timeoutMs = Number(options.timeout || 30000);
  const packageJsonPath = path.join(__dirname, '..', 'package.json');
  const packageVersion = JSON.parse(fs.readFileSync(packageJsonPath, 'utf8')).version;
  const version = options.version || APP_INFO.version || packageVersion;
  const isLiteDirectLauncher = appPath.toLowerCase().includes('lite direct');
  const launcherArgsFilePath = path.join(os.tmpdir(), 'jav-lite-direct-launch.args');

  const env = {
    ...process.env,
    JAV_DESKTOP_REMOTE_DEBUG_PORT: String(port),
    JAV_DESKTOP_TEST_MODE: options.testMode === 'false' ? '0' : '1'
  };

  if (isLiteDirectLauncher) {
    fs.writeFileSync(
      launcherArgsFilePath,
      `--remote-debugging-port=${port} --remote-allow-origins=* --desktop-test-mode`,
      'ascii'
    );
  }

  const child = spawn(appPath, [], {
    env,
    stdio: 'ignore'
  });

  let browser;
  let page;

  try {
    browser = await waitForBrowser(`http://127.0.0.1:${port}`, timeoutMs);
    console.log('[smoke] browser connected');
    page = await waitForAppPage(browser, timeoutMs);
    console.log('[smoke] app page ready');
    await page.bringToFront();
    await page.waitForSelector('#start', { timeout: timeoutMs });
    console.log('[smoke] start button visible');

    const versionBadge = await page.$eval('#version-badge', (element) => element.textContent.trim());
    assert.strictEqual(versionBadge, `v${version}`);
    console.log('[smoke] version badge ok');

    const firstChipUrl = await page.$eval('.base-url-chip', (element) => element.dataset.url || '');
    await triggerClick(page, '.base-url-chip');
    await page.waitForFunction(
      (targetValue) => document.querySelector('#base').value === targetValue,
      { timeout: 5000 },
      firstChipUrl
    );
    console.log('[smoke] base url chip ok');

    await setInputValue(page, '#limit', 61);
    await triggerClick(page, '#use-suggested-pages');
    await page.waitForFunction(
      () => document.querySelector('#totalPages').value === '3',
      { timeout: 5000 }
    );
    console.log('[smoke] suggested pages ok');

    await triggerClick(page, '#browse-output');
    await page.waitForFunction(
      () => document.querySelector('#output').value.includes('jav-desktop-ui-test-output'),
      { timeout: 5000 }
    );
    await waitForLog(page, UI_TEXT_SOURCE.messages.outputSelectedPrefix);
    console.log('[smoke] output selection ok');

    await triggerClick(page, '#open-output');
    await waitForLog(page, UI_TEXT_SOURCE.messages.outputOpenedPrefix);
    console.log('[smoke] open output ok');

    await triggerClick(page, '#open-magnet-file');
    await waitForLog(page, UI_TEXT_SOURCE.messages.magnetOpenedPrefix);
    console.log('[smoke] open magnet ok');

    await triggerClick(page, '#open-log-folder');
    await waitForLog(page, UI_TEXT_SOURCE.messages.logFolderOpenedPrefix);
    console.log('[smoke] open log folder ok');

    await triggerClick(page, '#update-antiblock');
    await waitForLog(page, UI_TEXT_SOURCE.messages.antiBlockUpdatedPrefix);
    console.log('[smoke] update antiblock ok');

    await triggerClick(page, '#clear-log');
    await waitForLog(page, UI_TEXT_SOURCE.log.cleared);
    console.log('[smoke] clear log ok');

    const selectedOutput = await page.$eval('#output', (element) => element.value);

    await setInputValue(page, '#output', '');
    await triggerClick(page, '#start');
    await waitForText(page, '#state-message', UI_TEXT_SOURCE.validation.outputRequired);
    console.log('[smoke] start validation ok');

    await triggerClick(page, '#restart');
    await waitForLog(page, UI_TEXT_SOURCE.validation.outputRequired);
    console.log('[smoke] restart validation ok');

    await setInputValue(page, '#output', selectedOutput);
    await setInputValue(page, '#magnetExcludeKeywords', '-U，SIS001');
    await triggerClick(page, '#start');
    await waitForLog(page, UI_TEXT_SOURCE.validation.magnetExcludeKeywordsInvalid);
    await setInputValue(page, '#magnetExcludeKeywords', '-U,SIS001');
    await page.waitForFunction(
      () => document.querySelector('#magnetExcludeKeywords').value === '-U,SIS001',
      { timeout: 5000 }
    );
    console.log('[smoke] magnet keyword validation and refocus ok');

    const stopDisabled = await page.$eval('#stop', (element) => element.disabled);
    assert.strictEqual(stopDisabled, true);

    console.log(`Desktop UI smoke test passed for ${appPath}`);
  } finally {
    if (page) {
      await closeWindow(page);
      await sleep(1000);
    }

    if (browser) {
      await browser.disconnect();
    }

    if (isLiteDirectLauncher) {
      fs.rmSync(launcherArgsFilePath, { force: true });
    }

    if (!child.killed && child.exitCode == null) {
      try {
        child.kill();
      } catch {
        // Ignore cleanup failures for already-exited wrappers.
      }
    }
  }
}

main().catch((error) => {
  console.error(error instanceof Error ? error.stack || error.message : String(error));
  process.exitCode = 1;
});

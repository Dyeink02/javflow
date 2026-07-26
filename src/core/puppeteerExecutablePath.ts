import fs from 'fs';
import logger from './logger';

const DEFAULT_BROWSER_PATHS = [
  'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
  'C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe',
  'C:\\Users\\%USERNAME%\\AppData\\Local\\Google\\Chrome\\Application\\chrome.exe',
  'C:\\Program Files\\Chromium\\Application\\chrome.exe',
  'C:\\Program Files (x86)\\Chromium\\Application\\chrome.exe',
  '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
  '/Applications/Chromium.app/Contents/MacOS/Chromium',
  '/usr/local/Caskroom/google-chrome/latest/Google Chrome.app/Contents/MacOS/Google Chrome',
  '/usr/bin/google-chrome',
  '/usr/bin/chromium-browser',
  '/usr/bin/chromium',
  '/snap/bin/chromium',
  '/usr/bin/google-chrome-stable',
  '/usr/local/bin/chrome',
  '/usr/local/bin/chromium'
];

function expandBrowserPath(candidate: string): string {
  return candidate.replace('%USERNAME%', process.env.USERNAME || '');
}

function findExistingBrowserPath(candidates: string[]): string | undefined {
  for (const candidate of candidates) {
    const resolvedPath = expandBrowserPath(candidate);
    if (fs.existsSync(resolvedPath)) {
      logger.info(`Found Chrome/Chromium at: ${resolvedPath}`);
      return resolvedPath;
    }
  }

  return undefined;
}

function tryResolveBundledBrowserPath(): string | undefined {
  try {
    const bundledPuppeteer = require('puppeteer');
    const browserPath =
      typeof bundledPuppeteer?.executablePath === 'function'
        ? bundledPuppeteer.executablePath()
        : undefined;

    if (browserPath && fs.existsSync(browserPath)) {
      logger.info(`Using bundled Chrome: ${browserPath}`);
      return browserPath;
    }
  } catch (error) {
    logger.debug(
      `Bundled Chrome not found: ${error instanceof Error ? error.message : String(error)}`
    );
  }

  return undefined;
}

export function getPuppeteerExecutablePath(): string | undefined {
  const systemBrowserPath = findExistingBrowserPath(DEFAULT_BROWSER_PATHS);
  if (systemBrowserPath) {
    return systemBrowserPath;
  }

  const bundledBrowserPath = tryResolveBundledBrowserPath();
  if (bundledBrowserPath) {
    return bundledBrowserPath;
  }

  logger.error('No Chrome/Chromium found. Please install Google Chrome or Chromium.');
  logger.error('Tried the following paths:');
  for (const candidate of DEFAULT_BROWSER_PATHS) {
    const resolvedPath = expandBrowserPath(candidate);
    logger.error(`  ${resolvedPath} - ${fs.existsSync(resolvedPath) ? 'EXISTS' : 'NOT FOUND'}`);
  }

  return undefined;
}

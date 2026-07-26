#!/usr/bin/env node

import fs from 'fs';
import path from 'path';
import { program } from 'commander';
import logger from './core/logger';
import ScraperRunner from './core/scraperRunner';
import { ErrorHandler } from './utils/errorHandler';

const packageJsonPath = path.join(__dirname, '..', 'package.json');
const packageJson = JSON.parse(fs.readFileSync(packageJsonPath, 'utf-8'));

program.version(packageJson.version);

program
  .command('crawl', { isDefault: true })
  .description('Start crawling JAV metadata and magnet links')
  .option('-p, --parallel <num>', 'Parallel request count, default 2')
  .option('-t, --timeout <num>', 'Request timeout in milliseconds, default 30000')
  .option('-l, --limit <num>', 'Maximum film count, 0 means unlimited')
  .option('--pages <num>', 'Total index pages to crawl, optional')
  .option('--items-per-page <num>', 'Estimated items per index page, default 30')
  .option('-o, --output <file_path>', 'Output directory for crawl results')
  .option('-s, --search <string>', 'Search keyword')
  .option('-b, --base <url>', 'Base URL to crawl from')
  .option('-x, --proxy <url>', 'Proxy server, for example http://127.0.0.1:8087')
  .option('-d, --delay <num>', 'Delay between requests in seconds, default 2')
  .option('-n, --nomag', 'Skip titles without magnet links')
  .option('-a, --allmag', 'Fetch all magnet links for each title')
  .option('--magnet-exclude <keywords>', 'Exclude magnet titles by keyword, split with commas or new lines')
  .option('--magnet-content-validation', 'Inspect magnet file list and skip ad/junk bundles automatically')
  .option('-N, --nopic', 'Skip poster download')
  .option('-c, --cookies <string>', 'Manual cookie string, for example "key=value; foo=bar"')
  .option('--cloudflare', 'Enable Cloudflare bypass mode')
  .option('--second-validation', 'Run result-only second validation after crawl')
  .option('--template <name>', 'Apply a saved task template name')
  .option('--no-strict-ssl', 'Disable strict SSL validation')
  .action(async (options) => {
    const runner = new ScraperRunner({
      parallel: options.parallel,
      timeout: options.timeout,
      limit: options.limit,
      totalPages: options.pages,
      itemsPerPage: options.itemsPerPage,
      output: options.output,
      search: options.search,
      base: options.base,
      proxy: options.proxy,
      delay: options.delay,
      nomag: options.nomag,
      allmag: options.allmag,
      magnetExcludeKeywords: options.magnetExclude,
      magnetContentValidation: options.magnetContentValidation,
      nopic: options.nopic,
      cookies: options.cookies,
      cloudflare: options.cloudflare,
      secondValidation: options.secondValidation,
      taskTemplate: options.template,
      strictSSL:
        Object.prototype.hasOwnProperty.call(options, 'strictSSL') && options.strictSSL === false
          ? false
          : null,
      useProgressBars: true,
      handleSignals: true
    });

    runner.on('state', (payload: any) => {
      logger.info(`[runner:${payload.status}] ${payload.message}`);
    });

    try {
      await runner.run();
      process.exit(0);
    } catch (error) {
      ErrorHandler.handleGenericError(error, 'crawler execution');
      process.exit(1);
    }
  });

program
  .command('update')
  .description('Update anti-block URL cache')
  .option('-b, --base <url>', 'Base URL used for update request')
  .option('-x, --proxy <url>', 'Proxy server, for example http://127.0.0.1:8087')
  .action(async (options) => {
    try {
      const result = await ScraperRunner.updateAntiBlockUrls({
        base: options.base,
        proxy: options.proxy
      });

      logger.success(
        `Anti-block URL cache updated: ${result.antiBlockUrls.length} entries saved to ${result.filePath}`
      );
      process.exit(0);
    } catch (error) {
      ErrorHandler.handleGenericError(error, 'update anti-block URLs');
      process.exit(1);
    }
  });

program.parse();

const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');

const ResultValidator = require('../dist/core/resultValidator').default;

describe('ResultValidator', () => {
  it('reports duplicates, missing items and low-confidence pages', () => {
    const outputDir = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-validator-'));

    fs.writeFileSync(
      path.join(outputDir, 'filmData.json'),
      JSON.stringify(
        [
          {
            title: 'ABF-001 标题',
            sourceLink: 'https://www.javbus.com/ABF-001',
            category: [],
            actress: [],
            magnetLinks: [{ link: 'magnet:?xt=urn:btih:first', size: '1GB' }]
          },
          {
            title: 'ABF-001 标题重复',
            sourceLink: 'https://www.javbus.com/ABF-001',
            category: [],
            actress: [],
            magnetLinks: [{ link: 'magnet:?xt=urn:btih:first', size: '1GB' }]
          }
        ],
        null,
        2
      ),
      'utf8'
    );

    const report = ResultValidator.validateOutput(outputDir, {
      schemaVersion: 2,
      appVersion: '1.1.16',
      status: 'running',
      message: 'testing',
      updatedAt: new Date().toISOString(),
      startedAt: new Date().toISOString(),
      config: {
        base: 'https://www.javbus.com',
        output: outputDir,
        limit: 10,
        totalPages: 1,
        itemsPerPage: 30,
        parallel: 1,
        delay: 2,
        timeout: 30000,
        secondValidation: true,
        taskTemplate: 'balanced'
      },
      progress: {
        nextPageIndex: 2,
        expectedItemsPerPage: 30,
        queued: 2,
        attempted: 2,
        completed: 1
      },
      links: {
        expected: ['https://www.javbus.com/ABF-001', 'https://www.javbus.com/ABF-002'],
        expectedIds: ['ABF-001', 'ABF-002'],
        queued: ['https://www.javbus.com/ABF-001', 'https://www.javbus.com/ABF-002'],
        queuedIds: ['ABF-001', 'ABF-002'],
        processed: ['https://www.javbus.com/ABF-001'],
        processedIds: ['ABF-001'],
        persisted: ['https://www.javbus.com/ABF-001'],
        persistedIds: ['ABF-001'],
        persistedFilmIds: ['ABF-001']
      },
      reconciliation: {
        expectedIds: ['ABF-001', 'ABF-002'],
        queuedIds: ['ABF-001', 'ABF-002'],
        processedIds: ['ABF-001'],
        persistedIds: ['ABF-001'],
        expectedButNotQueuedIds: [],
        expectedButNotPersistedIds: ['ABF-002'],
        processedButNotPersistedIds: [],
        duplicateExpectedIds: []
      },
      missingItems: ['ABF-002'],
      pageAudits: [
        {
          pageNumber: 1,
          url: 'https://www.javbus.com/star/test',
          expectedCount: 30,
          actualCount: 12,
          retryCount: 3,
          validationPassed: false,
          confidenceScore: 38,
          confidence: 'low',
          reason: '条数明显偏低',
          updatedAt: new Date().toISOString()
        }
      ],
      validationReport: null
    });

    assert.strictEqual(report.duplicateCount, 1);
    assert.strictEqual(report.missingFromQueueCount, 1);
    assert.strictEqual(report.expectedItemCount, 2);
    assert.strictEqual(report.expectedButNotQueuedCount, 0);
    assert.strictEqual(report.lowConfidencePageCount, 1);
    assert.strictEqual(report.passed, false);

    fs.rmSync(outputDir, { recursive: true, force: true });
  });

  it('fails validation when expected ids were not queued or persisted', () => {
    const outputDir = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-validator-gap-'));
    fs.writeFileSync(path.join(outputDir, 'filmData.json'), JSON.stringify([], null, 2), 'utf8');

    const report = ResultValidator.validateOutput(outputDir, {
      schemaVersion: 2,
      appVersion: '1.1.16',
      status: 'running',
      message: 'testing queue gaps',
      updatedAt: new Date().toISOString(),
      startedAt: new Date().toISOString(),
      config: {
        base: 'https://www.javbus.com',
        output: outputDir,
        limit: 2,
        totalPages: 1,
        itemsPerPage: 30,
        parallel: 1,
        delay: 2,
        timeout: 30000,
        secondValidation: true,
        taskTemplate: 'balanced'
      },
      progress: {
        nextPageIndex: 2,
        expectedItemsPerPage: 30,
        queued: 1,
        attempted: 1,
        completed: 0
      },
      links: {
        expected: ['https://www.javbus.com/ABF-101', 'https://www.javbus.com/ABF-102'],
        expectedIds: ['ABF-101', 'ABF-102'],
        queued: ['https://www.javbus.com/ABF-101'],
        queuedIds: ['ABF-101'],
        processed: ['https://www.javbus.com/ABF-101'],
        processedIds: ['ABF-101'],
        persisted: [],
        persistedIds: [],
        persistedFilmIds: []
      },
      reconciliation: {
        expectedIds: ['ABF-101', 'ABF-102'],
        queuedIds: ['ABF-101'],
        processedIds: ['ABF-101'],
        persistedIds: [],
        expectedButNotQueuedIds: ['ABF-102'],
        expectedButNotPersistedIds: ['ABF-101', 'ABF-102'],
        processedButNotPersistedIds: ['ABF-101'],
        duplicateExpectedIds: []
      },
      missingItems: ['ABF-101', 'ABF-102'],
      failedDetails: [],
      pageAudits: [],
      validationReport: null
    });

    assert.strictEqual(report.expectedButNotQueuedCount, 1);
    assert.strictEqual(report.processedButNotPersistedCount, 1);
    assert.deepStrictEqual(report.missingItems, ['ABF-101', 'ABF-102']);
    assert.strictEqual(report.passed, false);

    fs.rmSync(outputDir, { recursive: true, force: true });
  });
  it('uses the softer pass summary for internal consistency checks', () => {
    const outputDir = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-validator-pass-'));
    fs.writeFileSync(
      path.join(outputDir, 'filmData.json'),
      JSON.stringify(
        [
          {
            title: 'ABF-201 title',
            sourceLink: 'https://www.javbus.com/ABF-201',
            category: [],
            actress: [],
            magnetLinks: [{ link: 'magnet:?xt=urn:btih:ok', size: '1GB' }]
          }
        ],
        null,
        2
      ),
      'utf8'
    );

    const report = ResultValidator.validateOutput(outputDir, {
      schemaVersion: 2,
      appVersion: '0.19.0',
      status: 'completed',
      message: 'done',
      updatedAt: new Date().toISOString(),
      startedAt: new Date().toISOString(),
      config: {
        base: 'https://www.javbus.com',
        output: outputDir,
        limit: 1,
        totalPages: 1,
        itemsPerPage: 30,
        parallel: 1,
        delay: 2,
        timeout: 30000,
        secondValidation: true,
        taskTemplate: 'balanced'
      },
      progress: {
        nextPageIndex: 2,
        expectedItemsPerPage: 30,
        queued: 1,
        attempted: 1,
        completed: 1
      },
      links: {
        expected: ['https://www.javbus.com/ABF-201'],
        expectedIds: ['ABF-201'],
        queued: ['https://www.javbus.com/ABF-201'],
        queuedIds: ['ABF-201'],
        processed: ['https://www.javbus.com/ABF-201'],
        processedIds: ['ABF-201'],
        persisted: ['https://www.javbus.com/ABF-201'],
        persistedIds: ['ABF-201'],
        persistedFilmIds: ['ABF-201']
      },
      reconciliation: {
        expectedIds: ['ABF-201'],
        queuedIds: ['ABF-201'],
        processedIds: ['ABF-201'],
        persistedIds: ['ABF-201'],
        expectedButNotQueuedIds: [],
        expectedButNotPersistedIds: [],
        processedButNotPersistedIds: [],
        duplicateExpectedIds: []
      },
      missingItems: [],
      pageAudits: [],
      validationReport: null
    });

    assert.strictEqual(report.passed, true);
    assert.ok(report.summary.includes('\u5185\u90e8\u4e00\u81f4\u6027'));

    fs.rmSync(outputDir, { recursive: true, force: true });
  });

});

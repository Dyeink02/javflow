const assert = require('assert');

const ScraperRunner = require('../dist/core/scraperRunner').default;

describe('ScraperRunner limit tracking', () => {
  it('tracks only the remaining links inside the configured limit window', () => {
    const runner = new ScraperRunner();
    runner.config = { limit: 3 };
    runner.expectedItemIds = new Set(['ABP-001']);

    const trackedLinks = runner.getTrackedPageLinks([
      'https://www.javbus.com/ABP-002',
      'https://www.javbus.com/ABP-003',
      'https://www.javbus.com/ABP-004'
    ]);

    assert.deepStrictEqual(trackedLinks, [
      'https://www.javbus.com/ABP-002',
      'https://www.javbus.com/ABP-003'
    ]);
  });

  it('does not leave overflow links in the expected reconciliation set', () => {
    const runner = new ScraperRunner();
    runner.config = { limit: 1 };

    const trackedLinks = runner.getTrackedPageLinks([
      'https://www.javbus.com/ABP-001',
      'https://www.javbus.com/ABP-002',
      'https://www.javbus.com/ABP-003'
    ]);

    runner.recordExpectedPageLinks(1, trackedLinks);

    assert.deepStrictEqual(Array.from(runner.expectedItemIds), ['ABP-001']);
    assert.deepStrictEqual(runner.getExpectedButNotQueuedLinks(), ['https://www.javbus.com/ABP-001']);
  });

  it('writes light snapshots without heavy reconciliation payloads and keeps policy-skip ids', () => {
    const runner = new ScraperRunner();
    runner.config = {
      BASE_URL: 'https://www.javbus.com',
      base: 'https://www.javbus.com',
      output: 'C:/temp',
      limit: 10,
      totalPages: 1,
      parallel: 2,
      delay: 2,
      timeout: 30000,
      secondValidation: false,
      taskTemplate: 'balanced'
    };
    runner.expectedDetailLinks = new Set(['https://www.javbus.com/ABP-001']);
    runner.queuedDetailLinks = new Set(['https://www.javbus.com/ABP-001']);
    runner.processedDetailLinks = new Set(['https://www.javbus.com/ABP-001']);
    runner.persistedDetailLinks = new Set(['https://www.javbus.com/ABP-001']);
    runner.persistedFilmIds = new Set(['ABP-001']);
    runner.skippedByPolicyItemIds = new Set(['ABP-002']);

    const snapshot = runner.buildTaskSnapshot('running', 'testing', 'light');

    assert.deepStrictEqual(snapshot.links.expected, ['https://www.javbus.com/ABP-001']);
    assert.deepStrictEqual(snapshot.links.skippedIds, ['ABP-002']);
    assert.strictEqual(snapshot.progress.skipped, 1);
    assert.strictEqual(snapshot.links.expectedIds, undefined);
    assert.strictEqual(snapshot.reconciliation, undefined);
    assert.strictEqual(snapshot.validationReport, undefined);
  });

  it('explains duplicate source entries in the final state and unfinished report', () => {
    const runner = new ScraperRunner();
    runner.config = {
      limit: 10,
      secondValidation: true
    };
    runner.expectedItemIds = new Set(['AAA-001', 'AAA-002', 'AAA-003', 'AAA-004', 'AAA-005', 'AAA-006']);
    runner.expectedDetailLinks = new Set([
      'https://www.javbus.com/AAA-001',
      'https://www.javbus.com/AAA-002',
      'https://www.javbus.com/AAA-003',
      'https://www.javbus.com/AAA-004',
      'https://www.javbus.com/AAA-005',
      'https://www.javbus.com/AAA-006',
      'https://www.javbus.com/AAA-001?dup=1',
      'https://www.javbus.com/AAA-002?dup=1'
    ]);
    runner.expectedItemVariantLinks = new Map([
      ['AAA-001', new Set(['https://www.javbus.com/AAA-001', 'https://www.javbus.com/AAA-001?dup=1'])],
      ['AAA-002', new Set(['https://www.javbus.com/AAA-002', 'https://www.javbus.com/AAA-002?dup=1'])],
      ['AAA-003', new Set(['https://www.javbus.com/AAA-003'])],
      ['AAA-004', new Set(['https://www.javbus.com/AAA-004'])],
      ['AAA-005', new Set(['https://www.javbus.com/AAA-005'])],
      ['AAA-006', new Set(['https://www.javbus.com/AAA-006'])]
    ]);
    runner.duplicateExpectedIds = new Set(['AAA-001', 'AAA-002']);
    runner.persistedItemIds = new Set(['AAA-001', 'AAA-002', 'AAA-003', 'AAA-004', 'AAA-005', 'AAA-006']);
    runner.persistedFilmIds = new Set(['AAA-001', 'AAA-002', 'AAA-003', 'AAA-004', 'AAA-005', 'AAA-006']);
    runner.filmCount = 6;
    runner.validationReport = { passed: true };

    const finalState = runner.getFinalStateAfterExecution();
    const reportLines = runner.getUnfinishedReportLines(finalState.status, finalState.message);

    assert.strictEqual(finalState.status, 'incomplete');
    assert.ok(finalState.message.includes('输出结果已通过二次校验，但目标条数仍未补齐'));
    assert.ok(finalState.message.includes('站点原始分页仅解析到 8 条'));
    assert.ok(finalState.message.includes('发现 2 条重复番号（AAA-001、AAA-002）'));
    assert.ok(reportLines.includes('# 任务状态：未完成'));
    assert.ok(reportLines.includes('# 已定位重复番号'));
    assert.ok(reportLines.includes('# 已定位未完成番号'));
    assert.ok(reportLines.some((line) => line.includes('AAA-001 | 出现 2 次')));
  });

  it('treats exact duplicate page links as resolved when unique films are complete', () => {
    const runner = new ScraperRunner();
    runner.config = {
      limit: 4,
      secondValidation: true
    };
    runner.expectedEntryCountRaw = 4;
    runner.expectedItemIds = new Set(['AAA-001', 'AAA-002', 'AAA-003']);
    runner.expectedDetailLinks = new Set([
      'https://www.javbus.com/AAA-001',
      'https://www.javbus.com/AAA-002',
      'https://www.javbus.com/AAA-003'
    ]);
    runner.expectedItemVariantLinks = new Map([
      ['AAA-001', ['https://www.javbus.com/AAA-001', 'https://www.javbus.com/AAA-001']],
      ['AAA-002', ['https://www.javbus.com/AAA-002']],
      ['AAA-003', ['https://www.javbus.com/AAA-003']]
    ]);
    runner.duplicateExpectedIds = new Set(['AAA-001']);
    runner.persistedItemIds = new Set(['AAA-001', 'AAA-002', 'AAA-003']);
    runner.persistedFilmIds = new Set(['AAA-001', 'AAA-002', 'AAA-003']);
    runner.filmCount = 3;
    runner.validationReport = { passed: true };

    const finalState = runner.getFinalStateAfterExecution();

    assert.strictEqual(finalState.status, 'completed');
    assert.ok(finalState.message.includes('站点原始条目 4 条'));
    assert.ok(finalState.message.includes('重复番号 1 条（AAA-001）'));
    assert.ok(finalState.message.includes('按唯一番号完成 3 条'));
  });

  it('treats no-magnet policy skips as resolved instead of unfinished items', () => {
    const runner = new ScraperRunner();
    runner.config = {
      limit: 1,
      nomag: true,
      secondValidation: false
    };
    runner.expectedItemIds = new Set(['ABF-001']);
    runner.queuedItemIds = new Set(['ABF-001']);
    runner.processedItemIds = new Set(['ABF-001']);
    runner.skippedByPolicyItemIds = new Set(['ABF-001']);

    const finalState = runner.getFinalStateAfterExecution();

    assert.strictEqual(finalState.status, 'completed');
    assert.ok(finalState.message.includes('按当前配置跳过无磁力影片 1 条'));
    assert.deepStrictEqual(runner.getUncapturedItems(), []);
    assert.deepStrictEqual(runner.getProcessedButNotPersistedIds(), []);
  });

  it('clears policy-skip markers once the same film is later persisted successfully', () => {
    const runner = new ScraperRunner();
    runner.skippedByPolicyItemIds = new Set(['ABF-001', 'https://www.javbus.com/ABF-001']);

    runner.updatePersistedFilmState({
      title: 'ABF-001 title',
      sourceLink: 'https://www.javbus.com/ABF-001'
    });

    assert.deepStrictEqual(Array.from(runner.skippedByPolicyItemIds), []);
    assert.ok(runner.persistedFilmIds.has('ABF-001'));
  });

  it('includes duplicate-id diagnostics when a page sample collapses after dedupe', () => {
    const runner = new ScraperRunner();

    const diagnostic = runner.buildPageLinkDiagnosticReason(
      [
        'https://www.javbus.com/ABF-001',
        'https://www.javbus.com/ABF-001?dup=1',
        'https://www.javbus.com/ABF-002'
      ],
      ['https://www.javbus.com/ABF-001', 'https://www.javbus.com/ABF-002']
    );

    assert.ok(diagnostic.includes('ABF-001'));
    assert.ok(diagnostic.includes('3'));
    assert.ok(diagnostic.includes('2'));
  });

  it('terminates queues and active requests when stop is called', async () => {
    const runner = new ScraperRunner();
    let queueShutdownCalled = false;
    let requestCloseCalled = false;

    runner.isRunning = true;
    runner.emitState = () => undefined;
    runner.emitLog = () => undefined;
    runner.persistTaskState = () => undefined;
    runner.queueManager = {
      shutdown: async () => {
        queueShutdownCalled = true;
      }
    };
    runner.requestHandler = {
      close: async () => {
        requestCloseCalled = true;
      }
    };

    await runner.stop();

    assert.strictEqual(runner.isStopping, true);
    assert.strictEqual(queueShutdownCalled, true);
    assert.strictEqual(requestCloseCalled, true);
  });

  it('applies Go-restored task state before fallback snapshot parsing', () => {
    const runner = new ScraperRunner({
      resumeExisting: true,
      goRestoredTaskState: {
        shouldRestore: true,
        pageIndex: 6,
        expectedItemsPerPage: 30,
        filmsQueued: 5,
        filmsAttempted: 4,
        filmCount: 2,
        expectedLinks: [
          'https://www.javbus.com/ABP-101',
          'https://www.javbus.com/ABP-102'
        ],
        expectedItemIds: ['ABP-101', 'ABP-102'],
        queuedLinks: [
          'https://www.javbus.com/ABP-101',
          'https://www.javbus.com/ABP-102'
        ],
        processedLinks: ['https://www.javbus.com/ABP-101'],
        persistedLinks: ['https://www.javbus.com/ABP-101'],
        persistedFilmIds: ['ABP-101'],
        duplicateExpectedIds: ['ABP-102'],
        pendingDetailLinks: ['https://www.javbus.com/ABP-102'],
        logMessage: '已从 Go 恢复状态继续抓取。'
      }
    });
    const logs = [];
    runner.logInfo = (message) => {
      logs.push(message);
    };
    runner.taskStateManager = {
      loadSnapshot() {
        throw new Error('should not load local snapshot when Go restored state exists');
      }
    };

    runner.restoreTaskStateSnapshot();

    assert.strictEqual(runner.pageIndex, 6);
    assert.strictEqual(runner.filmsQueued, 5);
    assert.strictEqual(runner.filmsAttempted, 4);
    assert.strictEqual(runner.filmCount, 2);
    assert.deepStrictEqual(runner.getMissingDetailLinks(), ['https://www.javbus.com/ABP-102']);
    assert.ok(runner.persistedFilmIds.has('ABP-101'));
    assert.ok(runner.duplicateExpectedIds.has('ABP-102'));
    assert.ok(logs.some((message) => message.includes('Go 恢复状态')));
  });

  it('applies Go-persisted output state before fallback filmData parsing', () => {
    const runner = new ScraperRunner({
      resumeExisting: true,
      goPersistedOutputState: {
        filmDataExists: true,
        recordCount: 2,
        records: [
          {
            title: 'ABP-401 title',
            sourceLink: 'https://www.javbus.com/ABP-401'
          },
          {
            title: 'ABP-402 title',
            sourceLink: 'https://www.javbus.com/ABP-402'
          }
        ],
        logMessage: '已从 Go 历史结果继续恢复。'
      }
    });
    const logs = [];
    runner.config = {
      output: 'C:/unused'
    };
    runner.logInfo = (message) => {
      logs.push(message);
    };

    runner.loadPersistedOutputState();

    assert.strictEqual(runner.filmCount, 2);
    assert.ok(runner.persistedFilmIds.has('ABP-401'));
    assert.ok(runner.persistedFilmIds.has('ABP-402'));
    assert.ok(logs.some((message) => message.includes('Go 历史结果')));
  });

  it('prefers Go-supplied pending detail links during resume scheduling', async () => {
    const runner = new ScraperRunner({
      resumeExisting: true,
      goRestoredTaskState: {
        shouldRestore: true,
        pendingDetailLinks: [
          'https://www.javbus.com/ABP-202',
          'https://www.javbus.com/ABP-201',
          'https://www.javbus.com/ABP-201'
        ]
      }
    });
    const resumedBatches = [];
    runner.persistTaskState = () => undefined;
    runner.logInfo = () => undefined;
    runner.isAlreadyPersisted = (link) => link.includes('ABP-202');
    runner.enqueueDetailLinksInBatches = async (_queueManager, links) => {
      resumedBatches.push(links);
    };

    await runner.resumePendingDetailLinks({});

    assert.deepStrictEqual(resumedBatches, [[
      'https://www.javbus.com/ABP-201'
    ]]);
  });

  it('builds execution states from the Go-supplied phase plan', () => {
    const runner = new ScraperRunner({
      goExecutionPlan: {
        phaseKeys: [
          'boot',
          'queue_setup',
          'index_discovery',
          'queue_drain',
          'final_drain'
        ]
      }
    });
    runner.config = {
      secondValidation: true
    };

    const phaseKeys = runner.buildExecutionStates().map((state) => state.key);

    assert.deepStrictEqual(phaseKeys, [
      'boot',
      'queue_setup',
      'index_discovery',
      'queue_drain',
      'final_drain'
    ]);
  });

  it('prefers the Go-supplied next-phase map over the local sequential fallback', () => {
    const runner = new ScraperRunner({
      goExecutionPlan: {
        phaseKeys: [
          'boot',
          'queue_setup',
          'index_discovery',
          'queue_drain',
          'page_gap_recovery',
          'queue_gap_recovery',
          'detail_recovery',
          'second_validation',
          'final_drain'
        ],
        nextPhaseByKey: {
          queue_drain: 'detail_recovery',
          detail_recovery: 'final_drain'
        }
      }
    });

    const states = runner.buildExecutionStates();
    const queueDrainState = states.find((state) => state.key === 'queue_drain');
    const detailRecoveryState = states.find((state) => state.key === 'detail_recovery');

    assert.ok(queueDrainState);
    assert.ok(detailRecoveryState);
    assert.strictEqual(queueDrainState.next(), 'detail_recovery');
    assert.strictEqual(detailRecoveryState.next(), 'final_drain');
  });

  it('uses the Go-supplied stop redirect phase when the task is stopping', () => {
    const runner = new ScraperRunner({
      goExecutionPlan: {
        phaseKeys: [
          'boot',
          'queue_setup',
          'index_discovery',
          'queue_drain',
          'page_gap_recovery',
          'queue_gap_recovery',
          'detail_recovery',
          'final_drain'
        ],
        stopRedirectPhaseKey: 'queue_gap_recovery'
      }
    });

    runner.isStopping = true;
    const states = runner.buildExecutionStates();
    const queueDrainState = states.find((state) => state.key === 'queue_drain');

    assert.ok(queueDrainState);
    assert.strictEqual(queueDrainState.next(), 'queue_gap_recovery');
    assert.strictEqual(runner.getStructuredPhaseKey('stopping'), 'queue_gap_recovery');
  });

  it('emits structured phase data together with the state payload', (done) => {
    const runner = new ScraperRunner({
      goExecutionPlan: {
        phaseKeys: [
          'boot',
          'queue_setup',
          'index_discovery',
          'queue_drain',
          'final_drain'
        ]
      }
    });
    runner.currentExecutionPhase = 'index_discovery';

    runner.once('state', (payload) => {
      try {
        assert.strictEqual(payload.phaseKey, 'index_discovery');
        assert.deepStrictEqual(payload.phasePlanKeys, [
          'boot',
          'queue_setup',
          'index_discovery',
          'queue_drain',
          'final_drain'
        ]);
        done();
      } catch (error) {
        done(error);
      }
    });

    runner.emitState('running', '正在抓取第 1 页。');
  });

  it('marks the task completed after recovery fills all expected items', () => {
    const runner = new ScraperRunner();
    runner.config = {
      limit: 2,
      secondValidation: true,
      useCloudflareBypass: true
    };
    runner.expectedItemIds = new Set(['SONE-001', 'SONE-002']);
    runner.queuedItemIds = new Set(['SONE-001', 'SONE-002']);
    runner.processedItemIds = new Set(['SONE-001', 'SONE-002']);
    runner.persistedItemIds = new Set(['SONE-001', 'SONE-002']);
    runner.persistedFilmIds = new Set(['SONE-001', 'SONE-002']);
    runner.filmCount = 2;
    runner.validationReport = { passed: true };

    const finalState = runner.getFinalStateAfterExecution();

    assert.strictEqual(finalState.status, 'completed');
    assert.ok(finalState.message.includes('已二次校验完成'));
  });

  it('detects film code substrings in film ids for filtering', () => {
    const runner = new ScraperRunner();
    runner.config = { filmCodeFilterThreshold: 'VR' };

    assert.strictEqual(runner.isFilmCodeItemFiltered('MDVR-393'), true);
    assert.strictEqual(runner.isFilmCodeItemFiltered('MDVR-352'), true);
    assert.strictEqual(runner.isFilmCodeItemFiltered('DSVR-053'), true);
    assert.strictEqual(runner.isFilmCodeItemFiltered('ABP-001'), false);
    assert.strictEqual(runner.isFilmCodeItemFiltered(''), false);
  });

  it('skips film-code filtered items when enqueueing detail links', () => {
    const runner = new ScraperRunner();
    runner.config = { filmCodeFilterThreshold: 'VR' };
    runner.logInfo = () => undefined;

    const pushed = [];
    const queueManager = {
      getDetailPageQueue: () => ({
        push: (tasks) => pushed.push(...tasks)
      })
    };

    runner.enqueueDetailLinks(queueManager, [
      'https://www.javbus.com/MDVR-393',
      'https://www.javbus.com/ABP-001',
      'https://www.javbus.com/MDVR-352'
    ], true);

    assert.strictEqual(runner.filmsQueued, 1);
    assert.strictEqual(pushed.length, 1);
    assert.strictEqual(pushed[0].link, 'https://www.javbus.com/ABP-001');
    assert.ok(runner.filteredByFilmCodeItemIds.has('MDVR-393'));
    assert.ok(runner.filteredByFilmCodeItemIds.has('MDVR-352'));
    assert.ok(!runner.filteredByFilmCodeItemIds.has('ABP-001'));
  });
});

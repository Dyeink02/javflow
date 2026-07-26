const assert = require('assert');

const {
  DEFAULT_EXECUTION_PHASE_KEYS,
  normalizeExecutionPlan,
  resolveExecutionNextPhase,
  resolveStructuredPhaseKey
} = require('../dist/core/scraperRunnerExecutionPlanUtils');

describe('scraperRunnerExecutionPlanUtils', () => {
  it('normalizes sparse Go execution plans into a complete transition plan', () => {
    const normalizedPlan = normalizeExecutionPlan({
      phaseKeys: [
        'queue_setup',
        'index_discovery',
        'index_discovery',
        'queue_drain',
        'detail_recovery'
      ],
      nextPhaseByKey: {
        queue_drain: 'detail_recovery',
        detail_recovery: 'unknown'
      },
      stopRedirectPhaseKey: 'queue_drain'
    }, DEFAULT_EXECUTION_PHASE_KEYS);

    assert.deepStrictEqual(normalizedPlan.phaseKeys, [
      'boot',
      'queue_setup',
      'index_discovery',
      'queue_drain',
      'detail_recovery',
      'final_drain'
    ]);
    assert.deepStrictEqual(normalizedPlan.nextPhaseByKey, {
      boot: 'queue_setup',
      queue_setup: 'index_discovery',
      index_discovery: 'queue_drain',
      queue_drain: 'detail_recovery',
      detail_recovery: 'final_drain'
    });
    assert.strictEqual(normalizedPlan.initialPhaseKey, 'boot');
    assert.strictEqual(normalizedPlan.finalPhaseKey, 'final_drain');
    assert.strictEqual(normalizedPlan.stopRedirectPhaseKey, 'queue_drain');
  });

  it('redirects to the stop phase when the task is stopping', () => {
    const plan = {
      phaseKeys: [
        'boot',
        'queue_setup',
        'index_discovery',
        'queue_drain',
        'detail_recovery',
        'final_drain'
      ],
      nextPhaseByKey: {
        queue_drain: 'detail_recovery'
      },
      stopRedirectPhaseKey: 'queue_drain'
    };

    assert.strictEqual(
      resolveExecutionNextPhase(plan, 'queue_drain', 'page_gap_recovery', false),
      'detail_recovery'
    );
    assert.strictEqual(
      resolveExecutionNextPhase(plan, 'index_discovery', null, true),
      'queue_drain'
    );
  });

  it('maps structured phase keys from runtime status when no current phase is active', () => {
    const plan = {
      phaseKeys: [
        'boot',
        'queue_setup',
        'index_discovery',
        'queue_drain',
        'final_drain'
      ],
      stopRedirectPhaseKey: 'queue_drain'
    };

    assert.strictEqual(resolveStructuredPhaseKey(plan, '', 'running'), 'boot');
    assert.strictEqual(resolveStructuredPhaseKey(plan, '', 'stopping'), 'queue_drain');
    assert.strictEqual(resolveStructuredPhaseKey(plan, '', 'completed'), 'final_drain');
  });
});

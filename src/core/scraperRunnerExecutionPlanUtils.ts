// @ts-nocheck
"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.DEFAULT_EXECUTION_PHASE_KEYS = void 0;
exports.normalizeExecutionPlan = normalizeExecutionPlan;
exports.resolveExecutionNextPhase = resolveExecutionNextPhase;
exports.resolveStructuredPhaseKey = resolveStructuredPhaseKey;
const DEFAULT_EXECUTION_PHASE_KEYS = Object.freeze([
    'boot',
    'queue_setup',
    'resume_pending',
    'index_discovery',
    'queue_drain',
    'page_gap_recovery',
    'queue_gap_recovery',
    'detail_recovery',
    'second_validation',
    'final_drain'
]);
exports.DEFAULT_EXECUTION_PHASE_KEYS = DEFAULT_EXECUTION_PHASE_KEYS;
function getKnownExecutionPhases() {
    return new Set(DEFAULT_EXECUTION_PHASE_KEYS);
}
function normalizeExecutionPlan(goExecutionPlan, fallbackPhaseKeys = DEFAULT_EXECUTION_PHASE_KEYS) {
    const knownPhases = getKnownExecutionPhases();
    const rawPhaseKeys = Array.isArray(goExecutionPlan?.phaseKeys) && goExecutionPlan.phaseKeys.length > 0
        ? goExecutionPlan.phaseKeys
        : fallbackPhaseKeys;
    const normalizedPhaseKeys = [];
    for (const phaseKey of rawPhaseKeys) {
        if (!knownPhases.has(phaseKey) || normalizedPhaseKeys.includes(phaseKey)) {
            continue;
        }
        normalizedPhaseKeys.push(phaseKey);
    }
    if (normalizedPhaseKeys[0] !== 'boot') {
        normalizedPhaseKeys.unshift('boot');
    }
    if (!normalizedPhaseKeys.includes('final_drain')) {
        normalizedPhaseKeys.push('final_drain');
    }
    const orderedPhaseSet = new Set(normalizedPhaseKeys);
    const normalizedNextPhaseMap = {};
    for (let index = 0; index < normalizedPhaseKeys.length - 1; index += 1) {
        const currentPhaseKey = normalizedPhaseKeys[index];
        const nextPhaseKey = normalizedPhaseKeys[index + 1];
        if (!currentPhaseKey || !nextPhaseKey) {
            continue;
        }
        normalizedNextPhaseMap[currentPhaseKey] = nextPhaseKey;
    }
    const rawNextPhaseMap = goExecutionPlan?.nextPhaseByKey;
    if (rawNextPhaseMap && typeof rawNextPhaseMap === 'object') {
        for (const [phaseKey, nextPhaseKey] of Object.entries(rawNextPhaseMap)) {
            if (!orderedPhaseSet.has(phaseKey) || !orderedPhaseSet.has(nextPhaseKey)) {
                continue;
            }
            normalizedNextPhaseMap[phaseKey] = nextPhaseKey;
        }
    }
    const initialPhaseKey = orderedPhaseSet.has(goExecutionPlan?.initialPhaseKey)
        ? goExecutionPlan.initialPhaseKey
        : normalizedPhaseKeys[0] || 'boot';
    const finalPhaseKey = orderedPhaseSet.has(goExecutionPlan?.finalPhaseKey)
        ? goExecutionPlan.finalPhaseKey
        : normalizedPhaseKeys[normalizedPhaseKeys.length - 1] || 'final_drain';
    const stopRedirectPhaseKey = orderedPhaseSet.has(goExecutionPlan?.stopRedirectPhaseKey)
        ? goExecutionPlan.stopRedirectPhaseKey
        : finalPhaseKey;
    return {
        source: typeof goExecutionPlan?.source === 'string' ? goExecutionPlan.source : '',
        phaseKeys: normalizedPhaseKeys,
        nextPhaseByKey: normalizedNextPhaseMap,
        initialPhaseKey,
        finalPhaseKey,
        stopRedirectPhaseKey,
        logMessage: typeof goExecutionPlan?.logMessage === 'string' ? goExecutionPlan.logMessage : ''
    };
}
function resolveExecutionNextPhase(plan, currentPhaseKey, fallbackNextPhase = null, isStopping = false) {
    const normalizedPlan = normalizeExecutionPlan(plan);
    if (isStopping) {
        return normalizedPlan.stopRedirectPhaseKey || 'final_drain';
    }
    if (currentPhaseKey && normalizedPlan.nextPhaseByKey[currentPhaseKey]) {
        return normalizedPlan.nextPhaseByKey[currentPhaseKey];
    }
    return fallbackNextPhase || normalizedPlan.finalPhaseKey || 'final_drain';
}
function resolveStructuredPhaseKey(plan, currentExecutionPhase, status) {
    const normalizedPlan = normalizeExecutionPlan(plan);
    if (normalizedPlan.phaseKeys.includes(currentExecutionPhase)) {
        return currentExecutionPhase;
    }
    if (['completed', 'incomplete', 'stopped', 'error'].includes(status)) {
        return normalizedPlan.finalPhaseKey || 'final_drain';
    }
    if (status === 'stopping') {
        return normalizedPlan.stopRedirectPhaseKey || normalizedPlan.finalPhaseKey || 'final_drain';
    }
    return normalizedPlan.initialPhaseKey || 'boot';
}

// @ts-nocheck
"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.resolveIndexValidationRetryDelayMs = exports.resolveIndexValidationPolicy = exports.resolveStrictIndexPageRetryLimit = exports.shouldAcceptIndexValidationResult = exports.shouldEnforceExactPageValidation = exports.normalizeIndexValidationPhase = exports.INDEX_VALIDATION_PHASE_RECOVERY = exports.INDEX_VALIDATION_PHASE_INITIAL = void 0;
const INDEX_VALIDATION_PHASE_INITIAL = 'initial';
exports.INDEX_VALIDATION_PHASE_INITIAL = INDEX_VALIDATION_PHASE_INITIAL;
const INDEX_VALIDATION_PHASE_RECOVERY = 'recovery';
exports.INDEX_VALIDATION_PHASE_RECOVERY = INDEX_VALIDATION_PHASE_RECOVERY;
function normalizeIndexValidationPhase(phase) {
    const normalizedPhase = String(phase || '').trim().toLowerCase();
    if (!normalizedPhase || normalizedPhase === INDEX_VALIDATION_PHASE_INITIAL) {
        return INDEX_VALIDATION_PHASE_INITIAL;
    }
    return INDEX_VALIDATION_PHASE_RECOVERY;
}
exports.normalizeIndexValidationPhase = normalizeIndexValidationPhase;
function shouldEnforceExactPageValidation(limit, expectedCount) {
    return Boolean(limit > 0 && expectedCount !== null && expectedCount !== undefined && expectedCount > 0);
}
exports.shouldEnforceExactPageValidation = shouldEnforceExactPageValidation;
function resolveStrictIndexPageRetryLimit(phase, strictIndexPageRetryLimit, largeTaskMode) {
    const normalizedStrictRetryLimit = strictIndexPageRetryLimit > 0 ? strictIndexPageRetryLimit : 1;
    const normalizedPhase = normalizeIndexValidationPhase(phase);
    if (normalizedPhase === INDEX_VALIDATION_PHASE_INITIAL) {
        return Math.min(normalizedStrictRetryLimit, largeTaskMode ? 2 : 3);
    }
    return Math.min(normalizedStrictRetryLimit, largeTaskMode ? 3 : 4);
}
exports.resolveStrictIndexPageRetryLimit = resolveStrictIndexPageRetryLimit;
function resolveIndexValidationPolicy(input) {
    const phase = normalizeIndexValidationPhase(input.phase);
    const strictPageLock = shouldEnforceExactPageValidation(input.limit, input.expectedCount);
    const looseRetryLimit = input.indexPageRetryLimit > 0 ? input.indexPageRetryLimit : 1;
    const maxAttempts = strictPageLock
        ? resolveStrictIndexPageRetryLimit(phase, input.strictIndexPageRetryLimit, Boolean(input.largeTaskMode))
        : looseRetryLimit;
    return {
        phase,
        strictPageLock,
        maxAttempts
    };
}
exports.resolveIndexValidationPolicy = resolveIndexValidationPolicy;
function resolveIndexValidationRetryDelayMs(input) {
    const attempt = input.attempt > 0 ? input.attempt : 1;
    if (!input.strictPageLock) {
        return 1500;
    }
    if (normalizeIndexValidationPhase(input.phase) === INDEX_VALIDATION_PHASE_INITIAL) {
        return Math.min(2200, 600 + attempt * 450);
    }
    return Math.min(4200, 1200 + attempt * 700);
}
exports.resolveIndexValidationRetryDelayMs = resolveIndexValidationRetryDelayMs;
function shouldAcceptIndexValidationResult(input) {
    if (input.expectedCount === null || input.expectedCount === undefined) {
        return true;
    }
    if (!input.strictPageLock && input.isLastTargetPage) {
        return true;
    }
    return input.bestCount >= input.expectedCount;
}
exports.shouldAcceptIndexValidationResult = shouldAcceptIndexValidationResult;

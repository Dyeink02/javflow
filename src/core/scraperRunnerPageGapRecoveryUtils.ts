// @ts-nocheck
"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.resolvePageGapPassEnd = exports.resolvePageGapAuditFollowUp = exports.resolvePageGapPassStart = void 0;
function resolvePageGapPassStart(pendingCount, pass) {
    if (pendingCount > 0) {
        return {
            shouldRunPass: true,
            stopRecovery: false,
            logMessage: ''
        };
    }
    return {
        shouldRunPass: false,
        stopRecovery: true,
        logMessage: pass > 1 ? '分页缺口补查完成，所有已知页面均达到预期条数。' : ''
    };
}
exports.resolvePageGapPassStart = resolvePageGapPassStart;
function resolvePageGapAuditFollowUp(input) {
    if (input.newLinksCount > 0) {
        return {
            action: 'enqueue_new_links',
            logMessage: `第 ${input.pageNumber} 页补查新增 ${input.newLinksCount} 个影片链接，已加入详情队列。`
        };
    }
    if (input.validationPassed) {
        return {
            action: 'validated',
            logMessage: `第 ${input.pageNumber} 页分页缺口补查通过。`
        };
    }
    return {
        action: 'incomplete',
        logMessage: `第 ${input.pageNumber} 页补查后仍为 ${input.mergedActualCount}/${input.expectedCount}。`
    };
}
exports.resolvePageGapAuditFollowUp = resolvePageGapAuditFollowUp;
function resolvePageGapPassEnd(remainingCount, recoveredCount) {
    if (remainingCount <= 0) {
        return {
            status: 'completed',
            stopRecovery: true,
            logMessage: '分页缺口补查已完成，当前所有目标页面均达到预期条数。'
        };
    }
    if (recoveredCount <= 0) {
        return {
            status: 'stagnant',
            stopRecovery: true,
            logMessage: '本轮分页缺口补查未提升结果，停止继续重复补查，避免影响抓取体验。'
        };
    }
    return {
        status: 'continue',
        stopRecovery: false,
        logMessage: ''
    };
}
exports.resolvePageGapPassEnd = resolvePageGapPassEnd;

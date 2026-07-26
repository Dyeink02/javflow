import type { PageAuditRecord } from './taskStateManager';

export {
  classifyDetailFailure,
  getDetailRecoveryBudget,
  buildRecoveryCategorySummary,
  getRecoverableMissingDetailLinks
} from './scraperRunnerDetailFailurePolicyUtils';

export function getRecoverablePageAudits(pageAudits: PageAuditRecord[]): PageAuditRecord[] {
  return [...pageAudits]
    .filter(
      (audit) =>
        typeof audit.expectedCount === 'number' &&
        audit.expectedCount > 0 &&
        audit.actualCount < audit.expectedCount
    )
    .sort((left, right) => left.pageNumber - right.pageNumber);
}

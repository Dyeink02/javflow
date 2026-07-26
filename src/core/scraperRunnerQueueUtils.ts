import type {
  DuplicateExpectedEntryGroup,
  TaskReconciliationSnapshot
} from './taskStateManager';

function sortZh(items: Iterable<string>): string[] {
  return Array.from(items).sort((left, right) => left.localeCompare(right, 'zh-CN'));
}

export function getExpectedButNotQueuedIds(params: {
  expectedItemIds: Set<string>;
  queuedItemIds: Set<string>;
  persistedItemIds: Set<string>;
}): string[] {
  const { expectedItemIds, queuedItemIds, persistedItemIds } = params;
  return sortZh(expectedItemIds).filter(
    (itemId) => !queuedItemIds.has(itemId) && !persistedItemIds.has(itemId)
  );
}

export function getExpectedButNotQueuedLinks(params: {
  expectedItemIds: Set<string>;
  queuedItemIds: Set<string>;
  persistedItemIds: Set<string>;
  expectedItemLinkMap: Map<string, string>;
}): string[] {
  const { expectedItemLinkMap } = params;
  return getExpectedButNotQueuedIds(params)
    .map((itemId) => expectedItemLinkMap.get(itemId) || itemId)
    .filter(Boolean);
}

export function getExpectedButNotPersistedIds(params: {
  expectedItemIds: Set<string>;
  queuedItemIds: Set<string>;
  persistedItemIds: Set<string>;
  skippedItemIds?: Set<string>;
}): string[] {
  const { expectedItemIds, queuedItemIds, persistedItemIds, skippedItemIds } = params;
  const baselineIds = expectedItemIds.size > 0 ? expectedItemIds : queuedItemIds;
  return sortZh(baselineIds).filter(
    (itemId) => !persistedItemIds.has(itemId) && !(skippedItemIds && skippedItemIds.has(itemId))
  );
}

export function getProcessedButNotPersistedIds(params: {
  processedItemIds: Set<string>;
  persistedItemIds: Set<string>;
  skippedItemIds?: Set<string>;
}): string[] {
  const { processedItemIds, persistedItemIds, skippedItemIds } = params;
  return sortZh(processedItemIds).filter(
    (itemId) => !persistedItemIds.has(itemId) && !(skippedItemIds && skippedItemIds.has(itemId))
  );
}

export function buildReconciliation(params: {
  expectedItemIds: Set<string>;
  queuedItemIds: Set<string>;
  processedItemIds: Set<string>;
  persistedItemIds: Set<string>;
  skippedItemIds?: Set<string>;
  duplicateExpectedIds: Set<string>;
  expectedEntryCount: number;
  rawDuplicateEntryCount: number;
  rawDuplicateGroups: DuplicateExpectedEntryGroup[];
}): TaskReconciliationSnapshot {
  const {
    expectedItemIds,
    queuedItemIds,
    processedItemIds,
    persistedItemIds,
    skippedItemIds,
    duplicateExpectedIds,
    expectedEntryCount,
    rawDuplicateEntryCount,
    rawDuplicateGroups
  } = params;

  return {
    expectedIds: sortZh(expectedItemIds),
    queuedIds: sortZh(queuedItemIds),
    processedIds: sortZh(processedItemIds),
    persistedIds: sortZh(persistedItemIds),
    expectedButNotQueuedIds: getExpectedButNotQueuedIds({
      expectedItemIds,
      queuedItemIds,
      persistedItemIds
    }),
    expectedButNotPersistedIds: getExpectedButNotPersistedIds({
      expectedItemIds,
      queuedItemIds,
      persistedItemIds,
      skippedItemIds
    }),
    processedButNotPersistedIds: getProcessedButNotPersistedIds({
      processedItemIds,
      persistedItemIds,
      skippedItemIds
    }),
    duplicateExpectedIds: sortZh(duplicateExpectedIds),
    expectedEntryCount,
    rawDuplicateEntryCount,
    rawDuplicateGroups
  };
}

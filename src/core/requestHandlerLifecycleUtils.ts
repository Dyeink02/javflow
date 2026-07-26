import type { CloudflareAjaxWorkerSlot } from './requestHandlerTypes';

export async function closeCloudflareWorkerSlots(slots: CloudflareAjaxWorkerSlot[]): Promise<void> {
  if (slots.length === 0) {
    return;
  }

  const closingSlots = slots.splice(0);
  await Promise.allSettled(
    closingSlots.map(async (slot) => {
      slot.inFlight = 0;
      await slot.client.close();
    })
  );
}

export function createTrackedAbortController(
  activeAbortControllers: Set<AbortController>
): AbortController {
  const controller = new AbortController();
  activeAbortControllers.add(controller);
  return controller;
}

export function releaseTrackedAbortController(
  activeAbortControllers: Set<AbortController>,
  controller: AbortController
): void {
  activeAbortControllers.delete(controller);
}

export function abortTrackedControllers(activeAbortControllers: Set<AbortController>): void {
  for (const controller of activeAbortControllers) {
    controller.abort();
  }
  activeAbortControllers.clear();
}

export function isAbortLikeError(error: unknown): boolean {
  return (
    error instanceof Error &&
    (error.name === 'CanceledError' ||
      error.name === 'AbortError' ||
      error.message.includes('canceled') ||
      error.message.includes('Request cancelled'))
  );
}

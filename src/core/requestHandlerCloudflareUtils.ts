import type { CloudflareAjaxWorkerSlot } from './requestHandlerTypes';

export interface CloudflareAjaxGateState {
  inFlight: number;
  waiters: Array<() => void>;
}

export function getCloudflareAjaxConcurrencyLimit(params: {
  useCloudflareBypass: boolean;
  parallel: number;
}): number {
  if (!params.useCloudflareBypass) {
    return 1;
  }

  return Math.max(1, Math.min(2, Number(params.parallel) || 1));
}

export async function acquireCloudflareAjaxSlot(
  state: CloudflareAjaxGateState,
  limit: number
): Promise<void> {
  if (state.inFlight < limit) {
    state.inFlight += 1;
    return;
  }

  await new Promise<void>((resolve) => {
    state.waiters.push(() => {
      state.inFlight += 1;
      resolve();
    });
  });
}

export function releaseCloudflareAjaxSlot(state: CloudflareAjaxGateState): void {
  if (state.inFlight > 0) {
    state.inFlight -= 1;
  }

  const next = state.waiters.shift();
  if (next) {
    next();
  }
}

export function drainCloudflareAjaxWaiters(state: CloudflareAjaxGateState): void {
  const pendingWaiters = state.waiters.splice(0);
  for (const waiter of pendingWaiters) {
    waiter();
  }
}

export function ensureCloudflareAjaxWorkerClients(params: {
  slots: CloudflareAjaxWorkerSlot[];
  targetSize: number;
  createSlot: (slotId: number) => CloudflareAjaxWorkerSlot;
}): CloudflareAjaxWorkerSlot[] {
  const { slots, targetSize, createSlot } = params;

  while (slots.length < targetSize) {
    slots.push(createSlot(slots.length + 1));
  }

  return slots;
}

export function acquireLeastBusyCloudflareWorkerSlot(
  slots: CloudflareAjaxWorkerSlot[]
): CloudflareAjaxWorkerSlot {
  const selectedSlot = slots.reduce((bestSlot, currentSlot) => {
    if (!bestSlot || currentSlot.inFlight < bestSlot.inFlight) {
      return currentSlot;
    }

    return bestSlot;
  }, slots[0]);

  selectedSlot.inFlight += 1;
  return selectedSlot;
}

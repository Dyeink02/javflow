// Lightweight in-memory state container for the legacy desktop/sidecar runtime.
// compatibility-owner: active crawl-compatible JS runtime state; marker=compat-mainservices-runtime-state
// This is compatibility-only process state for old JS services that still back
// the sidecar path. The Wails main runtime should not grow new product state
// here; if a bug reproduces without the sidecar, start in the Go runtime first.
// Keep the desktop/sidecar reference explicit so this file is not mistaken for
// active product state during the final Electron cleanup pass.
//
// Ownership summary:
// 1) define the minimal compatibility-only JS process state shape
// 2) keep legacy sidecar runtime state centralized and intentionally small
// 3) avoid turning archived JS runtime state into a second product store
//
// File map for maintainers:
// 1) compatibility runtime state shape
// 2) construction-only export boundary

function createRuntimeState() {
  // Keep this state shape intentionally small. Anything added here increases
  // the amount of legacy JS process state a maintainer must reason about when
  // debugging sidecar incidents.
  return {
    mainWindow: null,
    activeRunner: null,
    currentTaskOutputDir: null,
    lastTaskOutputDir: null,
    organizerRunning: false,
    quittingAfterStop: false,
    pendingRestartSettings: null,
    ipcHandlersRegistered: false
  };
}

// This module intentionally exposes construction only. Mutation rules belong
// to the few remaining sidecar services that still need this compatibility
// state; do not turn this file into a second application store.

module.exports = {
  createRuntimeState
};

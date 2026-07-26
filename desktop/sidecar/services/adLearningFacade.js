// Node sidecar facade for ad-learning operations.
// deprecated: archived sidecar facade only; marker=archived-sidecar-adlearning-facade
// This intentionally reuses desktop/mainServices/adLearningService.js and the
// shared settings store instead of duplicating that logic in the sidecar layer.
//
// Current maintenance rule:
// - this facade is compatibility-only
// - do not route new default organizer behavior through this file
// - the Wails desktop app should prefer Go adlearning unless an archived
//   sidecar scenario explicitly requires this layer
// - this archived sidecar lane is disabled by default unless the archived
//   sidecar compatibility gate is enabled explicitly
// - if a bug reproduces in current organizer UI without the sidecar, debug the
//   Go organizer/ad-learning path before touching this facade
// - the legacy desktop/mainServices reference stays here only to keep the
//   compatibility lane auditable during the remaining Electron cleanup pass
//
// Ownership summary:
// 1) lazy-create archived JS ad-learning runtime dependencies
// 2) normalize/persist sidecar-facing ad-learning settings
// 3) mirror archived ad-learning progress/log state onto the sidecar bus
//
// File map for maintainers:
// 1) lazy archived runtime bootstrap
// 2) settings/model compatibility updates
// 3) learning progress/state mirroring

const path = require('path');

const progressSchema = require('../../common/progressSchema.js');
const { createAdLearningService } = require('../../mainServices/adLearningService.js');
const { createSidecarSettingsStore } = require('./sharedFacadeRuntime.js');
const { createOrganizerEventMirror } = require('./organizerEventMirror.js');
const { buildAdLearningSettingsPatch } = require('./organizerCompatSettings.js');

function createAdLearningFacade({ fs, eventBus }) {
  let runtime = null;
  const organizerEventMirror = createOrganizerEventMirror({ eventBus });

  // Fast troubleshooting split:
  // 1) archived sidecar learning payload/settings issue -> inspect this facade
  // 2) archived learning model/sample/media processing issue -> inspect
  //    `desktop/mainServices/adLearningService.js`
  // 3) active Wails organizer/ad-learning issue without sidecar -> inspect the
  //    Go organizer/adlearning path instead

  function ensureRuntime() {
    // Runtime is lazy because most crawl-only sessions should not pay for
    // ad-learning/service initialization at sidecar bootstrap time.
    if (runtime) {
      return runtime;
    }

    const { app, settingsStore } = createSidecarSettingsStore({ fs });

    runtime = {
      settingsStore,
      service: createAdLearningService({ app, fs, path })
    };
    return runtime;
  }

  async function getSummary() {
    return ensureRuntime().service.getSummary();
  }

  async function updateModel(options = {}) {
    // Model updates still mirror a compatibility settings contract so organizer
    // sidecar flows and archived desktop flows continue to see the same knobs.
    const context = ensureRuntime();
    const result = await context.service.updateModel(options);
    const currentSettings = context.settingsStore.loadSettings();
    context.settingsStore.saveSettings(buildAdLearningSettingsPatch(currentSettings, options));
    return result;
  }

  async function importSamples(options = {}) {
    return ensureRuntime().service.importSamples(options);
  }

  async function learnSamplesByCodes(options = {}) {
    // This facade owns event-bus mirroring and settings/runtime access only.
    // Sample selection/learning behavior stays in adLearningService itself.
    const context = ensureRuntime();
    const taskId = `learning-${Date.now()}`;
    const requestedCodeCount = Array.isArray(options.codes) ? options.codes.length : 0;

    const startingPayload = {
      status: 'running',
      mode: 'learning',
      message: `Start learning by codes: ${requestedCodeCount} target codes.`
    };

    eventBus.emitState('learning', startingPayload, { taskId });
    organizerEventMirror.emitOrganizerState(startingPayload, { taskId });

    const result = await context.service.learnSamplesByCodes({
      ...options,
      onProgress(progress) {
        // Keep progress-message formatting at the facade boundary so the lower
        // service can stay transport-agnostic.
        const normalizedProgress = progressSchema.createProgress(progress.scope || 'learning', progress.phase, progress);
        const message = progressSchema.buildLearningProgressMessage(normalizedProgress);
        const payload = {
          status: 'running',
          mode: 'learning-progress',
          message,
          progress: normalizedProgress
        };

        eventBus.emitState('learning', payload, { taskId });
        organizerEventMirror.emitOrganizerState(payload, { taskId });
      }
    });

    const completedPayload = {
      status: 'completed',
      mode: 'learning',
      message: `Learning by codes completed: matched ${result.matchedVideoCount || 0}, imported ${result.importedSampleCount || 0}.`,
      progress: progressSchema.createProgress('learning', 'completed', {
        matchedVideoCount: result.matchedVideoCount || 0,
        importedSampleCount: result.importedSampleCount || 0,
        missingCodeCount: Array.isArray(result.missingCodes) ? result.missingCodes.length : 0,
        hitRate: Number(result.hitRate || 0),
        falsePositiveRate: Number(result.falsePositiveRate || 0),
        sampleIncrement: Number(result.sampleIncrement || result.importedSampleCount || 0)
      })
    };

    eventBus.emitState('learning', completedPayload, { taskId });
    organizerEventMirror.emitOrganizerState(completedPayload, { taskId });
    organizerEventMirror.emitOrganizerLog(
      organizerEventMirror.createTimedOrganizerLog('info', completedPayload.message),
      { taskId }
    );

    return result;
  }

  async function evaluateVideoRisk(options = {}) {
    return ensureRuntime().service.evaluateVideoRisk(options);
  }

  function getService() {
    return ensureRuntime().service;
  }

  function getSettingsStore() {
    // Settings-store exposure exists only because organizer compatibility flows
    // still reuse the same persisted knobs. Avoid widening this into a generic
    // state-sharing API for new product behavior.
    return ensureRuntime().settingsStore;
  }

  return {
    getSummary,
    updateModel,
    importSamples,
    learnSamplesByCodes,
    evaluateVideoRisk,
    getService,
    getSettingsStore
  };
}

module.exports = {
  createAdLearningFacade
};

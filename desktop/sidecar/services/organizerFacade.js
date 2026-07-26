// Node sidecar facade for organizer operations.
// deprecated: archived sidecar facade only; marker=archived-sidecar-organizer-facade
// This intentionally reuses desktop/mainServices/organizerService.js so the
// compatibility path and the historical desktop behavior stay aligned.
//
// Current maintenance rule:
// - this is not the preferred organizer runtime in the Wails desktop app
// - do not extend this facade with new organizer product behavior
// - any new organizer capability should land in the Go bridge/service path
//   unless an explicit archived-compatibility need is proven first
// - this archived sidecar lane is disabled by default unless the archived
//   sidecar compatibility gate is enabled explicitly
// - the historical desktop/mainServices reference stays visible so the
//   remaining compatibility lane can be audited and retired in one place
//
// Ownership summary:
// 1) normalize organizer sidecar payloads/settings
// 2) delegate execution to the archived JS organizer service
// 3) mirror organizer progress/log/state back onto the sidecar event bus
//
// File map for maintainers:
// 1) facade bootstrap + archived service wiring
// 2) shallow file/code preload helpers
// 3) runOrganizer payload normalization + event mirroring

const path = require('path');

const progressSchema = require('../../common/progressSchema.js');
const { createOrganizerService } = require('../../mainServices/organizerService.js');
const { createOrganizerEventMirror } = require('./organizerEventMirror.js');
const {
  resolveOrganizerCompatibilitySettings,
  buildOrganizerSettingsPatch
} = require('./organizerCompatSettings.js');

function createOrganizerFacade({ fs, eventBus, adLearningFacade }) {
  // The facade boundary is intentionally narrow: normalize sidecar payloads,
  // mirror state/log/progress, and delegate business behavior downward.
  // That keeps current Wails-side organizer ownership from drifting back into
  // this compatibility lane.
  //
  // Fast troubleshooting split:
  // 1) archived sidecar organizer payload/settings/event issue -> inspect this
  //    facade first
  // 2) archived organizer workflow/phase behavior issue -> inspect
  //    `desktop/mainServices/organizerService.js`
  // 3) active Wails organizer issue without sidecar -> inspect the Go
  //    organizer/bridge path instead
  const organizerService = createOrganizerService({ fs, path });
  const organizerEventMirror = createOrganizerEventMirror({ eventBus });

  async function loadCrawlFilmCodes(options = {}) {
    // Film-code preload is the main low-coupling touchpoint between crawler
    // outputs and organizer input. Keep that contract shallow and file-oriented.
    return organizerService.loadCrawlFilmCodes(options);
  }

  function resolveTargetPath(rootPath, kind) {
    return organizerService.resolveTargetPath(rootPath, kind);
  }

  async function runOrganizer(options = {}) {
    // Facade responsibility split:
    // 1) normalize sidecar/UI payload into organizer settings
    // 2) persist compatibility settings shape
    // 3) mirror progress/log/state onto the sidecar event bus
    // 4) delegate actual organizer business rules to organizerService
    //
    // New organizer algorithms should not land here first.
    const taskId = `organizer-${Date.now()}`;
    const settingsStore = adLearningFacade.getSettingsStore();
    const currentSettings = settingsStore.loadSettings();
    const resolvedSettings = resolveOrganizerCompatibilitySettings(currentSettings, options);
    // Let organizerService own the compatibility merge rule for
    // preloadedExpected vs legacy expectedCodes fields so sidecar and
    // Electron-era IPC paths cannot drift apart.
    const expectedInput =
      typeof organizerService.resolveExpectedInput === 'function'
        ? organizerService.resolveExpectedInput(options)
        : {
            preloadedExpected: options.preloadedExpected || null,
            expectedCodes: Array.isArray(options.expectedCodes) ? options.expectedCodes : [],
            expectedCodeEntries: Array.isArray(options.expectedCodeEntries) ? options.expectedCodeEntries : []
          };

    settingsStore.saveSettings(
      buildOrganizerSettingsPatch(currentSettings, options, resolvedSettings)
    );

    if (resolvedSettings.adDetectionEnabled) {
      await adLearningFacade
        .updateModel({
          keywords: resolvedSettings.resolvedKeywords,
          adScore: resolvedSettings.resolvedAdThreshold,
          modelType: resolvedSettings.adModelType
        })
        .catch((error) => {
          organizerEventMirror.emitOrganizerLog(
            organizerEventMirror.createTimedOrganizerLog(
              'warn',
              `Ad-learning model sync failed; continuing with current strategy: ${error instanceof Error ? error.message : String(error)}`
            ),
            { taskId }
          );
        });
    }

    organizerEventMirror.emitOrganizerState(
      {
        status: 'starting',
        mode: 'organizer',
        message: `${Boolean(options.dryRun) ? 'Organizer preview scan starting...' : 'Organizer task starting...'} Ad handling: ${
          resolvedSettings.adFileAction === 'delete-directly' ? 'delete directly' : 'move to waiting-delete'
        }`
      },
      { taskId }
    );

    const result = await organizerService.runOrganizer({
      ...options,
      expectedCodes: expectedInput.expectedCodes,
      expectedCodeEntries: expectedInput.expectedCodeEntries,
      preloadedExpected: expectedInput.preloadedExpected,
      videoExtensions: resolvedSettings.videoExtensions,
      adDetectionEnabled: resolvedSettings.adDetectionEnabled,
      adModelType: resolvedSettings.adModelType,
      adThreshold: resolvedSettings.resolvedAdThreshold,
      adFileAction: resolvedSettings.adFileAction,
      evaluateAdRisk: resolvedSettings.adDetectionEnabled
        ? ({ videoPath, adThreshold }) =>
            adLearningFacade.evaluateVideoRisk({
              videoPath,
              adThreshold,
              modelType: resolvedSettings.adModelType
            })
        : null,
      onLog(entry) {
        organizerEventMirror.emitOrganizerLog(entry, { taskId });
      },
      onProgress(progress) {
        // Keep UI/event formatting here so organizerService stays focused on the
        // actual scan/judge/rename/move workflow.
        const normalizedProgress = progressSchema.createProgress(progress.scope || 'organizer', progress.phase, progress);
        organizerEventMirror.emitOrganizerState(
          {
            status: 'running',
            mode: 'organizer-progress',
            message: progressSchema.buildOrganizerProgressMessage(normalizedProgress),
            progress: normalizedProgress
          },
          { taskId }
        );
      }
    });

    organizerEventMirror.emitOrganizerState(
      {
        status: 'completed',
        mode: 'organizer',
        message: result.dryRun
          ? `Preview completed: matched ${result.summary.qualifiedVideo || 0} videos.`
          : `Organizer completed: waiting ${result.summary.movedToWaiting || 0}, waiting-delete ${
              result.summary.movedToDelete || 0
            }, intro-ad ${result.summary.movedToIntroAd || 0}, deleted directly ${
              result.summary.deletedDirectly || 0
            }, missing codes ${result.summary.missingCodeCount || 0}.`,
        summary: result.summary,
        reportMap: result.reportMap || {},
        reportFiles: result.reportFiles || [],
        missingDownload: result.missingDownload || {},
        adRisk: result.adRisk || {}
      },
      { taskId }
    );

    return result;
  }

  return {
    loadCrawlFilmCodes,
    resolveTargetPath,
    runOrganizer
  };
}

module.exports = {
  createOrganizerFacade
};

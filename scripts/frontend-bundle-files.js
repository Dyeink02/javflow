// toolchain-owner: active frontend JS bundle manifest; marker=active-toolchain-frontend-bundle-files
// Single source of truth for active desktop frontend JS bundle membership.
// Only files listed here should flow into the generated renderer bundle used
// by the current Wails desktop runtime.
//
// Ownership summary:
// 1) declare active desktop bundle membership in one place
// 2) keep bundle order reviewable as data instead of scattered imports
// 3) prevent archived compatibility files from drifting into the active bundle
//
// Boundary rule:
// build-time manifest only; runtime code should not mutate or derive paths here.
//
// File map for maintainers:
// 1) active common bundle entries
// 2) active renderer bundle entries
// 3) exported bundle manifest contract

const FRONTEND_BUNDLE_FILES = [
  'common/text/appInfo.js',
  'common/text/taskConfig.js',
  'common/text/versionHistory.js',
  'common/text/uiTextSource.js',
  'common/text/runtimeText.js',
  'common/bridgeProtocol.js',
  'common/appText.js',
  'common/progressSchema.js',
  'common/crawlPanelModel.js',
  'common/rendererHelpers.js',
  'renderer/uiText.js',
  'renderer/logController.js',
  'renderer/heroBorderFlowController.js',
  'renderer/artifactInputHelper.js',
  'renderer/rendererElementDomains.js',
  'renderer/rendererElements.js',
  'renderer/rendererShellView.js',
  'renderer/rendererShellController.js',
  'renderer/rendererBootstrapController.js',
  'renderer/crawlResultHistoryView.js',
  'renderer/crawlResultHistoryController.js',
  'renderer/stateHelpers.js',
  'renderer/stagePanelRenderer.js',
  'renderer/resultPanelRenderer.js',
  'renderer/reviewPanelRenderer.js',
  'renderer/stateController.js',
  'renderer/formController.js',
  'renderer/rankingListView.js',
  'renderer/rankingController.js',
  'renderer/organizerDependencyView.js',
  'renderer/organizerDependencyController.js',
  'renderer/organizerLearningView.js',
  'renderer/organizerLearningController.js',
  'renderer/organizerCrawlOutputController.js',
  'renderer/organizerReviewView.js',
  'renderer/organizerController.js',
  'renderer/subscriptionListView.js',
  'renderer/subscriptionController.js',
  'renderer/libraryMetadataController.js',
  'renderer/platformBridge.wails.js',
  'renderer/platformBridge.js',
  'renderer/crawlRuntimeController.js',
  'renderer/renderer.js'
];

module.exports = {
  FRONTEND_BUNDLE_FILES
};

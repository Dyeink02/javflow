// toolchain-owner: active maintainability guard registry; marker=active-toolchain-verify-maintainability-config
// Centralized boundary config for maintainability verification.
// Keep long allowlists and marker registries here so the execution flow in
// verify-maintainability.js stays readable.
//
// Ownership summary:
// 1) declare maintainability guard inputs in one place
// 2) keep allowlists/markers/config separate from verification flow control
// 3) make later boundary changes reviewable as data edits instead of logic edits
//
// File map for maintainers:
// 1) active frontend/runtime boundary constants
// 2) guard definition factories and allowlists
// 3) exported config surface consumed by `verify-maintainability.js`

const path = require('path');

const ACTIVE_FRONTEND_PREFIXES = ['common/', 'renderer/'];
const FORBIDDEN_FRONTEND_RUNTIME_PATHS = [
  'desktop/mainServices',
  'desktop\\mainServices',
  'desktop/sidecar',
  'desktop\\sidecar'
];

const FORBIDDEN_ACTIVE_ELECTRON_PATTERNS = [
  "require('electron')",
  'require("electron")',
  "from 'electron'",
  'from "electron"'
];

const RETIRED_LEGACY_TARGETS = [
  'desktop/mainServices/windowService.js'
];
const VERIFY_CONFIG_SELF_FILE = 'scripts/verify-maintainability-config.js';
const OWNERSHIP_HEADER_SNIPPETS = ['Ownership summary:', 'File map for maintainers:'];
const BOUNDARY_HEADER_SNIPPETS = ['Boundary rule:', 'File map for maintainers:'];
const CURRENT_MAINTENANCE_HEADER_SNIPPETS = ['Current maintenance rule:', 'File map for maintainers:'];
const MAINTENANCE_HEADER_SNIPPETS = ['Maintenance boundary:', 'File map for maintainers:'];
const MAINTENANCE_OWNERSHIP_HEADER_SNIPPETS = ['Maintenance boundary:', 'Ownership summary:', 'File map for maintainers:'];
const OWNERSHIP_BOUNDARY_HEADER_SNIPPETS = ['Ownership summary:', 'Boundary rule:'];
const MAINTENANCE_RULE_HEADER_SNIPPETS = ['Maintenance rule:', 'File map for maintainers:'];
const BOOTSTRAP_COMPLETED_SNIPPETS = ['bootstrapCompleted', 'bootstrapPromise', 'function bootstrap()'];
const BOOTSTRAP_COMPLETED_DIRECT_SNIPPETS = ['bootstrapCompleted', 'function bootstrap()'];
const ORGANIZER_WORKSPACE_FORBIDDEN_SNIPPETS = [
  'formController.',
  'switchWorkspace(',
  'resolveActressCrawlTarget(',
  'startCrawl(',
  'stopCrawl(',
  'restartCrawl('
];
const ORGANIZER_LOGGING_FORBIDDEN_SNIPPETS = ['runOrganizer(', 'onOrganizerState(', 'onOrganizerLog('];
const RENDERER_ASSEMBLY_SNIPPETS = [
  'logControllerFactory.createLogController(',
  'crawlResultHistoryControllerFactory.createCrawlResultHistoryController(',
  'rendererShellControllerFactory.createRendererShellController(',
  'stateControllerFactory.createStateController(',
  'formControllerFactory.createFormController(',
	'actressAtlasControllerFactory.createActressAtlasController(',
  'rankingControllerFactory.createRankingController(',
  'organizerControllerFactory.createOrganizerController(',
  'subscriptionControllerFactory.createSubscriptionController(',
  'crawlRuntimeControllerFactory.createCrawlRuntimeController(',
  'rendererBootstrapControllerFactory.createRendererBootstrapController(',
  'heroBorderFlowControllerFactory.createHeroBorderFlowController(',
  'appUpdateControllerFactory.createAppUpdateController(',
  'libraryMetadataControllerFactory.createLibraryMetadataController('
];
const ORGANIZER_CONTROLLER_ASSEMBLY_SNIPPETS = [
  'learningControllerFactory.createOrganizerLearningController(',
  'crawlOutputControllerFactory.createOrganizerCrawlOutputController(',
  'dependencyControllerFactory.createOrganizerDependencyController('
];

function buildAllowlistMap(entries) {
  return new Map(entries.map(([file, allowedReferences]) => [file, [...allowedReferences]]));
}

function buildLegacyAllowlistEntries(entries) {
  return buildAllowlistMap(entries);
}

function desktopPath(rootDir, ...segments) {
  return path.join(rootDir, 'desktop', ...segments);
}

function sidecarPath(rootDir, ...segments) {
  return desktopPath(rootDir, 'sidecar', ...segments);
}

function mainServicesPath(rootDir, ...segments) {
  return desktopPath(rootDir, 'mainServices', ...segments);
}

function rendererPath(rootDir, ...segments) {
  return desktopPath(rootDir, 'renderer', ...segments);
}

function commonPath(rootDir, ...segments) {
  return desktopPath(rootDir, 'common', ...segments);
}

function scriptsPath(rootDir, ...segments) {
  return path.join(rootDir, 'scripts', ...segments);
}

function testPath(rootDir, ...segments) {
  return path.join(rootDir, 'test', ...segments);
}

function internalPath(rootDir, ...segments) {
  return path.join(rootDir, 'wails-shell', 'internal', ...segments);
}

function jsRoot(rootPath) {
  return { root: rootPath, extensions: new Set(['.js']) };
}

function goRoot(rootPath) {
  return { root: rootPath, extensions: new Set(['.go']) };
}

function withVerifyConfigExcluded(check) {
  return {
    ...check,
    excludeFiles: [VERIFY_CONFIG_SELF_FILE]
  };
}

function snippetCheck(file, snippets) {
  return { file, snippets };
}

function markerCheck(file, snippet) {
  return { file, snippet };
}

function normalizePathSegments(segments) {
  return Array.isArray(segments) ? segments : [segments];
}

function snippetChecksFromEntries(rootDir, pathBuilder, entries, defaultSnippets) {
  return entries.map(([segments, snippets = defaultSnippets]) =>
    snippetCheck(pathBuilder(rootDir, ...normalizePathSegments(segments)), snippets)
  );
}

function toolchainMarkerOwnershipCheck(label, target, allowedFiles, options = {}) {
  const check = {
    label,
    target,
    allowedFiles
  };
  return options.excludeVerifyConfig ? withVerifyConfigExcluded(check) : check;
}

function ownershipCheck(label, target, allowedFiles, options = {}) {
  const check = {
    label,
    target,
    allowedFiles
  };
  return options.excludeVerifyConfig ? withVerifyConfigExcluded(check) : check;
}

function rendererBootstrapGuard(relativePath, requiredSnippets) {
  return { relativePath, requiredSnippets };
}

function markerCoverageGuard(label, roots, extensions = ['.js']) {
  return {
    label,
    roots,
    extensions: new Set(extensions),
    requiredSnippet: 'marker=',
    excludeFiles: []
  };
}

function markerChecksFromEntries(rootDir, pathBuilder, entries) {
  return entries.map(([segments, snippet]) =>
    markerCheck(pathBuilder(rootDir, ...normalizePathSegments(segments)), snippet)
  );
}

function createControllerDomOwnershipGuardDefinitions() {
  return {
    controllerFiles: [
      'desktop/renderer/rendererShellController.js',
      'desktop/renderer/crawlResultHistoryController.js',
      'desktop/renderer/stateController.js',
      'desktop/renderer/formController.js',
      'desktop/renderer/rankingController.js',
      'desktop/renderer/organizerController.js',
      'desktop/renderer/organizerDependencyController.js',
      'desktop/renderer/organizerLearningController.js',
      'desktop/renderer/organizerCrawlOutputController.js',
      'desktop/renderer/subscriptionController.js',
      'desktop/renderer/rendererBootstrapController.js',
      'desktop/renderer/appUpdateController.js',
      'desktop/renderer/crawlRuntimeController.js'
    ],
    forbiddenPatterns: [
      'document.createElement(',
      'document.getElementById(',
      'querySelector(',
      'querySelectorAll('
    ]
  };
}

function createFrontendBoundaryGuardDefinition(rootDir) {
  return {
    activeSourceRoots: [
      { root: commonPath(rootDir), extensions: new Set(['.js']) },
      { root: rendererPath(rootDir), extensions: new Set(['.js', '.html']) }
    ]
  };
}

function createArtifactResolverBoundaryGuardDefinitions() {
  return {
    guardedFiles: [
      'desktop/renderer/organizerCrawlOutputController.js',
      'desktop/renderer/subscriptionController.js'
    ],
    forbiddenSnippets: [
      'desktopArtifactInputResolver',
      'getArtifactInputResolver(',
      'resolveLatestInputSafe(',
      'describeResolvedInputOrFallback('
    ]
  };
}

function createFrontendDependencyOrderRules() {
  return [
    ['common/rendererHelpers.js', 'renderer/logController.js'],
    ['renderer/artifactInputHelper.js', 'renderer/organizerCrawlOutputController.js'],
    ['renderer/artifactInputHelper.js', 'renderer/subscriptionController.js'],
    ['renderer/rendererElementDomains.js', 'renderer/rendererElements.js'],
    ['renderer/rendererElements.js', 'renderer/renderer.js'],
    ['renderer/rendererShellView.js', 'renderer/rendererShellController.js'],
    ['renderer/appUpdateController.js', 'renderer/renderer.js'],
    ['common/rendererHelpers.js', 'renderer/organizerController.js'],
    ['common/rendererHelpers.js', 'renderer/subscriptionController.js'],
    ['common/rendererHelpers.js', 'renderer/rankingController.js'],
    ['common/rendererHelpers.js', 'renderer/formController.js'],
    ['renderer/crawlResultHistoryView.js', 'renderer/crawlResultHistoryController.js'],
    ['renderer/rankingListView.js', 'renderer/rankingController.js'],
    ['renderer/organizerDependencyView.js', 'renderer/organizerDependencyController.js'],
    ['renderer/organizerReviewView.js', 'renderer/organizerController.js'],
    ['renderer/organizerLearningView.js', 'renderer/organizerLearningController.js'],
    ['renderer/subscriptionListView.js', 'renderer/subscriptionController.js'],
    ['renderer/stateHelpers.js', 'renderer/stagePanelRenderer.js'],
    ['renderer/stateHelpers.js', 'renderer/resultPanelRenderer.js'],
    ['renderer/stateHelpers.js', 'renderer/reviewPanelRenderer.js'],
    ['renderer/stagePanelRenderer.js', 'renderer/stateController.js'],
    ['renderer/resultPanelRenderer.js', 'renderer/stateController.js'],
    ['renderer/reviewPanelRenderer.js', 'renderer/stateController.js'],
    ['renderer/stateController.js', 'renderer/renderer.js']
  ];
}

function createRendererBootstrapGuardDefinitions() {
  return [
    rendererBootstrapGuard('desktop/renderer/formController.js', ['bootstrapCompleted', 'async function bootstrap()']),
    rendererBootstrapGuard('desktop/renderer/rankingController.js', BOOTSTRAP_COMPLETED_SNIPPETS),
    rendererBootstrapGuard('desktop/renderer/subscriptionController.js', BOOTSTRAP_COMPLETED_SNIPPETS),
    rendererBootstrapGuard('desktop/renderer/organizerController.js', BOOTSTRAP_COMPLETED_SNIPPETS),
    rendererBootstrapGuard('desktop/renderer/crawlRuntimeController.js', ['panelsBootstrapped', 'bootstrapPanelsPromise', 'function bootstrapPanels()']),
    rendererBootstrapGuard('desktop/renderer/rendererShellController.js', BOOTSTRAP_COMPLETED_DIRECT_SNIPPETS),
    rendererBootstrapGuard('desktop/renderer/rendererBootstrapController.js', ['staticFeedsBound', 'bootstrapCompleted', 'bootstrapPromise', 'function bootstrap()']),
    rendererBootstrapGuard('desktop/renderer/crawlResultHistoryController.js', BOOTSTRAP_COMPLETED_DIRECT_SNIPPETS)
  ];
}

function createRendererEntryBoundaryGuardDefinition() {
  return {
    rendererEntryPath: 'desktop/renderer/renderer.js',
    forbiddenChecks: [
      {
        snippet: 'document.getElementById(',
        message: 'renderer.js should use rendererElements.collectRendererElements instead of direct getElementById'
      }
    ]
  };
}

function createRendererWorkspaceBoundaryGuardDefinitions() {
  return {
    guardedFiles: [
      {
        relativePath: 'desktop/renderer/organizerController.js',
        forbiddenSnippets: ORGANIZER_WORKSPACE_FORBIDDEN_SNIPPETS
      },
      {
        relativePath: 'desktop/renderer/organizerCrawlOutputController.js',
        forbiddenSnippets: ORGANIZER_WORKSPACE_FORBIDDEN_SNIPPETS
      },
      {
        relativePath: 'desktop/renderer/subscriptionController.js',
        forbiddenSnippets: ORGANIZER_LOGGING_FORBIDDEN_SNIPPETS
      },
      {
        relativePath: 'desktop/renderer/rankingController.js',
        forbiddenSnippets: ORGANIZER_LOGGING_FORBIDDEN_SNIPPETS
      }
    ],
    rendererEntryPath: 'desktop/renderer/renderer.js',
    requiredEntrySnippets: [
	  'actressAtlasControllerFactory.createActressAtlasController',
      'rankingControllerFactory.createRankingController',
      'subscriptionControllerFactory.createSubscriptionController',
      'organizerControllerFactory.createOrganizerController',
      'switchWorkspace: (workspaceKey) => rendererShellController.setWorkspace(workspaceKey)'
    ]
  };
}

function createRendererControllerAssemblyGuardDefinitions(rootDir) {
  return {
    searchRoots: [jsRoot(rendererPath(rootDir))],
    allowedAssemblies: new Map([
      [
        'desktop/renderer/renderer.js',
        RENDERER_ASSEMBLY_SNIPPETS
      ],
      [
        'desktop/renderer/organizerController.js',
        ORGANIZER_CONTROLLER_ASSEMBLY_SNIPPETS
      ]
    ]),
    createPattern: /\b([A-Za-z0-9_]+)\.create([A-Z][A-Za-z0-9]+Controller)\(/g
  };
}

function createSourceHygieneTargets() {
  return [
    { relativePath: 'backups', reason: 'maintenance snapshots' },
    { relativePath: 'docs/backups', reason: 'historical working backups' },
    { relativePath: 'node_modules', reason: 'installed dependencies' },
    { relativePath: 'desktop/resources/ffmpeg', reason: 'large runtime binary payload' },
    { relativePath: 'wails-shell/build', reason: 'Wails build output' },
    { relativePath: 'wails-shell/release', reason: 'packaged release output' },
    { relativePath: 'dist', reason: 'packaged output belongs outside source maintenance' }
  ];
}

function createSourceHygieneGeneratedOutputConfig(rootDir) {
  return {
    generatedDesktopRoot: rendererPath(rootDir, '.generated')
  };
}

function createCompatibilitySyntaxTargets(rootDir) {
  return [
    sidecarPath(rootDir, 'index.js'),
    sidecarPath(rootDir, 'commandRouter.js'),
    sidecarPath(rootDir, 'serviceRegistry.js'),
    sidecarPath(rootDir, 'services', 'crawlService.js'),
    sidecarPath(rootDir, 'services', 'crawlRuntimeFactory.js'),
    sidecarPath(rootDir, 'services', 'goRunnerHost.js'),
    sidecarPath(rootDir, 'services', 'rankingFacade.js'),
    sidecarPath(rootDir, 'services', 'organizerCompatSettings.js'),
    sidecarPath(rootDir, 'services', 'sharedFacadeRuntime.js'),
    sidecarPath(rootDir, 'services', 'organizerEventMirror.js'),
    sidecarPath(rootDir, 'services', 'organizerFacade.js'),
    mainServicesPath(rootDir, 'organizerService.js'),
    mainServicesPath(rootDir, 'runnerService.js')
  ];
}

function createLegacyImportSourceRoots(rootDir) {
  return [
    jsRoot(desktopPath(rootDir)),
    jsRoot(scriptsPath(rootDir)),
    jsRoot(testPath(rootDir))
  ];
}

function createLegacyImportBoundaryDefinition(rootDir) {
  return {
    sourceRoots: createLegacyImportSourceRoots(rootDir),
    guardedTargetPrefixes: ['desktop/mainServices/', 'desktop/sidecar/']
  };
}

function createActiveJSRoots(rootDir) {
  return [
    commonPath(rootDir),
    rendererPath(rootDir),
    sidecarPath(rootDir),
    mainServicesPath(rootDir),
    scriptsPath(rootDir)
  ];
}

function createGoOwnershipHeaderRoot(rootDir) {
  return internalPath(rootDir);
}

function createGoMaintainabilityHeaderConfig(rootDir) {
  return {
    roots: [
      internalPath(rootDir)
    ],
    allowNoHeader: [],
    extensions: new Set(['.go']),
    headerWindowLines: 48,
    requiredSnippets: OWNERSHIP_HEADER_SNIPPETS
  };
}

function createGoMaintainabilityTestGroups() {
  return [
    {
      label: 'Go domain contract tests',
      rationale: [
        'organizer safety/rename/ad-review flows',
        'AV subscription artifact-import contracts',
        'ad-learning model/evaluation contracts'
      ],
      packages: ['./internal/organizer', './internal/avsubscriptionv2', './internal/adlearning']
    },
    {
      label: 'Go read-model contract tests',
      rationale: [
        'review/result/stage/ui-state projections stay stable',
        'run-context path selection keeps one canonical source of truth'
      ],
      packages: [
        './internal/crawlreview',
        './internal/crawlresult',
        './internal/crawlstage',
        './internal/crawluistate',
        './internal/crawlruncontext'
      ]
    },
    {
      label: 'Go dependency boundary tests',
      rationale: [
        'organizer / avsubscriptionv2 should not drift back into crawler runtime internals',
        'crawler runtime / read-model packages should stay isolated from organizer/subscription business domains',
        'bridge / desktop / sidecar shells should keep explicit dependency boundaries'
      ],
      packages: ['./internal/dependencyrules']
    },
    {
      label: 'Go core module tests',
      rationale: [
        'crawlrunner / crawltask / crawlquality stay stable while decoupling continues',
        'bridge / organizer / avsubscriptionv2 integration-facing contracts remain green'
      ],
      packages: [
        './internal/crawlrunner',
        './internal/crawltask',
        './internal/crawlquality',
        './internal/bridge',
        './internal/organizer',
        './internal/avsubscriptionv2'
      ]
    }
  ];
}

function createNodeMaintainabilityTestGroups() {
  return [
    {
      label: 'archived sidecar compatibility tests',
      runner: 'npx',
      args: [
        'mocha',
        'test/sidecarCommandRouter.test.js',
        'test/sidecarRuntimePaths.test.js',
        'test/sidecarEventBus.test.js',
        'test/sidecarServiceRegistry.test.js',
        'test/sharedFacadeRuntime.test.js',
        'test/taskLogService.test.js',
        'test/crawlRuntimeFactory.test.js',
        'test/organizerCompatSettings.test.js',
        'test/organizerEventMirror.test.js',
        'test/rankingFacade.test.js'
      ],
      rationale: [
        'command-router archived-domain gating and Go crawl compatibility',
        'runtime-path compatibility defaults and app-shim behavior',
        'shared facade / task-log / runtime-factory compatibility helpers'
      ]
    },
    {
      label: 'shared common contract tests',
      runner: 'npx',
      args: [
        'mocha',
        'test/progressSchema.test.js',
        'test/actressRankingAvfanSource.test.js',
        'test/actressRankingOfficialSource.test.js',
        'test/javBusActressLookupService.test.js',
        'test/actressRankingLocalHistory.test.js',
        'test/serviceText.test.js',
        'test/settingsStoreRankingHistory.test.js'
      ],
      rationale: [
        'progress wording/schema contract',
        'ranking parser and lookup contract',
        'shared service-text and ranking-history scaffolds'
      ]
    },
    {
      label: 'compatibility boundary tests',
      runner: 'npx',
      args: [
        'mocha',
        'test/runnerService.test.js',
        'test/logBridge.test.js',
        'test/proxyValidationService.test.js',
        'test/organizerService.test.js'
      ],
      rationale: [
        'runnerService legacy-vs-Go-task-controller split',
        'logBridge formatting/buffering contract',
        'proxyValidationService and organizerService compatibility guardrails'
      ]
    }
  ];
}

function createJSMaintainabilityHeaderConfig(rootDir) {
  return {
    roots: createActiveJSRoots(rootDir),
    allowNoHeader: [
      rendererPath(rootDir, '.generated', 'bundle.js')
    ],
    extensions: new Set(['.js', '.ps1']),
    headerWindowLines: 32,
    requiredSnippets: OWNERSHIP_HEADER_SNIPPETS
  };
}

function createActiveSourceCorruptionScanRoots(rootDir) {
  return [
    jsRoot(commonPath(rootDir)),
    jsRoot(rendererPath(rootDir)),
    jsRoot(sidecarPath(rootDir)),
    jsRoot(mainServicesPath(rootDir)),
    jsRoot(scriptsPath(rootDir)),
    goRoot(internalPath(rootDir))
  ];
}

function createActiveElectronBoundaryConfig(rootDir) {
  return {
    activeRoots: [
      jsRoot(commonPath(rootDir)),
      jsRoot(rendererPath(rootDir)),
      jsRoot(sidecarPath(rootDir)),
      goRoot(internalPath(rootDir)),
      jsRoot(scriptsPath(rootDir))
    ],
    allowlist: [
      scriptsPath(rootDir, 'build-demo-variants.js'),
      scriptsPath(rootDir, 'nsis-paths.js'),
      scriptsPath(rootDir, 'prepare-nsis-templates.js'),
      scriptsPath(rootDir, 'verify-maintainability-config.js')
    ]
  };
}

function createPackagingTextGuardChecks(rootDir) {
  return [
    snippetCheck(scriptsPath(rootDir, 'build-wails-dual-packages.js'), [
        'JavFlow',
        '未找到内置 FFmpeg：',
        '卸载 ',
        '直开版',
        'Wails 双分发打包完成：'
      ]),
    snippetCheck(
      scriptsPath(rootDir, 'wails-paths.js'),
      ['未找到 node.exe，无法生成 Wails 第一阶段运行时包。']
    ),
    snippetCheck(scriptsPath(rootDir, 'build-wails-go-lite.js'), [
        'Go Lite package build completed.',
        'Profile: no Node.js / sidecar / dist / TypeScript, only Go EXE + frontend + FFmpeg'
      ])
  ];
}

function createMachineSpecificPathGuardConfig(rootDir) {
  return {
    forbiddenSnippets: [
      'OpenAI.Codex_',
      'WindowsApps\\OpenAI.Codex_',
      'WindowsApps/OpenAI.Codex_'
    ],
    roots: [
      jsRoot(commonPath(rootDir)),
      jsRoot(rendererPath(rootDir)),
      jsRoot(sidecarPath(rootDir)),
      jsRoot(mainServicesPath(rootDir)),
      jsRoot(scriptsPath(rootDir)),
      goRoot(internalPath(rootDir))
    ],
    allowlist: [
      scriptsPath(rootDir, 'verify-maintainability.js'),
      scriptsPath(rootDir, 'verify-maintainability-config.js')
    ]
  };
}

function createMarkerCoverageGuardConfig(rootDir) {
  return [
    markerCoverageGuard('scripts marker coverage', [scriptsPath(rootDir)]),
    markerCoverageGuard('desktop sidecar marker coverage', [sidecarPath(rootDir)]),
    markerCoverageGuard('desktop mainServices marker coverage', [mainServicesPath(rootDir)])
  ];
}

function createToolchainEntryGuardChecks(rootDir) {
  return [
    snippetCheck(path.join(rootDir, 'package.json'), [
        '"verify:maintainability": "node scripts/verify-maintainability.js"',
        '"inspect:utf8": "node scripts/inspect-utf8.js"',
        '"wails:build": "node scripts/run-wails-build.js"',
        '"smoke:wails:phase1": "node scripts/smoke-wails-phase1.js"'
      ]),
    snippetCheck(
      internalPath(rootDir, 'crawltask', 'service.go'),
      ['Package crawltask is the unified task-controller layer between the bridge']
    ),
    snippetCheck(
      internalPath(rootDir, 'crawlrunner', 'runner.go'),
      ['Package crawlrunner owns the Go-native crawl state machine and execution']
    ),
    snippetCheck(
      internalPath(rootDir, 'crawlquality', 'service.go'),
      ['Package crawlquality summarizes crawl outputs, logs, and quality diagnostics']
    )
  ];
}

function createScriptToolchainOwnershipChecks() {
  const entries = [
    ['compat dual package toolchain marker ownership', 'marker=compat-toolchain-wails-dual-packages', ['scripts/build-wails-dual-packages.js']],
    ['future go-lite toolchain marker ownership', 'marker=target-toolchain-wails-go-lite', ['scripts/build-wails-go-lite.js']],
    ['active bundle-css toolchain marker ownership', 'marker=active-toolchain-bundle-css', ['scripts/bundle-css.js']],
    ['active frontend bundle manifest marker ownership', 'marker=active-toolchain-frontend-bundle-files', ['scripts/frontend-bundle-files.js']],
    ['active run-wails-dev toolchain marker ownership', 'marker=active-toolchain-run-wails-dev', ['scripts/run-wails-dev.js']],
    ['compat smoke bootstrap toolchain marker ownership', 'marker=compat-toolchain-smoke-phase1-bootstrap', ['scripts/smoke-wails-phase1.js']],
    ['compat smoke crawl-start toolchain marker ownership', 'marker=compat-toolchain-smoke-phase1-crawl-start', ['scripts/smoke-wails-phase1-crawl.js']],
    ['active verify entrypoint toolchain marker ownership', 'marker=active-toolchain-verify-maintainability', ['scripts/verify-maintainability.js']],
    ['active assemble-html toolchain marker ownership', 'marker=active-toolchain-assemble-html', ['scripts/assemble-html.js']],
    ['active build-lite-exe toolchain marker ownership', 'marker=active-toolchain-build-lite-exe', ['scripts/build-lite-exe.js']],
    ['active bundle-js toolchain marker ownership', 'marker=active-toolchain-bundle-js', ['scripts/bundle-js.js']],
    ['active check-encoding toolchain marker ownership', 'marker=active-toolchain-check-encoding', ['scripts/check-encoding.js']],
    ['active frontend-paths toolchain marker ownership', 'marker=active-toolchain-frontend-paths', ['scripts/frontend-paths.js']],
    ['active inspect-utf8 toolchain marker ownership', 'marker=active-toolchain-inspect-utf8', ['scripts/inspect-utf8.js']],
    ['active nsis-paths toolchain marker ownership', 'marker=active-toolchain-nsis-paths', ['scripts/nsis-paths.js']],
    ['active postbuild toolchain marker ownership', 'marker=active-toolchain-postbuild', ['scripts/postbuild.js']],
    ['active prepare-nsis-templates toolchain marker ownership', 'marker=active-toolchain-prepare-nsis-templates', ['scripts/prepare-nsis-templates.js']],
    ['active run-wails-build toolchain marker ownership', 'marker=active-toolchain-run-wails-build', ['scripts/run-wails-build.js']],
    ['active smoke-desktop-ui toolchain marker ownership', 'marker=active-toolchain-smoke-desktop-ui', ['scripts/smoke-desktop-ui.js']],
    ['active sync-wails-frontend toolchain marker ownership', 'marker=active-toolchain-sync-wails-frontend', ['scripts/sync-wails-frontend.js']],
    ['active verify-frontend-text toolchain marker ownership', 'marker=active-toolchain-verify-frontend-text', ['scripts/verify-frontend-text.js']],
    ['active wails-paths toolchain marker ownership', 'marker=active-toolchain-wails-paths', ['scripts/wails-paths.js']]
  ];

  return [
    ...entries.map(([label, target, allowedFiles]) =>
      toolchainMarkerOwnershipCheck(label, target, allowedFiles, { excludeVerifyConfig: true })
    ),
    {
      label: 'active verify config toolchain marker ownership',
      target: 'marker=active-toolchain-verify-maintainability-config',
      allowedFiles: ['scripts/verify-maintainability-config.js']
    }
  ];
}

function createLegacyReferenceAllowlist() {
  const sidecarFacadeEntries = [
    ['desktop/sidecar/services/adLearningFacade.js', ['desktop/mainServices']],
    ['desktop/sidecar/services/organizerFacade.js', ['desktop/mainServices']],
    ['desktop/sidecar/services/crawlRuntimeFactory.js', ['desktop/mainServices']],
    ['desktop/sidecar/services/taskLogService.js', ['desktop/mainServices']],
    ['desktop/sidecar/services/sharedFacadeRuntime.js', ['desktop/mainServices']]
  ];
  const bridgeAndToolingEntries = [
    ['desktop/mainServices/runtimeState.js', ['desktop/sidecar']],
    ['scripts/build-wails-dual-packages.js', ['desktop/mainServices', 'desktop/sidecar']],
    ['scripts/verify-maintainability-config.js', ['desktop/mainServices', 'desktop/sidecar']],
    ['scripts/verify-maintainability.js', ['desktop/sidecar']],
    ['wails-shell/internal/interfaces/interfaces.go', ['desktop/sidecar']],
    ['wails-shell/internal/messages/messages.go', ['desktop/sidecar']]
  ];

  return buildLegacyAllowlistEntries([...sidecarFacadeEntries, ...bridgeAndToolingEntries]);
}

function createLegacyReferenceBoundaryConfig(rootDir) {
  return {
    targetSnippets: ['desktop/mainServices', 'desktop/sidecar'],
    sourceRoots: [
      jsRoot(commonPath(rootDir)),
      jsRoot(rendererPath(rootDir)),
      jsRoot(sidecarPath(rootDir)),
      jsRoot(mainServicesPath(rootDir)),
      jsRoot(scriptsPath(rootDir)),
      goRoot(internalPath(rootDir))
    ]
  };
}

function createArchivedSidecarForbiddenCallSnippets() {
  return [
    'callSidecar("organizer"',
    'callSidecar("learning"',
    'callSidecar("ranking"',
    "callSidecar('organizer'",
    "callSidecar('learning'",
    "callSidecar('ranking'",
    'callSidecarJSON("organizer"',
    'callSidecarJSON("learning"',
    'callSidecarJSON("ranking"',
    "callSidecarJSON('organizer'",
    "callSidecarJSON('learning'",
    "callSidecarJSON('ranking'"
  ];
}

function createArchivedSidecarDomainBoundaryDefinition(rootDir) {
  return {
    bridgeRoots: [path.join(rootDir, 'wails-shell', 'internal', 'bridge')],
    bridgeExtensions: new Set(['.go']),
    excludeRelativeSuffixes: ['api_sidecar_helpers.go']
  };
}

function createBridgeCommandOwnershipDefinition() {
  return {
    platformBridgeFile: 'desktop/renderer/platformBridge.wails.js',
    ownerFiles: {
      organizer: 'wails-shell/internal/bridge/api_dispatch_organizer_commands.go',
      learning: 'wails-shell/internal/bridge/api_dispatch_ad_learning.go',
      ranking: 'wails-shell/internal/bridge/api_dispatch_lookup_rankings.go',
      target: 'wails-shell/internal/bridge/api_dispatch_lookup_targets.go',
      subscription: 'wails-shell/internal/bridge/api_dispatch_subscription_v2.go',
      dependency: 'wails-shell/internal/bridge/api_dispatch_dependency.go',
      dialog: 'wails-shell/internal/bridge/api_dispatch_dialog.go',
      runtimeQuery: 'wails-shell/internal/bridge/api_dispatch_runtime_crawl_queries.go',
      crawlLifecycle: 'wails-shell/internal/bridge/api_dispatch_crawl_lifecycle.go',
      crawlSupport: 'wails-shell/internal/bridge/api_dispatch_crawl_support.go',
      runtimeBootstrap: 'wails-shell/internal/bridge/api_dispatch_runtime_bootstrap.go'
    },
    requiredOwnership: [
      ['app:run-organizer', 'organizer', 'case "app:run-organizer":'],
      ['app:load-crawl-film-codes', 'organizer', 'case "app:load-crawl-film-codes":'],
      ['app:get-ad-learning-summary', 'learning', 'case "app:get-ad-learning-summary":'],
      ['app:update-ad-learning-model', 'learning', 'case "app:update-ad-learning-model":'],
      ['app:import-ad-learning-samples', 'learning', 'case "app:import-ad-learning-samples":'],
      ['app:learn-ad-samples-by-codes', 'learning', 'case "app:learn-ad-samples-by-codes":'],
      ['app:get-actress-rankings', 'ranking', 'case "app:get-actress-rankings":'],
      ['app:resolve-actress-crawl-target', 'target', 'case "app:resolve-actress-crawl-target":'],
      ['app:list-av-subscriptions-v2', 'subscription', 'case "app:list-av-subscriptions-v2":'],
      ['app:scan-av-subscriptions-v2-from-output', 'subscription', 'case "app:scan-av-subscriptions-v2-from-output":'],
      ['app:add-av-subscription-v2-manual', 'subscription', 'case "app:add-av-subscription-v2-manual":'],
      ['app:refresh-av-subscriptions-v2', 'subscription', 'case "app:refresh-av-subscriptions-v2":'],
      ['app:refresh-av-subscription-v2', 'subscription', 'case "app:refresh-av-subscription-v2":'],
      ['app:prepare-av-subscription-v2-crawl', 'subscription', 'case "app:prepare-av-subscription-v2-crawl":'],
      ['app:start-av-subscription-v2-crawl', 'subscription', 'case "app:start-av-subscription-v2-crawl":'],
      ['app:start-av-subscription-v2-batch-crawl', 'subscription', 'case "app:start-av-subscription-v2-batch-crawl":'],
      ['app:finalize-av-subscription-v2-crawl', 'subscription', 'case "app:finalize-av-subscription-v2-crawl":'],
      ['app:stop-av-subscription-v2-crawl', 'subscription', 'case "app:stop-av-subscription-v2-crawl":'],
      ['app:av-subscription-v2-crawl-status', 'subscription', 'case "app:av-subscription-v2-crawl-status":'],
      ['app:remove-av-subscription-v2', 'subscription', 'case "app:remove-av-subscription-v2":'],
      ['app:clear-av-subscriptions-v2', 'subscription', 'case "app:clear-av-subscriptions-v2":'],
      ['app:mark-av-subscription-v2-synced', 'subscription', 'case "app:mark-av-subscription-v2-synced":'],
      ['app:patch-av-subscription-v2', 'subscription', 'case "app:patch-av-subscription-v2":'],
      ['app:patch-all-av-subscriptions-actress-filter-v2', 'subscription', 'case "app:patch-all-av-subscriptions-actress-filter-v2":'],
      ['app:reorder-av-subscriptions-v2', 'subscription', 'case "app:reorder-av-subscriptions-v2":'],
      ['app:enrich-av-subscription-v2-media', 'subscription', 'case "app:enrich-av-subscription-v2-media":'],
      ['app:get-dependency-status', 'dependency', 'case "app:get-dependency-status":'],
      ['app:install-dependency', 'dependency', 'case "app:install-dependency":'],
      ['app:uninstall-dependency', 'dependency', 'case "app:uninstall-dependency":'],
      ['app:choose-output', 'dialog', 'case "app:choose-output":'],
      ['app:choose-background-image', 'dialog', 'case "app:choose-background-image":'],
      ['app:clear-background-image', 'dialog', 'case "app:clear-background-image":'],
      ['app:choose-organizer-root', 'dialog', 'case "app:choose-organizer-root":'],
      ['app:choose-learning-samples', 'dialog', 'case "app:choose-learning-samples":'],
      ['app:open-path', 'dialog', 'case "app:open-path":'],
      ['app:open-external', 'dialog', 'case "app:open-external":'],
      ['app:open-output-dir', 'dialog', 'case "app:open-output-dir":'],
      ['app:open-log-folder', 'dialog', 'case "app:open-log-folder":'],
      ['app:open-magnet-file', 'dialog', 'case "app:open-magnet-file":'],
      ['app:open-organizer-path', 'dialog', 'case "app:open-organizer-path":'],
      ['app:get-crawl-run-context', 'runtimeQuery', 'case "app:get-crawl-run-context":'],
      ['app:get-crawl-stage-panel', 'runtimeQuery', 'case "app:get-crawl-stage-panel":'],
      ['app:get-crawl-result-panel', 'runtimeQuery', 'case "app:get-crawl-result-panel":'],
      ['app:get-crawl-review-panel', 'runtimeQuery', 'case "app:get-crawl-review-panel":'],
      ['app:get-run-quality-summary', 'runtimeQuery', 'case "app:get-run-quality-summary":'],
      ['app:get-crawl-task-snapshot', 'runtimeQuery', 'case "app:get-crawl-task-snapshot":'],
      ['app:start-crawl', 'crawlLifecycle', 'case "app:start-crawl":'],
      ['app:restart-crawl', 'crawlLifecycle', 'case "app:restart-crawl":'],
      ['app:stop-crawl', 'crawlLifecycle', 'case "app:stop-crawl":'],
      ['app:update-antiblock', 'crawlSupport', 'case "app:update-antiblock":'],
      ['app:get-settings', 'runtimeBootstrap', 'case "app:get-settings":'],
      ['app:get-log-context', 'runtimeBootstrap', 'case "app:get-log-context":'],
      ['app:get-integration-context', 'runtimeBootstrap', 'case "app:get-integration-context":'],
      ['app:validate-proxy', 'runtimeBootstrap', 'case "app:validate-proxy":'],
      ['app:check-app-update', 'runtimeBootstrap', 'case "app:check-app-update":'],
      ['app:download-app-update', 'runtimeBootstrap', 'case "app:download-app-update":'],
      ['app:apply-app-update', 'runtimeBootstrap', 'case "app:apply-app-update":']
    ]
  };
}

function createBridgeEventOwnershipDefinition() {
  return {
    platformBridgeFile: 'desktop/renderer/platformBridge.wails.js',
    bridgeProtocolFile: 'desktop/common/bridgeProtocol.js',
    rawEventOwnerFile: 'wails-shell/internal/bridge/api_crawl_state_events.go',
    requiredProtocolEvents: [
      'crawlUiState',
      'crawlStagePanel',
      'crawlResultPanel',
      'crawlRunContext',
      'crawlReviewPanel',
      'crawlQualitySummary',
      'sidecarLifecycle',
      'appNotice'
    ],
    requiredBridgeSubscriptions: [
      'onUiState',
      'onStagePanel',
      'onResultPanel',
      'onRunContext',
      'onReviewPanel',
      'onQualitySummary',
      'onSidecarLifecycle',
      'onAppNotice'
    ],
    requiredRawOwnershipSnippets: [
      'a.runtime.bus.Publish("", "log", "crawl.log"'
    ]
  };
}

function createToolchainPathOwnershipChecks() {
  return [
    {
      target: 'scripts/wails-paths.js',
      allowedImporters: [
        'scripts/build-wails-dual-packages.js',
        'scripts/postbuild.js',
        'scripts/run-wails-build.js',
        'scripts/run-wails-dev.js',
        'scripts/smoke-desktop-ui.js'
      ]
    },
    {
      target: 'scripts/frontend-paths.js',
      allowedImporters: [
        'scripts/assemble-html.js',
        'scripts/bundle-css.js',
        'scripts/bundle-js.js',
        'scripts/sync-wails-frontend.js',
        'scripts/verify-frontend-text.js'
      ]
    }
  ];
}

function createToolchainPathOwnershipSearchRoots(rootDir) {
  return [jsRoot(scriptsPath(rootDir))];
}

function createScriptToolchainOwnershipSearchRoots(rootDir) {
  return [jsRoot(scriptsPath(rootDir))];
}

function createRetiredElectronDirectUsageAllowlist(rootDir) {
  return [mainServicesPath(rootDir, 'windowService.js')];
}

function createRetiredElectronDirectUsageExcludeFiles() {
  return [VERIFY_CONFIG_SELF_FILE, 'scripts/verify-maintainability.js'];
}

function createRetiredElectronDirectUsageSearchRoots(rootDir) {
  return [
    jsRoot(desktopPath(rootDir)),
    jsRoot(scriptsPath(rootDir)),
    jsRoot(testPath(rootDir))
  ];
}

function createRetiredElectronDirectUsagePatterns() {
  return FORBIDDEN_ACTIVE_ELECTRON_PATTERNS.filter((snippet) => snippet.includes('require('));
}

function createLegacyImportAllowlist() {
  const mainServiceEntries = [
    ['desktop/mainServices/adLearningService.js', ['desktop/sidecar/services/adLearningFacade.js']],
    ['desktop/mainServices/logBridge.js', ['desktop/sidecar/services/taskLogService.js', 'test/logBridge.test.js']],
    ['desktop/mainServices/organizerService.js', ['desktop/sidecar/services/organizerFacade.js', 'test/organizerService.test.js']],
    ['desktop/mainServices/proxyValidationService.js', ['desktop/sidecar/serviceRegistry.js', 'test/proxyValidationService.test.js']],
    ['desktop/mainServices/runnerService.js', ['desktop/sidecar/services/crawlRuntimeFactory.js', 'test/runnerService.test.js']],
    ['desktop/mainServices/runtimeState.js', ['desktop/sidecar/services/crawlRuntimeFactory.js']],
    ['desktop/mainServices/settingsStore.js', ['desktop/sidecar/services/sharedFacadeRuntime.js', 'test/settingsStoreRankingHistory.test.js']]
  ];
  const organizerPhaseEntries = [
    ['desktop/mainServices/organizerModules/scanPhase.js', ['desktop/mainServices/organizerService.js']],
    ['desktop/mainServices/organizerModules/judgePhase.js', ['desktop/mainServices/organizerService.js']],
    ['desktop/mainServices/organizerModules/renamePhase.js', ['desktop/mainServices/organizerService.js']],
    ['desktop/mainServices/organizerModules/introAdPhase.js', ['desktop/mainServices/organizerService.js']],
    ['desktop/mainServices/organizerModules/reportPhase.js', ['desktop/mainServices/organizerService.js']],
    ['desktop/mainServices/organizerModules/cleanupPhase.js', ['desktop/mainServices/organizerService.js']]
  ];
  const sidecarEntryEntries = [
    ['desktop/sidecar/commandRouter.js', ['desktop/sidecar/index.js', 'test/sidecarCommandRouter.test.js']],
    ['desktop/sidecar/serviceRegistry.js', ['desktop/sidecar/commandRouter.js', 'test/sidecarServiceRegistry.test.js']],
    ['desktop/sidecar/runtimePaths.js', [
      'desktop/sidecar/commandRouter.js',
      'desktop/sidecar/services/crawlRuntimeFactory.js',
      'desktop/sidecar/services/rankingFacade.js',
      'desktop/sidecar/services/sharedFacadeRuntime.js',
      'test/rankingFacade.test.js',
      'test/sidecarCommandRouter.test.js',
      'test/sidecarRuntimePaths.test.js'
    ]]
  ];
  const sidecarServiceEntries = [
    ['desktop/sidecar/services/rankingFacade.js', ['desktop/sidecar/serviceRegistry.js', 'test/rankingFacade.test.js']],
    ['desktop/sidecar/services/adLearningFacade.js', ['desktop/sidecar/serviceRegistry.js']],
    ['desktop/sidecar/services/crawlService.js', ['desktop/sidecar/serviceRegistry.js', 'test/sidecarCommandRouter.test.js']],
    ['desktop/sidecar/services/crawlRuntimeFactory.js', ['desktop/sidecar/services/crawlService.js', 'test/crawlRuntimeFactory.test.js']],
    ['desktop/sidecar/services/sharedFacadeRuntime.js', [
      'desktop/sidecar/services/adLearningFacade.js',
      'desktop/sidecar/services/crawlRuntimeFactory.js',
      'test/sharedFacadeRuntime.test.js'
    ]],
    ['desktop/sidecar/services/organizerCompatSettings.js', [
      'desktop/sidecar/services/adLearningFacade.js',
      'desktop/sidecar/services/organizerFacade.js',
      'test/organizerCompatSettings.test.js'
    ]],
    ['desktop/sidecar/services/organizerEventMirror.js', [
      'desktop/sidecar/services/adLearningFacade.js',
      'desktop/sidecar/services/organizerFacade.js',
      'test/organizerEventMirror.test.js'
    ]],
    ['desktop/sidecar/services/eventBus.js', ['desktop/sidecar/index.js', 'test/sidecarEventBus.test.js']],
    ['desktop/sidecar/services/goRunnerHost.js', ['desktop/sidecar/services/crawlRuntimeFactory.js']],
    ['desktop/sidecar/services/organizerFacade.js', ['desktop/sidecar/serviceRegistry.js']],
    ['desktop/sidecar/services/taskLogService.js', ['desktop/sidecar/services/crawlRuntimeFactory.js', 'test/taskLogService.test.js']]
  ];

  return buildLegacyAllowlistEntries([
    ...mainServiceEntries,
    ...organizerPhaseEntries,
    ...sidecarEntryEntries,
    ...sidecarServiceEntries
  ]);
}

function createLegacyCompatibilityUtilityChecks() {
  return [
    {
      target: 'desktop/mainServices/settingsStore.js',
      allowedImporters: [
        'desktop/sidecar/services/sharedFacadeRuntime.js',
        'test/settingsStoreRankingHistory.test.js'
      ]
    },
    {
      target: 'desktop/mainServices/proxyValidationService.js',
      allowedImporters: [
        'desktop/sidecar/serviceRegistry.js',
        'test/proxyValidationService.test.js'
      ]
    },
    {
      target: 'desktop/sidecar/runtimePaths.js',
      allowedImporters: [
        'desktop/sidecar/commandRouter.js',
        'desktop/sidecar/services/crawlRuntimeFactory.js',
        'desktop/sidecar/services/sharedFacadeRuntime.js',
        'desktop/sidecar/services/rankingFacade.js',
        'test/rankingFacade.test.js',
        'test/sidecarCommandRouter.test.js',
        'test/sidecarRuntimePaths.test.js'
      ]
    },
    {
      target: 'desktop/sidecar/services/sharedFacadeRuntime.js',
      allowedImporters: [
        'desktop/sidecar/services/adLearningFacade.js',
        'desktop/sidecar/services/crawlRuntimeFactory.js',
        'test/sharedFacadeRuntime.test.js'
      ]
    },
    {
      target: 'desktop/sidecar/services/taskLogService.js',
      allowedImporters: [
        'desktop/sidecar/services/crawlRuntimeFactory.js',
        'test/taskLogService.test.js'
      ]
    },
    {
      target: 'desktop/sidecar/services/crawlRuntimeFactory.js',
      allowedImporters: [
        'desktop/sidecar/services/crawlService.js',
        'test/crawlRuntimeFactory.test.js'
      ]
    }
  ];
}

function createLegacyCompatibilityUtilitySearchRoots(rootDir) {
  return [
    jsRoot(desktopPath(rootDir)),
    jsRoot(scriptsPath(rootDir)),
    jsRoot(testPath(rootDir))
  ];
}

function createArchivedSidecarOwnershipSearchRoots(rootDir) {
  return [
    { root: desktopPath(rootDir), extensions: new Set(['.js']) },
    { root: scriptsPath(rootDir), extensions: new Set(['.js']) },
    { root: testPath(rootDir), extensions: new Set(['.js']) },
    { root: internalPath(rootDir), extensions: new Set(['.go']) }
  ];
}

function createRetiredElectronShellOwnershipSearchRoots(rootDir) {
  return [
    { root: desktopPath(rootDir), extensions: new Set(['.js']) },
    { root: scriptsPath(rootDir), extensions: new Set(['.js']) },
    { root: internalPath(rootDir), extensions: new Set(['.go']) }
  ];
}

function createLegacyBoundaryMarkers(rootDir) {
  const scriptAssetEntries = [
    ['build-demo-variants.js', 'archive/demo only'],
    ['bundle-js.js', 'Generated desktop JS bundler for the current Wails frontend build chain.'],
    ['bundle-css.js', 'Generated desktop CSS bundler for the current Wails frontend build chain.'],
    ['assemble-html.js', 'Generated desktop HTML assembler for the current Wails frontend build chain.'],
    ['check-encoding.js', 'Repository-wide UTF-8 guard for source and docs under active maintenance.'],
    ['frontend-paths.js', 'Shared path registry for the current desktop frontend build/sync pipeline.'],
    ['frontend-bundle-files.js', 'Single source of truth for active desktop frontend JS bundle membership.'],
    ['verify-frontend-text.js', 'Guard active frontend text against mojibake and missing required UI labels.'],
    ['verify-maintainability-config.js', 'Centralized boundary config for maintainability verification.'],
    ['verify-maintainability.js', 'Main maintainability verification entrypoint.'],
    ['inspect-utf8.js', 'UTF-8 source-of-truth inspector for active maintenance work.'],
    ['build-wails-dual-packages.js', 'Node-compatible runtime stage'],
    ['build-wails-go-lite.js', 'target release shape for future cleaner maintenance']
  ];
  const scriptRuntimeEntries = [
    ['run-wails-build.js', 'Primary desktop build entry for the current product'],
    ['wails-paths.js', 'Shared Wails build/release path and binary resolution helpers.'],
    ['nsis-paths.js', 'Shared NSIS compiler discovery helper for maintained Windows packaging'],
    ['run-wails-dev.js', 'Primary desktop dev entry for the current Wails runtime'],
    ['sync-wails-frontend.js', 'part of the current build chain'],
    ['smoke-wails-phase1.js', 'Minimal sidecar bootstrap smoke for the Wails compatibility lane'],
    ['smoke-wails-phase1-crawl.js', 'Crawl-start smoke for the Node sidecar compatibility lane'],
    ['smoke-desktop-ui.js', 'Packaged desktop UI smoke for the current Wails EXE'],
    ['build-lite-exe.js', 'Portable-lite wrapper packager around the current release executable.'],
    ['postbuild.js', 'Release-path sync step for the current Wails build chain.'],
    ['prepare-nsis-templates.js', 'NSIS template patcher for the maintained installer experience.'],
    ['create-workspace-backup.ps1', 'Workspace backup helper for local maintenance snapshots.']
  ];
  const organizerPhaseEntries = [
    [['organizerModules', 'cleanupPhase.js'], 'Legacy organizer compatibility phase: cleanup runs last and stays narrow'],
    [['organizerModules', 'introAdPhase.js'], 'Legacy organizer compatibility phase for post-move intro-ad review'],
    [['organizerModules', 'judgePhase.js'], 'Legacy organizer compatibility phase for raw file classification'],
    [['organizerModules', 'renamePhase.js'], 'Legacy organizer compatibility phase for rename/move execution'],
    [['organizerModules', 'reportPhase.js'], 'Legacy organizer compatibility phase for operator-facing report output'],
    [['organizerModules', 'scanPhase.js'], 'Legacy organizer compatibility phase for filesystem enumeration']
  ];
  const legacyDesktopRuntimeEntries = [
    ['runnerService.js', 'Legacy crawl runtime shared by the Node sidecar compatibility path.'],
    ['organizerService.js', 'Legacy organizer implementation reused by the Node sidecar facade.'],
    ['adLearningService.js', 'Legacy ad-learning implementation reused by the Node sidecar facade.'],
    ['proxyValidationService.js', 'Proxy validation helper retained for desktop compatibility flows.'],
    ['settingsStore.js', 'Shared desktop settings persistence used by legacy desktop modules and the'],
    ['runtimeState.js', 'Lightweight in-memory state container for the legacy desktop/sidecar runtime.'],
    ['logBridge.js', 'Shared task/session log adapter for legacy desktop modules and the Node']
  ];
  const sidecarCompatibilityEntries = [
    ['runtimePaths.js', 'Go/Wails runtime resolution is the source of truth'],
    ['serviceRegistry.js', 'Shared sidecar compatibility service registry.'],
    [['services', 'eventBus.js'], 'protocol plumbing for the Node compatibility lane'],
    [['services', 'crawlRuntimeFactory.js'], 'Shared sidecar runtime assembly for the active crawl compatibility lane.'],
    [['services', 'rankingFacade.js'], 'Shared sidecar facade for actress ranking / target-resolution compatibility.'],
    [['services', 'sharedFacadeRuntime.js'], 'Shared sidecar runtime helpers for compatibility facades.'],
    [['services', 'organizerCompatSettings.js'], 'Shared organizer/ad-learning compatibility settings helpers.'],
    [['services', 'organizerEventMirror.js'], 'Shared organizer-side event mirroring helper for compatibility facades.'],
    [['services', 'taskLogService.js'], 'if current Wails logs are wrong but sidecar is not involved'],
    [['services', 'adLearningFacade.js'], 'if a bug reproduces in current organizer UI without the sidecar'],
    ['index.js', 'Entry point for the Node sidecar process used by the Wails compatibility'],
    ['commandRouter.js', 'Node sidecar command router used by the Wails compatibility path.']
  ];
  const bridgeDomainEntries = [
    [['bridge', 'api.go'], 'Package bridge is the current Wails desktop command boundary'],
    [['bridge', 'api_facades.go'], 'This file declares the bridge-owned facade groupings'],
    [['bridge', 'api_runtime_state.go'], 'Runtime-state helpers are the bridge-owned synchronization point'],
    [['bridge', 'api_subscription_v2_crawl.go'], 'This file implements domain logic for the javflow backend.']
  ];
  const goServiceBoundaryEntries = [
    [['crawlrunner', 'runner.go'], 'Package crawlrunner owns the Go-native crawl state machine and execution'],
    [['crawltask', 'service.go'], 'Package crawltask is the unified task-controller layer between the bridge'],
    [['crawlquality', 'service.go'], 'Package crawlquality summarizes crawl outputs, logs, and quality diagnostics'],
    [['actresslookup', 'service.go'], 'Package actresslookup resolves actress crawl targets and count hints without'],
    [['actressranking', 'service.go'], 'Package actressranking serves normalized actress ranking data from online and'],
    [['adlearning', 'service.go'], 'Package adlearning owns Go-native ad-learning state, samples, and evaluation'],
    [['antiblock', 'service.go'], 'Package antiblock refreshes anti-block URLs and keeps the app\'s fallback base list current.'],
    [['crawlfetch', 'service.go'], 'Package crawlfetch wraps index, detail, and magnet fetch operations for the Go crawl path.'],
    [['crawlresult', 'service.go'], 'Package crawlresult builds the renderer-facing output/result panel read model.'],
    [['crawlreview', 'service.go'], 'Package crawlreview keeps the renderer-facing duplicate, unfinished, and filtered review panel.'],
    [['crawlruncontext', 'service.go'], 'Package crawlruncontext owns the canonical read model for current crawl artifact paths.'],
    [['crawlstage', 'service.go'], 'Package crawlstage builds the renderer-facing phase/stage panel read model.'],
    [['crawluistate', 'service.go'], 'Package crawluistate normalizes raw crawl events into the renderer-facing ui-state panel.'],
    [['dependency', 'service.go'], 'Package dependency manages runtime prerequisites such as FFmpeg and ONNX.'],
    [['proxy', 'service.go'], 'Package proxy validates proxy settings and normalizes proxy/target inputs for the desktop app.'],
    [['runtimecache', 'state.go'], 'Package runtimecache stores last-observed runtime snapshots for bridge/bootstrap read models.'],
    [['runtime', 'paths.go'], 'Package runtimepaths resolves the desktop runtime\'s coarse-grained filesystem'],
    [['desktop', 'dialogs.go'], 'Package desktop wraps Wails desktop shell integrations such as dialogs and local path actions.'],
    [['settings', 'store.go'], 'Package settings persists desktop settings and default path conventions for the current app.'],
    [['organizer', 'service.go'], 'Package organizer is the current Go-native video organizer domain'],
    [['avsubscriptionv2', 'types.go'], 'Package avsubscriptionv2 owns the rebuilt AV-subscription workflow.'],
    [['common', 'common.go'], 'Package common contains narrow cross-domain helpers'],
    [['contracts', 'crawlartifact', 'artifacts.go'], 'Package crawlartifact defines the persisted crawl-output contract shared by'],
    [['contracts', 'crawlartifact', 'paths.go'], 'This file owns the canonical crawl artifact path contract used by Go-side'],
    [['contracts', 'subscriptiontarget', 'target_profile.go'], 'Package subscriptiontarget defines the lightweight target contract shared by']
  ];
  const commonBoundaryEntries = [
    [['text', 'uiTextSource.js'], 'Active UI text source for the current desktop frontend.'],
    [['text', 'appInfo.js'], 'Shared app identity text for the active desktop frontend/runtime bundle.'],
    [['text', 'serviceText.js'], 'Shared service-layer wording for active desktop ranking/lookup features.'],
    [['text', 'versionHistory.js'], 'Shared version-history text used by the active desktop hero panels.'],
    ['appText.js', 'appText.js is the shared text aggregator'],
    ['bridgeProtocol.js', 'Shared bridge protocol constants used by both renderer and compatibility'],
    ['progressSchema.js', 'Shared organizer/learning progress vocabulary.'],
    ['javBusActressLookupService.js', 'Responsibility boundary:'],
    [['text', 'runtimeText.js'], 'Active runtime text source for log/state wording shared by renderer and'],
    [['text', 'taskConfig.js'], 'Active task-config text source for shared crawler templates and status labels.'],
    ['crawlPanelModel.js', 'Shared crawl-panel normalization model for the active desktop renderer.'],
    ['actressRankingService.js', 'Shared actress-ranking service used by the active desktop ranking workflow.'],
    ['actressRankingAvfanSource.js', 'AVfan ranking fetcher for the active desktop ranking workflow.'],
    ['actressRankingOfficialSource.js', 'Official DMM/FANZA ranking fetcher for the active desktop ranking workflow.'],
    ['actressRankingLocalHistory.js', 'Local ranking-history loader for the active desktop ranking workflow.'],
    ['actressRankingShared.js', 'Shared ranking-source utilities for the active desktop ranking workflow.']
  ];
  const rendererBoundaryEntries = [
    ['uiText.js', 'Renderer fallback text bootstrap for the current desktop UI.'],
    ['rendererShellController.js', 'Text ownership rule:'],
    ['appUpdateController.js', 'Renderer controller for the portable JavFlow update affordance.'],
    ['subscriptionController.js', 'Subscription controller manages lightweight crawl seeds plus refresh results.'],
    ['index.template.html', 'Active desktop HTML template shell.'],
    ['renderer.js', 'Main renderer bootstrap for the current desktop UI.'],
    ['platformBridge.js', 'Thin selector that exposes the active desktop bridge implementation.'],
    ['platformBridge.wails.js', 'Primary frontend bridge for the current Wails desktop runtime.'],
    ['crawlRuntimeController.js', 'Crawl runtime controller owns crawler-specific renderer hydration:'],
    ['stateController.js', 'State controller owns UI-facing aggregation of crawl/task/runtime state.'],
    ['formController.js', 'Form controller owns crawler setup inputs and client-side validation.'],
    ['rankingController.js', 'Ranking controller is the renderer-facing surface for actress ranking'],
    ['logController.js', 'Log controller is the renderer-side projection of backend logs.'],
    ['crawlResultHistoryController.js', 'Crawl result-history controller owns:'],
    ['rendererBootstrapController.js', 'Renderer bootstrap controller owns startup orchestration:'],
    ['organizerController.js', 'Organizer workspace controller for the current desktop renderer.'],
    ['organizerDependencyController.js', 'Organizer dependency controller for the current desktop renderer.'],
    ['organizerLearningController.js', 'Organizer learning controller for the current desktop renderer.'],
    ['organizerCrawlOutputController.js', 'Organizer crawl-output controller for the current desktop renderer.']
  ];
  const executionBoundaryEntries = [
    [['crawlexecution', 'index_page_plan.go'], 'runtime summary text that depends on crawl state may live here'],
    [['crawlexecution', 'controller_commands.go'], 'Controller-command decisions keep UI/bridge stop-restart requests separate'],
    [['crawlexecution', 'controller_transition.go'], 'Observed-state transitions own the controller\'s response to runner final'],
    [['crawlexecution', 'recovery.go'], 'Shared recovery message/merge helpers live here so queue-gap, page-gap, and'],
    [['crawlexecution', 'page_gap_recovery.go'], 'Page-gap recovery decisions own the second-pass planning used after index'],
    [['crawlexecution', 'reconciliation.go'], 'Reconciliation models the gap analysis between expected, queued, processed,'],
    [['crawlexecution', 'status.go'], 'Status helpers translate raw runner/controller status into the small shared'],
    [['crawlexecution', 'detail_recovery.go'], 'Detail-recovery decisions own the retry-budget pass for detail-page failures'],
    [['crawlexecution', 'final_state.go'], 'Final-state synthesis converts reconciliation/recovery outcomes into the']
  ];
  const crawlInfraEntries = [
    [['crawlrequest', 'browser_fallback.go'], 'Browser fallback owns the explicit chromedp-based compatibility lane for'],
    [['crawlrequest', 'page.go'], 'Page request helpers are the primary HTTP fetch boundary for index/detail'],
    [['crawloutput', 'output.go'], 'Package crawloutput owns the persisted crawler artifacts written during and'],
    [['organizer', 'run.go'], 'run.go owns the organizer\'s top-level orchestration across scan, transfer,'],
    [['crawlconfig', 'config.go'], 'Package crawlconfig owns the normalized crawl-run configuration shape shared'],
    [['crawlconfig', 'useragents.go'], 'User-agent helpers stay in crawlconfig so request behavior can rotate agents'],
    [['crawlidentity', 'identity.go'], 'Package crawlidentity owns film-code normalization and extraction rules used'],
    [['crawlindex', 'index.go'], 'Package crawlindex owns index-page URL planning and link-level bookkeeping'],
    [['crawlparse', 'parser.go'], 'Package crawlparse owns the HTML-to-structured-data extraction layer for'],
    [['crawlqueue', 'queue.go'], 'Package crawlqueue owns the worker queue and queue event stream used by the'],
    [['events', 'bus.go'], 'Package events owns the lightweight in-process event bus used by bridge-side'],
    [['crawltask', 'output_resolution.go'], 'Output resolution owns the task-controller rule for choosing a fresh run'],
    [['crawltask', 'task_log.go'], 'task_log.go owns the operator-facing UTF-8 task log envelope written by the'],
    [['crawltaskstate', 'manager.go'], 'manager.go owns the persisted task-state directory layout and file handles'],
    [['crawltaskstate', 'persisted_output.go'], 'persisted_output.go inspects on-disk crawl artifacts and summarizes what was'],
    [['crawltaskstate', 'types.go'], 'types.go owns the persisted state contract shared across resume, validation,'],
    [['crawlresult', 'observer.go'], 'Result observer owns deduplicated event emission for the renderer-facing'],
    [['crawlreview', 'observer.go'], 'Review observer owns deduplicated event emission for duplicate/unfinished/'],
    [['crawlruncontext', 'observer.go'], 'Run-context observer owns deduplicated event emission for crawl artifact path'],
    [['crawlstage', 'observer.go'], 'Stage observer owns deduplicated event emission for the renderer-facing'],
    [['crawluistate', 'observer.go'], 'UI-state observer owns deduplicated event emission for the renderer\'s live']
  ];

  return [
    ...markerChecksFromEntries(rootDir, scriptsPath, scriptAssetEntries),
    ...markerChecksFromEntries(rootDir, scriptsPath, scriptRuntimeEntries),
    ...markerChecksFromEntries(rootDir, mainServicesPath, organizerPhaseEntries),
    ...markerChecksFromEntries(rootDir, sidecarPath, sidecarCompatibilityEntries),
    ...markerChecksFromEntries(rootDir, mainServicesPath, legacyDesktopRuntimeEntries),
    ...markerChecksFromEntries(rootDir, internalPath, bridgeDomainEntries),
    ...markerChecksFromEntries(rootDir, internalPath, goServiceBoundaryEntries),
    ...markerChecksFromEntries(rootDir, commonPath, commonBoundaryEntries),
    ...markerChecksFromEntries(rootDir, rendererPath, rendererBoundaryEntries),
    ...markerChecksFromEntries(rootDir, internalPath, executionBoundaryEntries),
    ...markerChecksFromEntries(rootDir, internalPath, crawlInfraEntries)
  ];
}

function createArchivedSidecarOwnershipChecks() {
  const markerEntries = [
    ['archived sidecar adlearning facade marker ownership', 'marker=archived-sidecar-adlearning-facade', ['desktop/sidecar/services/adLearningFacade.js']],
    ['archived sidecar organizer facade marker ownership', 'marker=archived-sidecar-organizer-facade', ['desktop/sidecar/services/organizerFacade.js']],
    ['archived sidecar ranking facade marker ownership', 'marker=archived-sidecar-ranking-facade', ['desktop/sidecar/services/rankingFacade.js']],
    ['archived sidecar organizer compat settings marker ownership', 'marker=archived-sidecar-organizer-compat-settings', ['desktop/sidecar/services/organizerCompatSettings.js']],
    ['archived sidecar organizer event mirror marker ownership', 'marker=archived-sidecar-organizer-event-mirror', ['desktop/sidecar/services/organizerEventMirror.js']]
  ];
  const helperEntries = [
    ['archived sidecar adlearning facade constructor ownership', 'createAdLearningFacade', ['desktop/sidecar/serviceRegistry.js', 'desktop/sidecar/services/adLearningFacade.js']],
    ['archived sidecar organizer facade constructor ownership', 'createOrganizerFacade', ['desktop/sidecar/serviceRegistry.js', 'desktop/sidecar/services/organizerFacade.js']],
    ['archived sidecar ranking facade constructor ownership', 'createRankingFacade', ['desktop/sidecar/serviceRegistry.js', 'desktop/sidecar/services/rankingFacade.js', 'test/rankingFacade.test.js']],
    ['archived sidecar organizer compat settings helper ownership', 'buildOrganizerSettingsPatch', ['desktop/sidecar/services/organizerCompatSettings.js', 'desktop/sidecar/services/organizerFacade.js', 'test/organizerCompatSettings.test.js']],
    ['archived sidecar adlearning settings patch helper ownership', 'buildAdLearningSettingsPatch', ['desktop/sidecar/services/adLearningFacade.js', 'desktop/sidecar/services/organizerCompatSettings.js', 'test/organizerCompatSettings.test.js']],
    ['archived sidecar organizer event mirror helper ownership', 'createOrganizerEventMirror', ['desktop/sidecar/services/adLearningFacade.js', 'desktop/sidecar/services/organizerEventMirror.js', 'desktop/sidecar/services/organizerFacade.js', 'test/organizerEventMirror.test.js']]
  ];

  return [
    {
      label: 'archived sidecar env flag ownership',
      target: 'JAV_ENABLE_ARCHIVED_SIDECAR_DOMAINS',
      allowedFiles: [
        'desktop/sidecar/serviceRegistry.js',
        'scripts/verify-maintainability-config.js',
        'test/sidecarCommandRouter.test.js',
        'test/sidecarServiceRegistry.test.js'
      ]
    },
    {
      label: 'legacy sidecar entry path ownership',
      target: 'desktop/sidecar/index.js',
      allowedFiles: [
        'scripts/verify-maintainability-config.js',
        'wails-shell/internal/interfaces/interfaces.go',
        'wails-shell/internal/messages/messages.go'
      ]
    },
    ...markerEntries.map(([label, target, allowedFiles]) =>
      ownershipCheck(label, target, allowedFiles, { excludeVerifyConfig: true })
    ),
    ...helperEntries.map(([label, target, allowedFiles]) =>
      ownershipCheck(label, target, allowedFiles, { excludeVerifyConfig: true })
    )
  ];
}

function createCompatibilityCrawlOwnershipChecks() {
  const markerEntries = [
    ['compat sidecar command router marker ownership', 'marker=compat-sidecar-command-router', ['desktop/sidecar/commandRouter.js']],
    ['compat sidecar service registry marker ownership', 'marker=compat-sidecar-service-registry', ['desktop/sidecar/serviceRegistry.js']],
    ['compat sidecar shared runtime marker ownership', 'marker=compat-sidecar-shared-facade-runtime', ['desktop/sidecar/services/sharedFacadeRuntime.js']],
    ['compat mainservices settings store marker ownership', 'marker=compat-mainservices-settings-store', ['desktop/mainServices/settingsStore.js']],
    ['compat sidecar entry marker ownership', 'marker=compat-sidecar-entry', ['desktop/sidecar/index.js']],
    ['compat sidecar runtime paths marker ownership', 'marker=compat-sidecar-runtime-paths', ['desktop/sidecar/runtimePaths.js']],
    ['compat sidecar crawl service marker ownership', 'marker=compat-sidecar-crawl-service', ['desktop/sidecar/services/crawlService.js']],
    ['compat sidecar crawl runtime factory marker ownership', 'marker=compat-sidecar-crawl-runtime-factory', ['desktop/sidecar/services/crawlRuntimeFactory.js']],
    ['compat sidecar event bus marker ownership', 'marker=compat-sidecar-event-bus', ['desktop/sidecar/services/eventBus.js']],
    ['compat sidecar go runner host marker ownership', 'marker=compat-sidecar-go-runner-host', ['desktop/sidecar/services/goRunnerHost.js']],
    ['compat sidecar task log service marker ownership', 'marker=compat-sidecar-task-log-service', ['desktop/sidecar/services/taskLogService.js']],
    ['compat mainservices log bridge marker ownership', 'marker=compat-mainservices-log-bridge', ['desktop/mainServices/logBridge.js']],
    ['compat mainservices runner service marker ownership', 'marker=compat-mainservices-runner-service', ['desktop/mainServices/runnerService.js']],
    ['compat mainservices runtime state marker ownership', 'marker=compat-mainservices-runtime-state', ['desktop/mainServices/runtimeState.js']],
    ['compat mainservices proxy validation marker ownership', 'marker=compat-mainservices-proxy-validation', ['desktop/mainServices/proxyValidationService.js']]
  ];
  const constructorEntries = [
    ['compat sidecar command router constructor ownership', 'createCommandRouter', ['desktop/sidecar/commandRouter.js', 'desktop/sidecar/index.js', 'test/sidecarCommandRouter.test.js']],
    ['compat sidecar service registry constructor ownership', 'createServiceRegistry', ['desktop/sidecar/commandRouter.js', 'desktop/sidecar/serviceRegistry.js', 'test/sidecarServiceRegistry.test.js']],
    ['compat sidecar shared runtime constructor ownership', 'createSidecarSettingsStore', ['desktop/sidecar/services/adLearningFacade.js', 'desktop/sidecar/services/crawlRuntimeFactory.js', 'desktop/sidecar/services/sharedFacadeRuntime.js', 'test/sharedFacadeRuntime.test.js']],
    ['compat sidecar runtime state constructor ownership', 'createRuntimeState', ['desktop/mainServices/runtimeState.js', 'desktop/sidecar/services/crawlRuntimeFactory.js']],
    ['compat mainservices runner service constructor ownership', 'createRunnerService', ['desktop/mainServices/runnerService.js', 'desktop/sidecar/services/crawlRuntimeFactory.js', 'test/runnerService.test.js']],
    ['compat sidecar go runner host constructor ownership', 'createGoRunnerHost', ['desktop/sidecar/services/crawlRuntimeFactory.js', 'desktop/sidecar/services/goRunnerHost.js']],
    ['compat sidecar task log service constructor ownership', 'createTaskLogService', ['desktop/sidecar/services/crawlRuntimeFactory.js', 'desktop/sidecar/services/taskLogService.js', 'test/taskLogService.test.js']],
    ['compat sidecar app shim constructor ownership', 'createAppShim', ['desktop/sidecar/runtimePaths.js', 'desktop/sidecar/services/crawlRuntimeFactory.js', 'desktop/sidecar/services/sharedFacadeRuntime.js', 'test/sidecarRuntimePaths.test.js']],
    ['compat sidecar crawl service constructor ownership', 'createCrawlService', ['desktop/sidecar/serviceRegistry.js', 'desktop/sidecar/services/crawlService.js', 'test/sidecarCommandRouter.test.js']],
    ['compat sidecar event bus constructor ownership', 'createEventBus', ['desktop/sidecar/index.js', 'desktop/sidecar/services/eventBus.js', 'test/crawlRuntimeFactory.test.js', 'test/sidecarCommandRouter.test.js', 'test/sidecarEventBus.test.js', 'test/sidecarServiceRegistry.test.js']],
    ['compat proxy validation service constructor ownership', 'createProxyValidationService', ['desktop/mainServices/proxyValidationService.js', 'desktop/sidecar/serviceRegistry.js', 'test/proxyValidationService.test.js']],
    ['compat log bridge constructor ownership', 'createLogBridge', ['desktop/mainServices/logBridge.js', 'desktop/sidecar/services/taskLogService.js', 'test/logBridge.test.js']]
  ];

  return [
    ...markerEntries.map(([label, target, allowedFiles]) =>
      ownershipCheck(label, target, allowedFiles, { excludeVerifyConfig: true })
    ),
    ...constructorEntries.map(([label, target, allowedFiles]) =>
      ownershipCheck(label, target, allowedFiles, { excludeVerifyConfig: true })
    )
  ];
}

function createRetiredElectronShellOwnershipChecks() {
  const pathEntries = [
    ['retired electron window service path ownership', 'desktop/mainServices/windowService.js', ['scripts/verify-maintainability-config.js']],
    ['retired electron window service constructor ownership', 'createWindowService', ['desktop/mainServices/windowService.js', 'scripts/verify-maintainability-config.js']]
  ];
  const markerEntries = [
    ['retired electron window service deprecation marker', 'marker=retired-electron-window-service', ['desktop/mainServices/windowService.js']],
    ['archived demo packager deprecation marker', 'marker=archived-electron-demo-packager', ['scripts/build-demo-variants.js']],
    ['archived mainservices organizer service marker ownership', 'marker=archived-mainservices-organizer-service', ['desktop/mainServices/organizerService.js']],
    ['archived mainservices adlearning service marker ownership', 'marker=archived-mainservices-adlearning-service', ['desktop/mainServices/adLearningService.js']],
    ['archived organizer scan phase marker ownership', 'marker=archived-organizer-phase-scan', ['desktop/mainServices/organizerModules/scanPhase.js']],
    ['archived organizer judge phase marker ownership', 'marker=archived-organizer-phase-judge', ['desktop/mainServices/organizerModules/judgePhase.js']],
    ['archived organizer rename phase marker ownership', 'marker=archived-organizer-phase-rename', ['desktop/mainServices/organizerModules/renamePhase.js']],
    ['archived organizer intro-ad phase marker ownership', 'marker=archived-organizer-phase-intro-ad', ['desktop/mainServices/organizerModules/introAdPhase.js']],
    ['archived organizer report phase marker ownership', 'marker=archived-organizer-phase-report', ['desktop/mainServices/organizerModules/reportPhase.js']],
    ['archived organizer cleanup phase marker ownership', 'marker=archived-organizer-phase-cleanup', ['desktop/mainServices/organizerModules/cleanupPhase.js']]
  ];
  const helperEntries = [
    ['archived mainservices organizer service constructor ownership', 'createOrganizerService', ['desktop/mainServices/organizerService.js', 'desktop/sidecar/services/organizerFacade.js']],
    ['archived mainservices adlearning service constructor ownership', 'createAdLearningService', ['desktop/mainServices/adLearningService.js', 'desktop/sidecar/services/adLearningFacade.js']],
    ['archived mainservices settings store constructor ownership', 'createSettingsStore', ['desktop/mainServices/settingsStore.js', 'desktop/sidecar/services/sharedFacadeRuntime.js']],
    ['archived organizer scan phase entry ownership', 'runScanPhase', ['desktop/mainServices/organizerModules/scanPhase.js', 'desktop/mainServices/organizerService.js']],
    ['archived organizer judge phase entry ownership', 'runJudgePhase', ['desktop/mainServices/organizerModules/judgePhase.js', 'desktop/mainServices/organizerService.js']],
    ['archived organizer rename phase entry ownership', 'runRenamePhase', ['desktop/mainServices/organizerModules/renamePhase.js', 'desktop/mainServices/organizerService.js']],
    ['archived organizer intro-ad phase entry ownership', 'runIntroAdPhase', ['desktop/mainServices/organizerModules/introAdPhase.js', 'desktop/mainServices/organizerService.js']],
    ['archived organizer report phase entry ownership', 'runReportPhase', ['desktop/mainServices/organizerModules/reportPhase.js', 'desktop/mainServices/organizerService.js']],
    ['archived organizer cleanup phase entry ownership', 'runCleanupPhase', ['desktop/mainServices/organizerModules/cleanupPhase.js', 'desktop/mainServices/organizerService.js']]
  ];

  return [
    ...pathEntries.map(([label, target, allowedFiles]) =>
      ownershipCheck(label, target, allowedFiles)
    ),
    ...markerEntries.map(([label, target, allowedFiles]) =>
      ownershipCheck(label, target, allowedFiles, { excludeVerifyConfig: true })
    ),
    ...helperEntries.map(([label, target, allowedFiles]) =>
      ownershipCheck(label, target, allowedFiles, { excludeVerifyConfig: true })
    )
  ];
}

function createFocusedMaintainabilityHeaderChecks(rootDir) {
  const commonEntries = [
    ['progressSchema.js', MAINTENANCE_HEADER_SNIPPETS],
    ['actressRankingAvfanSource.js'],
    ['appText.js'],
    ['bridgeProtocol.js'],
    ['actressRankingService.js', MAINTENANCE_HEADER_SNIPPETS],
    ['actressRankingShared.js'],
    ['actressRankingLocalHistory.js'],
    ['actressRankingOfficialSource.js'],
    ['javBusActressLookupService.js', ['Responsibility boundary:', 'File map for maintainers:']],
    ['crawlPanelModel.js'],
    ['rendererHelpers.js'],
    [['text', 'serviceText.js']],
    [['text', 'appInfo.js']],
    [['text', 'runtimeText.js']],
    [['text', 'taskConfig.js']],
    [['text', 'uiTextSource.js']],
    [['text', 'versionHistory.js']]
  ];
  const rendererEntries = [
    ['platformBridge.wails.js', MAINTENANCE_HEADER_SNIPPETS],
    ['rendererBootstrapController.js', BOUNDARY_HEADER_SNIPPETS],
    ['renderer.js'],
    ['rendererElements.js'],
    ['rendererShellController.js'],
    ['rendererShellView.js'],
    ['rendererElementDomains.js'],
    ['logController.js', BOUNDARY_HEADER_SNIPPETS],
    ['crawlResultHistoryController.js', BOUNDARY_HEADER_SNIPPETS],
    ['reviewPanelRenderer.js', BOUNDARY_HEADER_SNIPPETS],
    ['artifactInputHelper.js'],
    ['crawlResultHistoryView.js'],
    ['crawlRuntimeController.js'],
    ['formController.js'],
    ['stateController.js'],
    ['stateHelpers.js'],
    ['resultPanelRenderer.js'],
    ['stagePanelRenderer.js'],
    ['subscriptionController.js'],
    ['subscriptionListView.js'],
    ['organizerController.js'],
    ['organizerCrawlOutputController.js'],
    ['organizerDependencyController.js'],
    ['organizerDependencyView.js'],
    ['organizerLearningController.js'],
    ['organizerLearningView.js'],
    ['organizerReviewView.js'],
    ['rankingController.js'],
    ['rankingListView.js'],
    ['platformBridge.js'],
    ['uiText.js']
  ];
  const mainServicesEntries = [
    ['settingsStore.js'],
    ['runnerService.js'],
    ['organizerService.js'],
    ['adLearningService.js'],
    ['proxyValidationService.js'],
    ['runtimeState.js'],
    ['windowService.js'],
    ['logBridge.js'],
    [['organizerModules', 'scanPhase.js']],
    [['organizerModules', 'judgePhase.js']],
    [['organizerModules', 'renamePhase.js']],
    [['organizerModules', 'introAdPhase.js']],
    [['organizerModules', 'reportPhase.js']],
    [['organizerModules', 'cleanupPhase.js']]
  ];
  const sidecarEntries = [
    ['runtimePaths.js', MAINTENANCE_RULE_HEADER_SNIPPETS],
    ['index.js'],
    ['commandRouter.js', CURRENT_MAINTENANCE_HEADER_SNIPPETS],
    ['serviceRegistry.js'],
    [['services', 'adLearningFacade.js'], CURRENT_MAINTENANCE_HEADER_SNIPPETS],
    [['services', 'organizerFacade.js'], CURRENT_MAINTENANCE_HEADER_SNIPPETS],
    [['services', 'rankingFacade.js'], CURRENT_MAINTENANCE_HEADER_SNIPPETS],
    [['services', 'organizerCompatSettings.js']],
    [['services', 'organizerEventMirror.js']],
    [['services', 'sharedFacadeRuntime.js']],
    [['services', 'eventBus.js']],
    [['services', 'crawlService.js']],
    [['services', 'crawlRuntimeFactory.js'], ['Current product crawl ownership still belongs to the Go task controller first.', 'File map for maintainers:']],
    [['services', 'goRunnerHost.js'], ['This file must not become a second task-controller policy layer.', 'File map for maintainers:']],
    [['services', 'taskLogService.js'], MAINTENANCE_RULE_HEADER_SNIPPETS]
  ];
  const internalEntries = [
    [['crawlrunner', 'runner.go'], MAINTENANCE_OWNERSHIP_HEADER_SNIPPETS],
    [['crawlquality', 'service.go'], MAINTENANCE_OWNERSHIP_HEADER_SNIPPETS],
    [['crawltask', 'service.go'], MAINTENANCE_OWNERSHIP_HEADER_SNIPPETS],
    [['crawlrunner', 'diagnostics.go']],
    [['organizer', 'fs_ops.go']],
    [['bridge', 'api_output_context.go']],
    [['bridge', 'api_settings_shared.go']],
    [['bridge', 'api.go']],
    [['bridge', 'api_dispatch_crawl_lifecycle.go']],
    [['bridge', 'api_crawl_mode_selection.go']],
    [['organizer', 'run_scan_phase.go']],
    [['organizer', 'run_review_phase.go']],
    [['crawlreview', 'service.go'], OWNERSHIP_BOUNDARY_HEADER_SNIPPETS],
    [['crawlresult', 'service.go'], OWNERSHIP_BOUNDARY_HEADER_SNIPPETS],
    [['crawlstage', 'service.go'], OWNERSHIP_BOUNDARY_HEADER_SNIPPETS],
    [['crawluistate', 'service.go'], ['Boundary rule:', 'NewService keeps one central active-item truncation rule']],
    [['crawlruncontext', 'service.go'], ['Ownership summary:', 'single owner for "which crawl output/log/artifact paths']]
  ];

  return [
    ...snippetChecksFromEntries(rootDir, commonPath, commonEntries, OWNERSHIP_HEADER_SNIPPETS),
    ...snippetChecksFromEntries(rootDir, rendererPath, rendererEntries, OWNERSHIP_HEADER_SNIPPETS),
    ...snippetChecksFromEntries(rootDir, mainServicesPath, mainServicesEntries, OWNERSHIP_HEADER_SNIPPETS),
    ...snippetChecksFromEntries(rootDir, sidecarPath, sidecarEntries, OWNERSHIP_HEADER_SNIPPETS),
    ...snippetChecksFromEntries(rootDir, internalPath, internalEntries, OWNERSHIP_HEADER_SNIPPETS)
  ];
}

module.exports = {
  ACTIVE_FRONTEND_PREFIXES,
  FORBIDDEN_FRONTEND_RUNTIME_PATHS,
  FORBIDDEN_ACTIVE_ELECTRON_PATTERNS,
  RETIRED_LEGACY_TARGETS,
  createControllerDomOwnershipGuardDefinitions,
  createFrontendBoundaryGuardDefinition,
  createArtifactResolverBoundaryGuardDefinitions,
  createFrontendDependencyOrderRules,
  createArchivedSidecarDomainBoundaryDefinition,
  createBridgeCommandOwnershipDefinition,
  createBridgeEventOwnershipDefinition,
  createToolchainPathOwnershipChecks,
  createRendererBootstrapGuardDefinitions,
  createRendererEntryBoundaryGuardDefinition,
  createRendererWorkspaceBoundaryGuardDefinitions,
  createRendererControllerAssemblyGuardDefinitions,
  createSourceHygieneTargets,
  createSourceHygieneGeneratedOutputConfig,
  createCompatibilitySyntaxTargets,
  createLegacyImportSourceRoots,
  createLegacyImportBoundaryDefinition,
  createActiveJSRoots,
  createGoOwnershipHeaderRoot,
  createGoMaintainabilityHeaderConfig,
  createGoMaintainabilityTestGroups,
  createNodeMaintainabilityTestGroups,
  createJSMaintainabilityHeaderConfig,
  createActiveSourceCorruptionScanRoots,
  createActiveElectronBoundaryConfig,
  createPackagingTextGuardChecks,
  createMachineSpecificPathGuardConfig,
  createMarkerCoverageGuardConfig,
  createToolchainEntryGuardChecks,
  createScriptToolchainOwnershipChecks,
  createArchivedSidecarOwnershipChecks,
  createCompatibilityCrawlOwnershipChecks,
  createRetiredElectronShellOwnershipChecks,
  createFocusedMaintainabilityHeaderChecks,
  createLegacyReferenceAllowlist,
  createLegacyReferenceBoundaryConfig,
  createArchivedSidecarForbiddenCallSnippets,
  createRetiredElectronDirectUsageAllowlist,
  createRetiredElectronDirectUsageExcludeFiles,
  createRetiredElectronDirectUsageSearchRoots,
  createRetiredElectronDirectUsagePatterns,
  createLegacyImportAllowlist,
  createLegacyCompatibilityUtilityChecks,
  createLegacyCompatibilityUtilitySearchRoots,
  createArchivedSidecarOwnershipSearchRoots,
  createRetiredElectronShellOwnershipSearchRoots,
  createToolchainPathOwnershipSearchRoots,
  createScriptToolchainOwnershipSearchRoots,
  createLegacyBoundaryMarkers
};

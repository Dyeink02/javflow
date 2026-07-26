// toolchain-owner: active maintainability verification entrypoint; marker=active-toolchain-verify-maintainability
// Main maintainability verification entrypoint.
// This script validates active frontend boundaries, legacy compatibility
// boundaries, encoding guards, and selected Go module tests.
//
// Ownership summary:
// 1) orchestrate cross-language maintainability verification in one entrypoint
// 2) keep boundary/header/encoding guard execution readable and reviewable
// 3) fail fast when active code drifts back toward historical coupling
//
// File map for maintainers:
// 1) shared filesystem/process helper utilities
// 2) grouped guard runners by frontend/compatibility/toolchain concern
// 3) top-level verification sequence in `main()`

const { spawnSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const { FRONTEND_BUNDLE_FILES } = require('./frontend-bundle-files');
const {
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
} = require('./verify-maintainability-config');

const rootDir = path.resolve(__dirname, '..');
const wailsDir = path.join(rootDir, 'wails-shell');
const LEGACY_IMPORT_ALLOWLIST = createLegacyImportAllowlist();
const LEGACY_BOUNDARY_MARKERS = createLegacyBoundaryMarkers(rootDir);

function runStep(label, command, args, options = {}) {
  console.log(`\n[verify] ${label}`);

  const spawnCommand = process.platform === 'win32' ? 'cmd.exe' : command;
  const spawnArgs =
    process.platform === 'win32'
      ? ['/d', '/s', '/c', [command, ...args].map(quoteWindowsArg).join(' ')]
      : args;

  const result = spawnSync(spawnCommand, spawnArgs, {
    cwd: options.cwd || rootDir,
    stdio: 'inherit',
    shell: false
  });

  if (result.status !== 0) {
    const detail = result.error ? `: ${result.error.message}` : '';
    throw new Error(`${label} failed with exit code ${result.status}${detail}`);
  }
}

function quoteWindowsArg(value) {
  const text = String(value);
  if (!/[ \t"&|<>^]/.test(text)) {
    return text;
  }
  return `"${text.replace(/"/g, '\\"')}"`;
}

function assertIgnoredOrLocal(relativePath, reason) {
  const target = path.join(rootDir, relativePath);
  if (fs.existsSync(target)) {
    console.log(`[verify] local-only path present: ${relativePath} (${reason})`);
  }
}

function collectFiles(rootPath, extensions) {
  const results = [];
  const pending = [rootPath];

  while (pending.length > 0) {
    const currentPath = pending.pop();
    const entries = fs.readdirSync(currentPath, { withFileTypes: true });

    for (const entry of entries) {
      const absolutePath = path.join(currentPath, entry.name);
      if (entry.isDirectory()) {
        if (entry.name === '.generated' || entry.name === 'assets') {
          continue;
        }
        pending.push(absolutePath);
        continue;
      }

      if (extensions.has(path.extname(entry.name).toLowerCase())) {
        results.push(absolutePath);
      }
    }
  }

  return results.sort();
}

function readRelativeText(relativePath) {
  return fs.readFileSync(path.join(rootDir, relativePath), 'utf8');
}

function collectSnippetViolations(items, fileKey, snippetKey) {
  const violations = [];

  for (const item of items) {
    const content = readRelativeText(item[fileKey]);
    const snippets = item[snippetKey];
    const matched = snippets.filter((snippet) => content.includes(snippet));
    if (matched.length > 0) {
      violations.push(`${item[fileKey]} -> ${matched.join(', ')}`);
    }
  }

  return violations;
}

function collectMissingSnippetChecks(checks) {
  const violations = [];

  for (const check of checks) {
    const content = fs.readFileSync(check.file, 'utf8');
    const missing = check.snippets.filter((snippet) => !content.includes(snippet));
    if (missing.length > 0) {
      violations.push(`${check.file}: missing snippets [${missing.join(', ')}]`);
    }
  }

  return violations;
}

function collectForbiddenSnippetMatches(searchRoots, allowlist, forbiddenSnippets) {
  const allowlistSet = new Set(allowlist);
  const violations = [];

  for (const rootPath of searchRoots) {
    for (const filePath of collectFiles(rootPath.root, rootPath.extensions)) {
      if (allowlistSet.has(filePath)) {
        continue;
      }

      const content = fs.readFileSync(filePath, 'utf8');
      const matched = forbiddenSnippets.find((snippet) => content.includes(snippet));
      if (matched) {
        violations.push(`${path.relative(rootDir, filePath).replace(/\\/g, '/')} -> ${matched}`);
      }
    }
  }

  return violations;
}

function collectHeaderViolations(roots, extensions, allowNoHeader) {
  const allowNoHeaderSet = new Set(allowNoHeader);
  const missing = [];

  for (const rootPath of roots) {
    for (const filePath of collectFiles(rootPath, extensions)) {
      if (allowNoHeaderSet.has(filePath)) {
        continue;
      }

      const content = fs.readFileSync(filePath, 'utf8');
      const headerWindow = content.split(/\r?\n/).slice(0, 20).join('\n');
      if (!headerWindow.includes('//') && !headerWindow.includes('#')) {
        missing.push(path.relative(rootDir, filePath).replace(/\\/g, '/'));
      }
    }
  }

  return missing;
}

function collectHeaderContractViolations(
  roots,
  extensions,
  allowNoHeader,
  requiredSnippets,
  headerWindowLines = 32
) {
  const allowNoHeaderSet = new Set(allowNoHeader);
  const violations = [];

  for (const rootPath of roots) {
    for (const filePath of collectFiles(rootPath, extensions)) {
      if (allowNoHeaderSet.has(filePath)) {
        continue;
      }

      const content = fs.readFileSync(filePath, 'utf8');
      const headerWindow = content.split(/\r?\n/).slice(0, headerWindowLines).join('\n');
      const missingSnippets = requiredSnippets.filter((snippet) => !headerWindow.includes(snippet));
      if (missingSnippets.length > 0) {
        violations.push(
          `${path.relative(rootDir, filePath).replace(/\\/g, '/')} missing [${missingSnippets.join(', ')}]`
        );
      }
    }
  }

  return violations;
}

function collectOwnershipTargetMatches(searchRoots, target, excludeFiles = []) {
  const excludeSet = new Set(excludeFiles);
  const actualFiles = [];

  for (const source of searchRoots) {
    for (const filePath of collectFiles(source.root, source.extensions)) {
      const relativePath = path.relative(rootDir, filePath).replace(/\\/g, '/');
      if (excludeSet.has(relativePath)) {
        continue;
      }
      const content = fs.readFileSync(filePath, 'utf8');
      if (content.includes(target)) {
        actualFiles.push(relativePath);
      }
    }
  }

  return actualFiles;
}

function collectRequireImporters(searchRoots, target, importPattern = /require\((['"])(.+?)\1\)/g) {
  const actualImporters = new Set();

  for (const rootPath of searchRoots) {
    for (const filePath of collectFiles(rootPath.root, rootPath.extensions)) {
      const relativeFrom = path.relative(rootDir, filePath).replace(/\\/g, '/');
      const content = fs.readFileSync(filePath, 'utf8');
      let match = null;
      while ((match = importPattern.exec(content)) !== null) {
        const resolvedTarget = resolveRequireTarget(filePath, match[2]);
        if (resolvedTarget === target) {
          actualImporters.add(relativeFrom);
        }
      }
    }
  }

  return Array.from(actualImporters).sort();
}

function collectRequireImporterViolations(checks, searchRoots, importPattern) {
  const violations = [];

  for (const check of checks) {
    const sortedActual = collectRequireImporters(searchRoots, check.target, importPattern);
    const sortedAllowed = Array.from(new Set(check.allowedImporters)).sort();
    if (sortedActual.length !== sortedAllowed.length) {
      violations.push(
        `${check.target}: expected [${sortedAllowed.join(', ')}], got [${sortedActual.join(', ')}]`
      );
      continue;
    }
    for (let index = 0; index < sortedAllowed.length; index += 1) {
      if (sortedAllowed[index] !== sortedActual[index]) {
        violations.push(
          `${check.target}: expected [${sortedAllowed.join(', ')}], got [${sortedActual.join(', ')}]`
        );
        break;
      }
    }
  }

  return violations;
}

function runOwnershipGuard({ title, failureLabel, checks, searchRoots }) {
  console.log(`\n[verify] ${title}`);

  const violations = [];

  for (const check of checks) {
    const actualFiles = collectOwnershipTargetMatches(searchRoots, check.target, check.excludeFiles || []);

    try {
      assertExactSortedValues(check.label, check.target, check.allowedFiles, actualFiles);
    } catch (error) {
      violations.push(error.message);
    }
  }

  if (violations.length > 0) {
    throw new Error(`${failureLabel} failed:\n- ${violations.join('\n- ')}`);
  }
}

function assertExactSortedValues(label, key, expectedValues, actualValues) {
  const sortedExpected = Array.from(new Set(expectedValues)).sort();
  const sortedActual = Array.from(new Set(actualValues)).sort();

  if (sortedActual.length !== sortedExpected.length) {
    throw new Error(
      `${label} changed for ${key}: expected [${sortedExpected.join(', ')}], got [${sortedActual.join(', ')}]`
    );
  }

  for (let index = 0; index < sortedExpected.length; index += 1) {
    if (sortedActual[index] !== sortedExpected[index]) {
      throw new Error(
        `${label} changed for ${key}: expected [${sortedExpected.join(', ')}], got [${sortedActual.join(', ')}]`
      );
    }
  }
}

function runFrontendBoundaryCheck() {
  console.log('\n[verify] frontend runtime boundary');

  for (const relativePath of FRONTEND_BUNDLE_FILES) {
    if (!ACTIVE_FRONTEND_PREFIXES.some((prefix) => relativePath.startsWith(prefix))) {
      throw new Error(`frontend bundle entry escaped allowed prefixes: ${relativePath}`);
    }
    if (
      relativePath.includes('mainServices') ||
      relativePath.includes('sidecar') ||
      relativePath.includes('..')
    ) {
      throw new Error(`frontend bundle entry crossed compatibility boundary: ${relativePath}`);
    }
  }

  const definition = createFrontendBoundaryGuardDefinition(rootDir);
  const activeFrontendFiles = definition.activeSourceRoots.flatMap((item) =>
    collectFiles(item.root, item.extensions)
  );

  for (const filePath of activeFrontendFiles) {
    const content = fs.readFileSync(filePath, 'utf8');
    const matchedPath = FORBIDDEN_FRONTEND_RUNTIME_PATHS.find((snippet) => content.includes(snippet));
    if (matchedPath) {
      throw new Error(`active frontend source references legacy runtime path ${matchedPath}: ${filePath}`);
    }
  }
}

function runFrontendDependencyOrderGuard() {
  console.log('\n[verify] frontend dependency order');

  const indexMap = new Map(FRONTEND_BUNDLE_FILES.map((file, index) => [file, index]));
  const orderedDependencies = createFrontendDependencyOrderRules();

  for (const [dependency, consumer] of orderedDependencies) {
    const dependencyIndex = indexMap.get(dependency);
    const consumerIndex = indexMap.get(consumer);
    if (!Number.isInteger(dependencyIndex) || !Number.isInteger(consumerIndex)) {
      throw new Error(`frontend dependency order guard missing bundle member: ${dependency} -> ${consumer}`);
    }
    if (dependencyIndex >= consumerIndex) {
      throw new Error(`frontend dependency order violated: ${dependency} must load before ${consumer}`);
    }
  }
}

function runControllerDomOwnershipGuard() {
  console.log('\n[verify] controller dom ownership');

  const { controllerFiles, forbiddenPatterns } = createControllerDomOwnershipGuardDefinitions();
  const violations = collectSnippetViolations(
    controllerFiles.map((relativePath) => ({
      relativePath,
      forbiddenPatterns
    })),
    'relativePath',
    'forbiddenPatterns'
  );

  if (violations.length > 0) {
    throw new Error(`controller dom ownership guard failed:\n- ${violations.join('\n- ')}`);
  }
}

function runRendererEntryBoundaryGuard() {
  console.log('\n[verify] renderer entry boundary');

  const definition = createRendererEntryBoundaryGuardDefinition();
  const content = readRelativeText(definition.rendererEntryPath);
  const violations = [];

  for (const check of definition.forbiddenChecks) {
    if (content.includes(check.snippet)) {
      violations.push(check.message);
    }
  }

  if (violations.length > 0) {
    throw new Error(`renderer entry boundary failed:\n- ${violations.join('\n- ')}`);
  }
}

function runArtifactResolverBoundaryGuard() {
  console.log('\n[verify] artifact resolver boundary');

  const { guardedFiles, forbiddenSnippets } = createArtifactResolverBoundaryGuardDefinitions();
  const violations = collectSnippetViolations(
    guardedFiles.map((relativePath) => ({ relativePath, forbiddenSnippets })),
    'relativePath',
    'forbiddenSnippets'
  );

  if (violations.length > 0) {
    throw new Error(`artifact resolver boundary failed:\n- ${violations.join('\n- ')}`);
  }
}

function runRendererBootstrapReentryGuard() {
  console.log('\n[verify] renderer bootstrap reentry guard');

  const guardedFiles = createRendererBootstrapGuardDefinitions();
  const violations = [];
  for (const item of guardedFiles) {
    const content = readRelativeText(item.relativePath);
    const missing = item.requiredSnippets.filter((snippet) => !content.includes(snippet));
    if (missing.length > 0) {
      violations.push(`${item.relativePath} -> missing ${missing.join(', ')}`);
    }
  }

  if (violations.length > 0) {
    throw new Error(`renderer bootstrap reentry guard failed:\n- ${violations.join('\n- ')}`);
  }
}

function runRendererWorkspaceBoundaryGuard() {
  console.log('\n[verify] renderer workspace boundary');

  const { guardedFiles, rendererEntryPath, requiredEntrySnippets } =
    createRendererWorkspaceBoundaryGuardDefinitions();
  const violations = [];

  for (const item of guardedFiles) {
    const content = readRelativeText(item.relativePath);
    const matched = item.forbiddenSnippets.filter((snippet) => content.includes(snippet));
    if (matched.length > 0) {
      violations.push(`${item.relativePath} -> ${matched.join(', ')}`);
    }
  }

  const rendererEntry = readRelativeText(rendererEntryPath);
  const missingEntrySnippets = requiredEntrySnippets.filter(
    (snippet) => !rendererEntry.includes(snippet)
  );
  if (missingEntrySnippets.length > 0) {
    violations.push(`${rendererEntryPath} -> missing ${missingEntrySnippets.join(', ')}`);
  }

  if (violations.length > 0) {
    throw new Error(`renderer workspace boundary failed:\n- ${violations.join('\n- ')}`);
  }
}

function runRendererControllerAssemblyGuard() {
  console.log('\n[verify] renderer controller assembly');

  const { allowedAssemblies, createPattern, searchRoots } =
    createRendererControllerAssemblyGuardDefinitions(rootDir);
  const controllerRoots = searchRoots.flatMap((item) => collectFiles(item.root, item.extensions));
  const violations = [];

  for (const filePath of controllerRoots) {
    const relativePath = path.relative(rootDir, filePath).replace(/\\/g, '/');
    const content = fs.readFileSync(filePath, 'utf8');
    const matches = content.match(createPattern) || [];
    if (matches.length === 0) {
      continue;
    }

    const allowed = allowedAssemblies.get(relativePath) || [];
    for (const match of matches) {
      if (!allowed.includes(match)) {
        violations.push(`${relativePath} -> unexpected controller construction ${match}`);
      }
    }
  }

  for (const [relativePath, expectedMatches] of allowedAssemblies.entries()) {
    const content = readRelativeText(relativePath);
    const missing = expectedMatches.filter((snippet) => !content.includes(snippet));
    if (missing.length > 0) {
      violations.push(`${relativePath} -> missing ${missing.join(', ')}`);
    }
  }

  if (violations.length > 0) {
    throw new Error(`renderer controller assembly failed:\n- ${violations.join('\n- ')}`);
  }
}

function runSourceHygieneCheck() {
  console.log('\n[verify] source hygiene');

  // These paths are allowed locally, but should stay outside any clean source
  // handoff or GitHub upload snapshot. The check keeps the boundary visible
  // without deleting developer data.
  for (const target of createSourceHygieneTargets()) {
    assertIgnoredOrLocal(target.relativePath, target.reason);
  }

  // Generated frontend output is intentionally recreated by the build step.
  // It must remain generated, not manually edited as a source of truth.
  const { generatedDesktopRoot } = createSourceHygieneGeneratedOutputConfig(rootDir);
  const generatedDesktop = generatedDesktopRoot;
  if (fs.existsSync(generatedDesktop)) {
    console.log('[verify] generated desktop renderer output exists after build');
  }
}

function runCompatibilitySyntaxCheck() {
  console.log('\n[verify] compatibility syntax');

  const compatibilityFiles = createCompatibilitySyntaxTargets(rootDir);

  for (const filePath of compatibilityFiles) {
    const source = fs.readFileSync(filePath, 'utf8');
    try {
      new Function(source);
    } catch (error) {
      throw new Error(`compatibility syntax check failed for ${filePath}: ${error.message}`);
    }
  }
}

function runActiveJSSyntaxCheck() {
  console.log('\n[verify] active js syntax');

  const jsRoots = createActiveJSRoots(rootDir);

  for (const rootPath of jsRoots) {
    for (const filePath of collectFiles(rootPath, new Set(['.js']))) {
      const result = spawnSync(process.execPath, ['--check', filePath], {
        cwd: rootDir,
        encoding: 'utf8',
        shell: false,
        stdio: 'pipe'
      });

      if (result.status !== 0) {
        const detail = [result.stderr, result.stdout].filter(Boolean).join('\n').trim();
        throw new Error(`active JS syntax failed for ${filePath}: ${detail || `exit code ${result.status}`}`);
      }
    }
  }
}

function runSidecarLoadSmokeCheck() {
  console.log('\n[verify] sidecar load smoke');

  const result = spawnSync(
    process.execPath,
    ['-e', "require('./desktop/sidecar/commandRouter.js');"],
    {
      cwd: rootDir,
      encoding: 'utf8',
      stdio: 'pipe',
      shell: false
    }
  );

  if (result.status !== 0) {
    const detail = [result.stderr, result.stdout]
      .filter(Boolean)
      .join('\n')
      .trim();
    throw new Error(`sidecar load smoke failed: ${detail || `exit code ${result.status}`}`);
  }
}

function resolveRequireTarget(sourceFile, specifier) {
  if (!specifier.startsWith('.')) {
    return '';
  }

  const base = path.resolve(path.dirname(sourceFile), specifier);
  const candidates = [base, `${base}.js`, path.join(base, 'index.js')];
  const resolved = candidates.find((candidate) => fs.existsSync(candidate));
  if (!resolved) {
    return '';
  }

  return path.relative(rootDir, resolved).replace(/\\/g, '/');
}

function runLegacyImportBoundaryCheck() {
  console.log('\n[verify] legacy import boundary');

  const requirePattern = /(?:require\(|import\s+[^'"`\\n]+?from\s+)(['"])(.+?)\1/g;
  const definition = createLegacyImportBoundaryDefinition(rootDir);
  const sourceRoots = definition.sourceRoots;
  const reverseImports = new Map();

  for (const sourceRoot of sourceRoots) {
    for (const filePath of collectFiles(sourceRoot.root, sourceRoot.extensions)) {
      const sourceText = fs.readFileSync(filePath, 'utf8');
      const relativeFrom = path.relative(rootDir, filePath).replace(/\\/g, '/');
      let match = null;

      while ((match = requirePattern.exec(sourceText)) !== null) {
        const target = resolveRequireTarget(filePath, match[2]);
        if (!target) {
          continue;
        }
        if (!definition.guardedTargetPrefixes.some((prefix) => target.startsWith(prefix))) {
          continue;
        }

        if (!reverseImports.has(target)) {
          reverseImports.set(target, new Set());
        }
        reverseImports.get(target).add(relativeFrom);
      }
    }
  }

  for (const [target, allowedImporters] of LEGACY_IMPORT_ALLOWLIST.entries()) {
    assertExactSortedValues(
      'legacy import boundary',
      target,
      allowedImporters,
      Array.from(reverseImports.get(target) || [])
    );
  }

  for (const [target, actualImportersSet] of reverseImports.entries()) {
    if (LEGACY_IMPORT_ALLOWLIST.has(target)) {
      continue;
    }

    const actualImporters = Array.from(actualImportersSet).sort();
    throw new Error(
      `legacy import boundary found unapproved target ${target}: importers [${actualImporters.join(', ')}]`
    );
  }

  for (const target of RETIRED_LEGACY_TARGETS) {
    const actualImporters = Array.from(reverseImports.get(target) || []).sort();
    if (actualImporters.length > 0) {
      throw new Error(
        `retired legacy target became active again ${target}: importers [${actualImporters.join(', ')}]`
      );
    }
  }
}

function runLegacyBoundaryMarkerCheck() {
  console.log('\n[verify] legacy boundary markers');

  for (const marker of LEGACY_BOUNDARY_MARKERS) {
    const content = fs.readFileSync(marker.file, 'utf8');
    if (!content.includes(marker.snippet)) {
      throw new Error(`legacy boundary marker missing in ${marker.file}: ${marker.snippet}`);
    }
  }
}

function runGoOwnershipHeaderCheck() {
  console.log('\n[verify] go ownership headers');

  const internalRoot = createGoOwnershipHeaderRoot(rootDir);
  const missing = collectHeaderViolations(
    [internalRoot],
    new Set(['.go']),
    collectFiles(internalRoot, new Set(['.go'])).filter((filePath) => filePath.endsWith('_test.go'))
  );

  if (missing.length > 0) {
    throw new Error(`Go ownership header missing in: ${missing.join(', ')}`);
  }

  const { roots, allowNoHeader, extensions, requiredSnippets, headerWindowLines } =
    createGoMaintainabilityHeaderConfig(rootDir);
  const goAllowNoHeader = [
    ...allowNoHeader,
    ...roots.flatMap((rootPath) => collectFiles(rootPath, extensions)).filter((filePath) => filePath.endsWith('_test.go'))
  ];
  const contractViolations = collectHeaderContractViolations(
    roots,
    extensions,
    goAllowNoHeader,
    requiredSnippets,
    headerWindowLines
  );
  if (contractViolations.length > 0) {
    throw new Error(`Go/maintainability header contract failed:\n- ${contractViolations.join('\n- ')}`);
  }
}

function runJSMaintainabilityHeaderCheck() {
  console.log('\n[verify] js ownership headers');

  const { roots, allowNoHeader, extensions, requiredSnippets, headerWindowLines } =
    createJSMaintainabilityHeaderConfig(rootDir);
  const missing = collectHeaderViolations(roots, extensions, allowNoHeader);
  const contractViolations = collectHeaderContractViolations(
    roots,
    extensions,
    allowNoHeader,
    requiredSnippets,
    headerWindowLines
  );

  if (missing.length > 0) {
    throw new Error(`JS/maintainability header missing in: ${missing.join(', ')}`);
  }
  if (contractViolations.length > 0) {
    throw new Error(`JS/maintainability header contract failed:\n- ${contractViolations.join('\n- ')}`);
  }
}

function runActiveSourceCorruptionCheck() {
  console.log('\n[verify] active source corruption guard');

  const sourceRoots = createActiveSourceCorruptionScanRoots(rootDir);
  const suspicious = [];

  for (const { root, extensions } of sourceRoots) {
    for (const filePath of collectFiles(root, extensions)) {
      const relativePath = path.relative(rootDir, filePath).replace(/\\/g, '/');
      const lines = fs.readFileSync(filePath, 'utf8').split(/\r?\n/);

      for (let index = 0; index < lines.length; index += 1) {
        const line = lines[index];
        if (line.startsWith('\ufeff')) {
          suspicious.push(`${relativePath}:${index + 1} leading UTF-8 BOM in active source`);
        }
        if (line.includes('\uFFFD')) {
          suspicious.push(`${relativePath}:${index + 1} replacement character U+FFFD suggests encoding damage`);
        }
        if (/^\s*c\s*$/.test(line)) {
          suspicious.push(`${relativePath}:${index + 1} standalone "c" line looks like text corruption`);
        }
        if (/for \(c\s*$/.test(line)) {
          suspicious.push(`${relativePath}:${index + 1} truncated "for (const ...)" pattern looks corrupted`);
        }
      }
    }
  }

  if (suspicious.length > 0) {
    throw new Error(`active source corruption guard failed:\n- ${suspicious.join('\n- ')}`);
  }
}

function runActiveElectronBoundaryCheck() {
  console.log('\n[verify] active electron boundary');

  const { activeRoots, allowlist } = createActiveElectronBoundaryConfig(rootDir);
  const violations = collectForbiddenSnippetMatches(activeRoots, allowlist, FORBIDDEN_ACTIVE_ELECTRON_PATTERNS);

  if (violations.length > 0) {
    throw new Error(`active electron boundary failed:\n- ${violations.join('\n- ')}`);
  }
}

function runRetiredElectronDirectUsageGuard() {
  console.log('\n[verify] retired electron direct usage');

  const allowlist = new Set(
    createRetiredElectronDirectUsageAllowlist(rootDir).map((filePath) =>
      path.relative(rootDir, filePath).replace(/\\/g, '/')
    )
  );
  const excludeFiles = new Set(createRetiredElectronDirectUsageExcludeFiles());
  const violations = [];
  const searchRoots = createRetiredElectronDirectUsageSearchRoots(rootDir);
  const directPatterns = createRetiredElectronDirectUsagePatterns();

  for (const rootPath of searchRoots) {
    for (const filePath of collectFiles(rootPath.root, rootPath.extensions)) {
      const relativePath = path.relative(rootDir, filePath).replace(/\\/g, '/');
      if (excludeFiles.has(relativePath)) {
        continue;
      }
      const content = fs.readFileSync(filePath, 'utf8');
      const matched = directPatterns.find((snippet) => content.includes(snippet));
      if (!matched) {
        continue;
      }
      if (!allowlist.has(relativePath)) {
        violations.push(`${relativePath} -> ${matched}`);
      }
    }
  }

  if (violations.length > 0) {
    throw new Error(`retired electron direct usage guard failed:\n- ${violations.join('\n- ')}`);
  }
}

function runPackagingTextGuard() {
  console.log('\n[verify] packaging text guard');

  const checks = createPackagingTextGuardChecks(rootDir);
  const violations = collectMissingSnippetChecks(checks);
  if (violations.length > 0) {
    throw new Error(`packaging text guard failed:\n- ${violations.join('\n- ')}`);
  }
}

function runMachineSpecificPathGuard() {
  console.log('\n[verify] machine-specific path guard');

  const { forbiddenSnippets, roots, allowlist } = createMachineSpecificPathGuardConfig(rootDir);
  const violations = collectForbiddenSnippetMatches(roots, allowlist, forbiddenSnippets);

  if (violations.length > 0) {
    throw new Error(`machine-specific path guard failed:\n- ${violations.join('\n- ')}`);
  }
}

function runToolchainEntryGuard() {
  console.log('\n[verify] toolchain entry guard');

  const checks = createToolchainEntryGuardChecks(rootDir);
  const violations = collectMissingSnippetChecks(checks);
  if (violations.length > 0) {
    throw new Error(`toolchain entry guard failed:\n- ${violations.join('\n- ')}`);
  }
}

function runMarkerCoverageGuard() {
  console.log('\n[verify] marker coverage guard');

  const violations = [];
  const checks = createMarkerCoverageGuardConfig(rootDir);
  for (const check of checks) {
    for (const rootPath of check.roots) {
      for (const filePath of collectFiles(rootPath, check.extensions)) {
        const relativePath = path.relative(rootDir, filePath).replace(/\\/g, '/');
        if ((check.excludeFiles || []).includes(relativePath)) {
          continue;
        }
        const content = fs.readFileSync(filePath, 'utf8');
        if (!content.includes(check.requiredSnippet)) {
          violations.push(`${check.label} missing ${check.requiredSnippet}: ${relativePath}`);
        }
      }
    }
  }

  if (violations.length > 0) {
    throw new Error(`marker coverage guard failed:\n- ${violations.join('\n- ')}`);
  }
}

function runFocusedMaintainabilityHeaderGuard() {
  console.log('\n[verify] focused maintainability headers');

  const checks = createFocusedMaintainabilityHeaderChecks(rootDir);
  const violations = collectMissingSnippetChecks(checks);
  if (violations.length > 0) {
    throw new Error(`focused maintainability header guard failed:\n- ${violations.join('\n- ')}`);
  }
}

function runLegacyReferenceBoundaryGuard() {
  console.log('\n[verify] legacy reference boundary');

  const { targetSnippets, sourceRoots } = createLegacyReferenceBoundaryConfig(rootDir);
  const expected = createLegacyReferenceAllowlist();
  const actual = new Map();

  for (const { root, extensions } of sourceRoots) {
    for (const filePath of collectFiles(root, extensions)) {
      const relativePath = path.relative(rootDir, filePath).replace(/\\/g, '/');
      const content = fs.readFileSync(filePath, 'utf8');
      const matched = targetSnippets.filter(
        (snippet) => content.includes(snippet) || content.includes(snippet.replace(/\//g, '\\'))
      );
      if (matched.length > 0) {
        actual.set(relativePath, matched.sort());
      }
    }
  }

  for (const [relativePath, expectedTargets] of expected.entries()) {
    assertExactSortedValues(
      'legacy reference boundary',
      relativePath,
      expectedTargets,
      actual.get(relativePath) || []
    );
  }

  for (const [relativePath, actualTargets] of actual.entries()) {
    if (expected.has(relativePath)) {
      continue;
    }
    throw new Error(
      `legacy reference boundary found unapproved file ${relativePath}: targets [${actualTargets.join(', ')}]`
    );
  }
}

function runArchivedSidecarDomainBoundaryGuard() {
  console.log('\n[verify] archived sidecar domain boundary');

  const definition = createArchivedSidecarDomainBoundaryDefinition(rootDir);
  const forbiddenCalls = createArchivedSidecarForbiddenCallSnippets();
  const violations = [];

  for (const rootPath of definition.bridgeRoots) {
    for (const filePath of collectFiles(rootPath, definition.bridgeExtensions)) {
      const relativePath = path.relative(rootDir, filePath).replace(/\\/g, '/');
      const content = fs.readFileSync(filePath, 'utf8');

      if ((definition.excludeRelativeSuffixes || []).some((suffix) => relativePath.endsWith(suffix))) {
        continue;
      }

      const matched = forbiddenCalls.filter((snippet) => content.includes(snippet));
      if (matched.length > 0) {
        violations.push(`${relativePath} -> ${matched.join(', ')}`);
      }
    }
  }

  if (violations.length > 0) {
    throw new Error(`archived sidecar domain boundary failed:\n- ${violations.join('\n- ')}`);
  }
}

function runLegacyCompatibilityUtilityBoundaryGuard() {
  console.log('\n[verify] legacy compatibility utility boundary');

  const checks = createLegacyCompatibilityUtilityChecks();
  const searchRoots = createLegacyCompatibilityUtilitySearchRoots(rootDir);
  const violations = collectRequireImporterViolations(checks, searchRoots);

  if (violations.length > 0) {
    throw new Error(`legacy compatibility utility boundary failed:\n- ${violations.join('\n- ')}`);
  }
}

function runBridgeCommandOwnershipGuard() {
  console.log('\n[verify] bridge command ownership');

  const definition = createBridgeCommandOwnershipDefinition();
  const bridgeFiles = Object.fromEntries(
    Object.entries(definition.ownerFiles).map(([owner, relativePath]) => [owner, readRelativeText(relativePath)])
  );
  const platformBridge = readRelativeText(definition.platformBridgeFile);

  const violations = [];

  for (const [command, owner, snippet] of definition.requiredOwnership) {
    if (!platformBridge.includes(`'${command}'`) && !platformBridge.includes(`"${command}"`)) {
      violations.push(`platformBridge.wails.js missing command ${command}`);
      continue;
    }

    const ownerFile = bridgeFiles[owner];
    if (!ownerFile || !ownerFile.includes(snippet)) {
      violations.push(`${command} not owned by expected bridge handler ${owner}`);
    }
  }

  if (violations.length > 0) {
    throw new Error(`bridge command ownership failed:\n- ${violations.join('\n- ')}`);
  }
}

function runBridgeEventOwnershipGuard() {
  console.log('\n[verify] bridge event ownership');

  const definition = createBridgeEventOwnershipDefinition();
  const platformBridge = readRelativeText(definition.platformBridgeFile);
  const bridgeProtocol = readRelativeText(definition.bridgeProtocolFile);
  const rawEventOwners = readRelativeText(definition.rawEventOwnerFile);
  const violations = [];

  for (const eventKey of definition.requiredProtocolEvents) {
    if (!bridgeProtocol.includes(`${eventKey}:`)) {
      violations.push(`bridgeProtocol missing BRIDGE_EVENTS.${eventKey}`);
    }
  }

  for (const subscriptionKey of definition.requiredBridgeSubscriptions) {
    if (!platformBridge.includes(`${subscriptionKey}:`)) {
      violations.push(`platformBridge.wails.js missing subscription ${subscriptionKey}`);
    }
  }

  for (const snippet of definition.requiredRawOwnershipSnippets) {
    if (!rawEventOwners.includes(snippet)) {
      violations.push(`api_crawl_state_events.go missing required raw ownership snippet: ${snippet}`);
    }
  }

  if (violations.length > 0) {
    throw new Error(`bridge event ownership failed:\n- ${violations.join('\n- ')}`);
  }
}

function runToolchainPathOwnershipGuard() {
  console.log('\n[verify] toolchain path ownership');

  const checks = createToolchainPathOwnershipChecks();
  const searchRoots = createToolchainPathOwnershipSearchRoots(rootDir);
  const violations = collectRequireImporterViolations(checks, searchRoots);

  if (violations.length > 0) {
    throw new Error(`toolchain path ownership failed:\n- ${violations.join('\n- ')}`);
  }
}

function runScriptToolchainOwnershipGuard() {
  runOwnershipGuard({
    title: 'script toolchain ownership',
    failureLabel: 'script toolchain ownership',
    checks: createScriptToolchainOwnershipChecks(),
    searchRoots: createScriptToolchainOwnershipSearchRoots(rootDir)
  });
}

function runArchivedSidecarOwnershipGuard() {
  runOwnershipGuard({
    title: 'archived sidecar ownership',
    failureLabel: 'archived sidecar ownership',
    checks: createArchivedSidecarOwnershipChecks(),
    searchRoots: createArchivedSidecarOwnershipSearchRoots(rootDir)
  });
}

function runCompatibilityCrawlOwnershipGuard() {
  runOwnershipGuard({
    title: 'compatibility crawl ownership',
    failureLabel: 'compatibility crawl ownership',
    checks: createCompatibilityCrawlOwnershipChecks(),
    searchRoots: createArchivedSidecarOwnershipSearchRoots(rootDir)
  });
}

function runRetiredElectronShellOwnershipGuard() {
  runOwnershipGuard({
    title: 'retired electron shell ownership',
    failureLabel: 'retired electron shell ownership',
    checks: createRetiredElectronShellOwnershipChecks(),
    searchRoots: createRetiredElectronShellOwnershipSearchRoots(rootDir)
  });
}

function runArchivedSidecarCompatibilityTests() {
  runNodeMaintainabilityTestGroup('archived sidecar compatibility tests');
}

function runSharedCommonContractTests() {
  runNodeMaintainabilityTestGroup('shared common contract tests');
}

function runCompatibilityBoundaryTests() {
  runNodeMaintainabilityTestGroup('compatibility boundary tests');
}

function runGoDomainContractTests() {
  runGoMaintainabilityTestGroup('Go domain contract tests');
}

function runGoReadModelContractTests() {
  runGoMaintainabilityTestGroup('Go read-model contract tests');
}

function runGoDependencyBoundaryTests() {
  runGoMaintainabilityTestGroup('Go dependency boundary tests');
}

function runGoCoreModuleTests() {
  runGoMaintainabilityTestGroup('Go core module tests');
}

function runGoMaintainabilityTestGroup(label) {
  const groups = createGoMaintainabilityTestGroups().map((group) => ({
    ...group,
    runner: 'go',
    args: ['test', ...group.packages],
    cwd: wailsDir
  }));
  runMaintainabilityTestGroup(label, groups, 'Go');
}

function runNodeMaintainabilityTestGroup(label) {
  const groups = createNodeMaintainabilityTestGroups().map((group) => ({
    ...group,
    cwd: rootDir
  }));
  runMaintainabilityTestGroup(label, groups, 'Node');
}

function runMaintainabilityTestGroup(label, groups, familyName) {
  const target = groups.find((group) => group.label === label);
  if (!target) {
    throw new Error(`Unknown ${familyName} maintainability test group: ${label}`);
  }

  if (Array.isArray(target.rationale) && target.rationale.length > 0) {
    console.log(`\n[verify] ${label}`);
    for (const item of target.rationale) {
      console.log(`  - ${item}`);
    }
  }

  runStep(label, target.runner || 'npx', target.args || [], { cwd: target.cwd || rootDir });
}

function main() {
  runStep('desktop frontend build', 'npm', ['run', 'build:desktop-frontend']);
  runStep('encoding check', 'npm', ['run', 'check:encoding']);
  runStep('frontend text check', 'node', ['scripts/verify-frontend-text.js']);
  runFrontendBoundaryCheck();
  runFrontendDependencyOrderGuard();
  runControllerDomOwnershipGuard();
  runRendererEntryBoundaryGuard();
  runArtifactResolverBoundaryGuard();
  runRendererBootstrapReentryGuard();
  runRendererWorkspaceBoundaryGuard();
  runRendererControllerAssemblyGuard();
  runActiveJSSyntaxCheck();
  runCompatibilitySyntaxCheck();
  runSidecarLoadSmokeCheck();
  runLegacyImportBoundaryCheck();
  runLegacyBoundaryMarkerCheck();
  runGoOwnershipHeaderCheck();
  runJSMaintainabilityHeaderCheck();
  runActiveSourceCorruptionCheck();
  runActiveElectronBoundaryCheck();
  runRetiredElectronDirectUsageGuard();
  runPackagingTextGuard();
  runMachineSpecificPathGuard();
  runToolchainEntryGuard();
  runMarkerCoverageGuard();
  runFocusedMaintainabilityHeaderGuard();
  runLegacyReferenceBoundaryGuard();
  runArchivedSidecarDomainBoundaryGuard();
  runLegacyCompatibilityUtilityBoundaryGuard();
  runBridgeCommandOwnershipGuard();
  runBridgeEventOwnershipGuard();
  runToolchainPathOwnershipGuard();
  runScriptToolchainOwnershipGuard();
  runCompatibilityCrawlOwnershipGuard();
  runArchivedSidecarOwnershipGuard();
  runRetiredElectronShellOwnershipGuard();
  runArchivedSidecarCompatibilityTests();
  runSharedCommonContractTests();
  runCompatibilityBoundaryTests();
  runGoDomainContractTests();
  runGoReadModelContractTests();
  runGoDependencyBoundaryTests();
  runGoCoreModuleTests();
  runSourceHygieneCheck();

  console.log('\n[verify] maintainability checks passed');
}

try {
  main();
} catch (error) {
  console.error(`\n[verify] ${error.message}`);
  process.exit(1);
}

// Runtime path shim for non-Wails compatibility contexts.
// compatibility-owner: active crawl-compatible runtime path shim; marker=compat-sidecar-runtime-paths
// The active Wails app resolves these directories on the Go side; this helper
// keeps older Node-side services runnable without depending on Electron APIs.
//
// Maintenance rule:
// - Go/Wails runtime resolution is the source of truth for current production
// - this file exists only so archived Node-side helpers/tests can still boot
// - do not add new product path policy here unless the Go side also needs it
// - if a bug reproduces in normal Wails startup, debug `wails-shell/app.go`
//   and bridge/runtime cache code before touching this shim
//
// Ownership summary:
// 1) normalize sidecar compatibility runtime paths
// 2) provide deterministic fallback directories for archived dev/test entrypoints
// 3) keep path shim behavior out of current Go/Wails runtime code
//
// File map for maintainers:
// 1) default context builders
// 2) context normalization + lazy accessors
// 3) minimal Electron-like app shim
const os = require('os');
const path = require('path');

let runtimeContext = null;

function getDefaultDocumentsDir() {
  return path.join(os.homedir(), 'Documents');
}

function toAbsolutePath(value, fallbackValue) {
  const rawValue = String(value || '').trim();
  const nextValue = rawValue || String(fallbackValue || '').trim();
  if (!nextValue) {
    return '';
  }

  return path.resolve(nextValue);
}

// When old sidecar entrypoints run outside the Wails bootstrap, keep their
// default paths repo-local so they remain deterministic in dev/test contexts.
function buildDefaultContext() {
  const repoRoot = path.resolve(__dirname, '..', '..');
  return {
    repoRoot,
    appPath: repoRoot,
    resourcesPath: repoRoot,
    userData: path.join(repoRoot, '.wails-dev', 'userData'),
    documents: getDefaultDocumentsDir(),
    temp: os.tmpdir()
  };
}

// Normalize the externally supplied compatibility context once so the archived
// services do not each implement their own path fallback rules.
function initializeRuntimeContext(rawContext = {}) {
  const defaults = buildDefaultContext();

  runtimeContext = {
    repoRoot: toAbsolutePath(rawContext.repoRoot, defaults.repoRoot),
    appPath: toAbsolutePath(rawContext.appPath, defaults.appPath),
    resourcesPath: toAbsolutePath(rawContext.resourcesPath, defaults.resourcesPath),
    userData: toAbsolutePath(rawContext.userData, defaults.userData),
    documents: toAbsolutePath(rawContext.documents, defaults.documents),
    temp: toAbsolutePath(rawContext.temp, defaults.temp)
  };

  return runtimeContext;
}

function getRuntimeContext() {
  // Keep lazy initialization local so archived helpers/tests can call into the
  // sidecar utilities without having to know whether bootstrap already ran.
  // If a compatibility bug looks like "wrong repo/userData/resources path",
  // inspect this shim before touching current Go runtime path ownership.
  if (!runtimeContext) {
    return initializeRuntimeContext({});
  }
  return runtimeContext;
}

function getPathByName(name) {
  // Path-name mapping stays intentionally tiny and compatibility-oriented. New
  // runtime path policy should be added on the Go/Wails side first.
  const context = getRuntimeContext();

  if (name === 'userData') {
    return context.userData;
  }
  if (name === 'documents') {
    return context.documents;
  }
  if (name === 'temp') {
    return context.temp;
  }

  return context.documents;
}

// Minimal Electron-like app shim for older helpers that still call `app.getPath`
// or `app.getAppPath`. Current Wails production code should not depend on this.
function createAppShim() {
  // Keep the shim intentionally tiny. The longer this object grows, the easier
  // it becomes for compatibility-only APIs to leak back into active product
  // code and blur the Wails-vs-Electron runtime boundary again.
  return {
    getPath: (name) => getPathByName(name),
    getAppPath: () => getRuntimeContext().appPath,
    quit: () => {}
  };
}

module.exports = {
  buildDefaultContext,
  initializeRuntimeContext,
  getRuntimeContext,
  getPathByName,
  createAppShim
};

// toolchain-owner: shared frontend build path registry; marker=active-toolchain-frontend-paths
// Shared path registry for the current desktop frontend build/sync pipeline.
// Build helpers should import these paths instead of re-deriving renderer or
// Wails frontend locations ad hoc.
//
// Ownership summary:
// 1) define active desktop renderer source/generated paths
// 2) define the Wails frontend sync target paths
// 3) keep path derivation out of individual frontend build scripts
//
// Boundary rule:
// runtime code should not depend on this file. It belongs to the frontend
// build/sync toolchain only.
//
// File map for maintainers:
// 1) repo/desktop/renderer root derivation
// 2) generated desktop artifact paths
// 3) Wails frontend sync target paths

const path = require('path');

const repoRoot = path.resolve(__dirname, '..');
const desktopDir = path.join(repoRoot, 'desktop');
const rendererDir = path.join(desktopDir, 'renderer');
const rendererAssetsDir = path.join(rendererDir, 'assets');
const rendererGeneratedDir = path.join(rendererDir, '.generated');
const rendererPartialsDir = path.join(rendererDir, 'partials');
const rendererStylesDir = path.join(rendererDir, 'styles');

const generatedIndexHtmlPath = path.join(rendererGeneratedDir, 'index.html');
const generatedStylesCssPath = path.join(rendererGeneratedDir, 'styles.css');
const generatedBundleJsPath = path.join(rendererGeneratedDir, 'bundle.js');

const wailsFrontendRendererDir = path.join(
  repoRoot,
  'wails-shell',
  'frontend',
  'desktop',
  'renderer'
);

module.exports = {
  repoRoot,
  desktopDir,
  rendererDir,
  rendererAssetsDir,
  rendererGeneratedDir,
  rendererPartialsDir,
  rendererStylesDir,
  generatedIndexHtmlPath,
  generatedStylesCssPath,
  generatedBundleJsPath,
  wailsFrontendRendererDir
};

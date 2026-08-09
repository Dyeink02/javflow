// toolchain-owner: active desktop stylesheet bundler; marker=active-toolchain-bundle-css
// Generated desktop CSS bundler for the current Wails frontend build chain.
// If packaged styles are stale or missing, debug this file before touching
// compatibility runtimes or archived Electron assets.
//
// Ownership summary:
// 1) define the generated stylesheet assembly order for the active desktop UI
// 2) concatenate maintained renderer style sources into one runtime artifact
// 3) keep style bundling concerns out of runtime/frontend sync code
//
// Boundary rule:
// build-time only; runtime code should not import this file.
//
// File map for maintainers:
// 1) generated output path helpers
// 2) deterministic stylesheet ordering list
// 3) concat/write pipeline for generated desktop CSS

const fs = require('fs');
const path = require('path');
const {
  rendererGeneratedDir,
  rendererStylesDir,
  generatedStylesCssPath
} = require('./frontend-paths');

const cssOrder = [
  'tokens.css',
  'base.css',
  'animations.css',
  'workspace.css',
  'layout.css',
  'hero.css',
  'forms.css',
  'panels.css',
  'log.css',
  'actressatlas.css',
  'actressatlas-loading.css',
  'ranking.css',
  'subscription.css',
  'librarymetadata.css',
  'responsive.css'
];

function ensureDirectory(targetPath) {
  fs.mkdirSync(targetPath, { recursive: true });
}

function readSourceWithoutBom(filePath) {
  const content = fs.readFileSync(filePath, 'utf-8');
  return content.charCodeAt(0) === 0xfeff ? content.slice(1) : content;
}

function writeUtf8WithBom(filePath, content) {
  const utf8Bom = Buffer.from([0xef, 0xbb, 0xbf]);
  fs.writeFileSync(filePath, Buffer.concat([utf8Bom, Buffer.from(content, 'utf8')]));
}

let bundled = '/* GENERATED CSS - DO NOT EDIT. Edit desktop/renderer/styles/*.css */\n\n';

for (const file of cssOrder) {
  const filePath = path.join(rendererStylesDir, file);
  if (fs.existsSync(filePath)) {
    bundled += `/* ==== ${file} ==== */\n`;
    bundled += readSourceWithoutBom(filePath);
    bundled += '\n\n';
  } else {
    console.warn(`WARNING: ${filePath} not found`);
  }
}

ensureDirectory(rendererGeneratedDir);
writeUtf8WithBom(generatedStylesCssPath, bundled);
console.log(`Bundled ${cssOrder.length} CSS files into: ${generatedStylesCssPath}`);
console.log(`Total size: ${(Buffer.byteLength(bundled) / 1024).toFixed(1)} KB`);

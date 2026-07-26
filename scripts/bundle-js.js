// toolchain-owner: active desktop JS bundler; marker=active-toolchain-bundle-js
// Generated desktop JS bundler for the current Wails frontend build chain.
// It packages the active common/renderer sources into the runtime bundle that
// the generated desktop UI syncs into Wails.
//
// Ownership summary:
// 1) assemble the active desktop common/renderer bundle in a fixed order
// 2) keep runtime bundle membership sourced from one reviewed manifest
// 3) emit the generated JS artifact consumed by Wails sync/build steps
//
// Boundary rule:
// build-time only; runtime code should not import this file.
//
// File map for maintainers:
// 1) generated output path helpers
// 2) source read/sanitization helpers
// 3) bundle assembly from `FRONTEND_BUNDLE_FILES`

const fs = require('fs');
const path = require('path');
const {
  desktopDir,
  rendererGeneratedDir,
  generatedBundleJsPath
} = require('./frontend-paths');
const { FRONTEND_BUNDLE_FILES } = require('./frontend-bundle-files');

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

let bundled = '/* GENERATED JS - DO NOT EDIT. Edit desktop/common or desktop/renderer sources. */\n';
bundled += '(function(){\n';
bundled += '"use strict";\n\n';

for (const file of FRONTEND_BUNDLE_FILES) {
  const filePath = path.join(desktopDir, file);
  if (fs.existsSync(filePath)) {
    bundled += `/* ==== ${file} ==== */\n`;
    bundled += readSourceWithoutBom(filePath);
    bundled += '\n';
  } else {
    console.warn(`WARN: ${filePath} not found, skipping`);
  }
}

bundled += '\n})();\n';

ensureDirectory(rendererGeneratedDir);
// Generated frontend artifacts are embedded into the Wails WebView runtime.
// Write them as UTF-8 with BOM so local-resource loading on Windows does not
// rely on charset sniffing for Chinese text.
writeUtf8WithBom(generatedBundleJsPath, bundled);
console.log(`Bundled ${FRONTEND_BUNDLE_FILES.length} JS files into: ${generatedBundleJsPath}`);
console.log(`Size: ${(Buffer.byteLength(bundled) / 1024).toFixed(1)} KB`);

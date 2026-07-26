#!/usr/bin/env node

// toolchain-owner: active Wails desktop build entry; marker=active-toolchain-run-wails-build
// Primary desktop build entry for the current product.
// This is the script maintainers should start from when the packaged Wails EXE
// is wrong or missing.
//
// Workflow map:
// 1) build desktop frontend artifacts
// 2) sync artifacts into the Wails frontend tree
// 3) run `wails build`
// 4) copy resulting EXE into `wails-shell/release`
//
// Ownership summary:
// 1) orchestrate the maintained Wails desktop build path end to end
// 2) keep frontend bundling/sync/build steps in one predictable entrypoint
// 3) expose the release EXE path used by later packaging helpers
//
// File map for maintainers:
// 1) toolchain/path imports from `wails-paths`
// 2) sequential build orchestration in `main()`
// 3) release EXE sync/reporting

const path = require('path');
const {
  repoRoot,
  wailsDir,
  resolveNpmBinary,
  resolveWailsBinary,
  runCommand,
  copyBuildExeToRelease
} = require('./wails-paths');

function assertAssetDir() {
  const fs = require('fs');
  const path = require('path');
  const wailsJsonPath = path.join(wailsDir, 'wails.json');
  const config = JSON.parse(fs.readFileSync(wailsJsonPath, 'utf8'));
  const expected = 'frontend/desktop/renderer';
  if (config.assetdir !== expected) {
    throw new Error(
      `wails.json assetdir 必须是 "${expected}" 才能正确加载前端入口 index.html，` +
      `当前为 "${config.assetdir}"。请修改 ${wailsJsonPath} 后重新构建。`
    );
  }
}

function main() {
  assertAssetDir();

  const npmBinary = resolveNpmBinary();
  const wailsBinary = resolveWailsBinary();

  // Compile TypeScript sidecar sources into dist/ so the Node sidecar runtime
  // can require parser.js / requestHandler.js when Cloudflare compat mode is used.
  runCommand(npmBinary, ['run', 'build'], { cwd: repoRoot });
  runCommand(npmBinary, ['run', 'build:desktop-frontend'], { cwd: repoRoot });
  runCommand(npmBinary, ['run', 'sync:wails-frontend'], { cwd: repoRoot });
  runCommand(wailsBinary, ['build'], { cwd: wailsDir });

  const releaseResult = copyBuildExeToRelease();

  // Patch Windows version info and icon onto the release EXE.
  // Wails v2 does not consistently embed the metadata declared in wails.json
  // on Windows, so we apply it as a post-build step.
  runCommand('node', [path.join(repoRoot, 'scripts', 'patch-exe-metadata.mjs')], { cwd: repoRoot });

  if (releaseResult.fallbackUsed) {
    console.log(`Wails build completed. Primary release EXE is locked: ${releaseResult.primaryPath}`);
    console.log(`Fallback release EXE: ${releaseResult.actualPath}`);
    return;
  }

  console.log(`Wails build completed: ${releaseResult.actualPath}`);
}

main();

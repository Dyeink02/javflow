#!/usr/bin/env node

// Primary desktop build entry for the current product. Frontend assets are
// assembled first, then Wails produces one self-contained executable. Actor
// Atlas has no optional helper process; profile/media use the existing bridge.
//
// Ownership summary:
// 1) assemble desktop frontend assets before native compilation
// 2) build the Wails executable and copy its portable release artifact
// 3) apply the final executable metadata patch
//
// File map for maintainers:
// 1) Wails asset directory assertion
// 2) primary frontend/native build orchestration
// 3) release executable handoff
// marker=active-toolchain-run-wails-build
const path = require('path');
const fs = require('fs');
const {
  repoRoot,
  wailsDir,
  resolveNpmBinary,
  resolveWailsBinary,
  runCommand,
  copyBuildExeToRelease
} = require('./wails-paths');

function assertAssetDir() {
  const config = JSON.parse(fs.readFileSync(path.join(wailsDir, 'wails.json'), 'utf8'));
  const expected = 'frontend/desktop/renderer';
  if (config.assetdir !== expected) {
    throw new Error(`wails.json assetdir must be "${expected}", got "${config.assetdir}"`);
  }
}

function readProductVersion() {
  const packagePath = path.join(repoRoot, 'package.json');
  const packageInfo = JSON.parse(fs.readFileSync(packagePath, 'utf8'));
  const version = String(process.env.JAVFLOW_BUILD_VERSION || packageInfo.version || '').trim();
  if (!/^\d+\.\d+\.\d+$/.test(version)) {
    throw new Error(`JAVFLOW_BUILD_VERSION/package.json version is invalid: ${version || '(empty)'}`);
  }
  return version;
}

function main() {
  assertAssetDir();
  const productVersion = readProductVersion();
  const npmBinary = resolveNpmBinary();
  const wailsBinary = resolveWailsBinary();
  runCommand(npmBinary, ['run', 'build'], { cwd: repoRoot });
  runCommand(npmBinary, ['run', 'build:desktop-frontend'], { cwd: repoRoot });
  runCommand(npmBinary, ['run', 'sync:wails-frontend'], { cwd: repoRoot });
  runCommand(wailsBinary, [
    'build',
    '-ldflags',
    // -s -w 去除符号表与 DWARF 调试信息（panic 堆栈仍可读， pclntab 不受影响），
    // 单此一项可缩小主程序约 25-30%。
    `-X javflow/internal/appupdate.BuildVersion=${productVersion} -s -w`
  ], { cwd: wailsDir });
  const releaseResult = copyBuildExeToRelease();
  runCommand('node', [path.join(repoRoot, 'scripts', 'patch-exe-metadata.mjs')], { cwd: repoRoot });
  console.log(`Wails build completed: ${releaseResult.actualPath}`);
}

main();

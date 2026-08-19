#!/usr/bin/env node

// target-state-owner: future pure-Go package shape after sidecar retirement; marker=target-toolchain-wails-go-lite
// Pure-Go packaging target once sidecar compatibility is fully retired.
// This script assembles only the Wails EXE, frontend assets, and optional
// FFmpeg payload. It should not be treated as the active production path
// while Cloudflare / age-check compatibility still depends on sidecar pieces.
//
// Maintenance boundary:
// - this is the target release shape for future cleaner maintenance
// - current production may still need the dual-package path while sidecar
//   compatibility remains
// - if packaging bugs reproduce only here, debug Wails frontend/runtime assets
//   first, not archived Electron scripts
//
// Ownership summary:
// 1) assemble the future pure-Go/Wails package shape without sidecar payloads
// 2) keep target-state packaging logic separate from current compatibility path
// 3) document the intended cleaner release contract for later migration work
//
// File map for maintainers:
// 1) packaging constants and output path derivation
// 2) EXE/assets/runtime copy helpers
// 3) installer/portable package assembly entrypoints

const fs = require('fs');
const os = require('os');
const path = require('path');
const { spawnSync } = require('child_process');
const { resolveNSISBinary } = require('./nsis-paths.js');

const REPO_ROOT = process.cwd();
const WAILS_RELEASE_DIR = path.join(REPO_ROOT, 'wails-shell', 'release');
const SOURCE_EXE_PATH = path.join(WAILS_RELEASE_DIR, 'javflow.exe');
const OUTPUT_DIR = path.join(WAILS_RELEASE_DIR, 'packages');
const TEMP_ROOT = path.join(os.tmpdir(), 'jav-auto-wails-go-lite');
const BUNDLED_FFMPEG_PATH = path.join(
  REPO_ROOT,
  'desktop',
  'resources',
  'ffmpeg',
  'win-x64',
  'ffmpeg.exe'
);
const PRODUCT_NAME = 'JavFlow';
const SHORTCUT_NAME = 'JavFlow';
const EXECUTABLE_NAME = 'javflow.exe';
const LITE_DIR_NAME = 'DesktopRuntime';

function readPackageJson() {
  return JSON.parse(fs.readFileSync(path.join(REPO_ROOT, 'package.json'), 'utf8'));
}

function ensureDirectory(targetPath) {
  fs.mkdirSync(targetPath, { recursive: true });
}

function ensureCleanDirectory(targetPath) {
  fs.rmSync(targetPath, { recursive: true, force: true });
  fs.mkdirSync(targetPath, { recursive: true });
}

function fileExists(targetPath) {
  try {
    return fs.statSync(targetPath).isFile();
  } catch {
    return false;
  }
}

function copyFile(sourcePath, targetPath) {
  ensureDirectory(path.dirname(targetPath));
  fs.copyFileSync(sourcePath, targetPath);
}

function copyDirectory(sourceDir, targetDir) {
  ensureDirectory(targetDir);
  const entries = fs.readdirSync(sourceDir, { withFileTypes: true });
  for (const entry of entries) {
    const srcPath = path.join(sourceDir, entry.name);
    const destPath = path.join(targetDir, entry.name);
    if (entry.isDirectory()) {
      copyDirectory(srcPath, destPath);
    } else {
      fs.copyFileSync(srcPath, destPath);
    }
  }
}

function escapeNSIS(value) {
  return String(value).replace(/\\/g, '\\\\').replace(/"/g, '\\"');
}

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    stdio: 'pipe',
    encoding: 'utf8',
    shell: false,
    ...options
  });
  if (result.error) {
    throw result.error;
  }
  return result;
}

function formatSize(filePath) {
  const stats = fs.statSync(filePath);
  return `${(stats.size / (1024 * 1024)).toFixed(2)} MB`;
}

function main() {
  const pkg = readPackageJson();
  const version = (pkg.version || '0.4.32').trim();

  if (!fileExists(SOURCE_EXE_PATH)) {
    throw new Error(`Missing Wails EXE at ${SOURCE_EXE_PATH}\nRun npm run phase1:build:exe first.`);
  }

  ensureCleanDirectory(OUTPUT_DIR);

  // Build the go-lite runtime stage: frontend + EXE + FFmpeg only.
  const liteStagePath = path.join(TEMP_ROOT, 'go-lite-stage');
  ensureCleanDirectory(liteStagePath);

  copyFile(SOURCE_EXE_PATH, path.join(liteStagePath, EXECUTABLE_NAME));

  const frontendDir = path.join(REPO_ROOT, 'wails-shell', 'frontend', 'desktop');
  if (fs.existsSync(frontendDir)) {
    const rendererDir = path.join(frontendDir, 'renderer');
    const commonDir = path.join(frontendDir, 'common');
    if (fs.existsSync(rendererDir)) {
      copyDirectory(rendererDir, path.join(liteStagePath, LITE_DIR_NAME, 'renderer'));
    }
    if (fs.existsSync(commonDir)) {
      copyDirectory(commonDir, path.join(liteStagePath, LITE_DIR_NAME, 'common'));
    }
  }

  if (fileExists(BUNDLED_FFMPEG_PATH)) {
    copyFile(BUNDLED_FFMPEG_PATH, path.join(liteStagePath, 'tools', 'ffmpeg', 'ffmpeg.exe'));
  }

  const iconPath = path.join(REPO_ROOT, 'build', 'icon.ico');
  const installerExeName = `JavFlow-GoLite-${version}.exe`;
  const installerOutputPath = path.join(OUTPUT_DIR, installerExeName);
  const uninstallLinkName = `Uninstall ${PRODUCT_NAME} Lite.lnk`;

  const installerScript = [
    'Unicode true',
    '!include "MUI2.nsh"',
    `!define MUI_ICON "${escapeNSIS(iconPath)}"`,
    `!define MUI_UNICON "${escapeNSIS(iconPath)}"`,
    'RequestExecutionLevel user',
    'SetCompressor /SOLID lzma',
    '!insertmacro MUI_PAGE_DIRECTORY',
    '!insertmacro MUI_PAGE_INSTFILES',
    '!insertmacro MUI_UNPAGE_CONFIRM',
    '!insertmacro MUI_UNPAGE_INSTFILES',
    '!insertmacro MUI_LANGUAGE "SimpChinese"',
    `OutFile "${escapeNSIS(installerOutputPath)}"`,
    `InstallDir "$LOCALAPPDATA\\\\${escapeNSIS(`${PRODUCT_NAME} Lite`)}"`,
    `Name "${escapeNSIS(`${PRODUCT_NAME} Lite (Go-native)`)}"`,
    'ShowInstDetails show',
    'ShowUninstDetails show',
    '',
    'Section "Install"',
    '  SetShellVarContext current',
    '  SetOutPath "$INSTDIR"',
    `  File /r "${escapeNSIS(path.join(liteStagePath, '*'))}"`,
    '  WriteUninstaller "$INSTDIR\\\\Uninstall.exe"',
    `  CreateDirectory "$SMPROGRAMS\\\\${escapeNSIS(SHORTCUT_NAME)}"`,
    `  CreateShortCut "$DESKTOP\\\\${escapeNSIS(PRODUCT_NAME)} Lite.lnk" "$INSTDIR\\\\${escapeNSIS(EXECUTABLE_NAME)}"`,
    `  CreateShortCut "$SMPROGRAMS\\\\${escapeNSIS(SHORTCUT_NAME)}\\\\${escapeNSIS(PRODUCT_NAME)} Lite.lnk" "$INSTDIR\\\\${escapeNSIS(EXECUTABLE_NAME)}"`,
    `  CreateShortCut "$SMPROGRAMS\\\\${escapeNSIS(SHORTCUT_NAME)}\\\\${escapeNSIS(uninstallLinkName)}" "$INSTDIR\\\\Uninstall.exe"`,
    'SectionEnd',
    '',
    'Section "Uninstall"',
    '  SetShellVarContext current',
    `  Delete "$DESKTOP\\\\${escapeNSIS(PRODUCT_NAME)} Lite.lnk"`,
    `  Delete "$SMPROGRAMS\\\\${escapeNSIS(SHORTCUT_NAME)}\\\\${escapeNSIS(PRODUCT_NAME)} Lite.lnk"`,
    `  Delete "$SMPROGRAMS\\\\${escapeNSIS(SHORTCUT_NAME)}\\\\${escapeNSIS(uninstallLinkName)}"`,
    `  RMDir "$SMPROGRAMS\\\\${escapeNSIS(SHORTCUT_NAME)}"`,
    '  RMDir /r "$INSTDIR"',
    'SectionEnd',
    ''
  ].join('\r\n');

  const nsisWorkDir = path.join(TEMP_ROOT, 'go-lite-nsis');
  ensureCleanDirectory(nsisWorkDir);
  const scriptPath = path.join(nsisWorkDir, 'go-lite.nsi');
  fs.writeFileSync(scriptPath, `\uFEFF${installerScript}`, 'utf8');

  run(resolveNSISBinary(), [scriptPath], { cwd: nsisWorkDir });

  if (!fileExists(installerOutputPath)) {
    throw new Error(`NSIS build finished but installer output is missing: ${installerOutputPath}`);
  }

  console.log('Go Lite package build completed.');
  console.log(`Installer: ${installerOutputPath} (${formatSize(installerOutputPath)})`);
  console.log('Profile: no Node.js / sidecar / dist / TypeScript, only Go EXE + frontend + FFmpeg');
  console.log('Use case: local machine without Node/Puppeteer after Cloudflare bypass is fully Go-native.');
}

main();

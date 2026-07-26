// packaging-owner: shared NSIS binary resolver; marker=active-toolchain-nsis-paths
// Shared NSIS compiler discovery helper for maintained Windows packaging
// scripts. Keep path probing here so packaging tools do not each carry their
// own hard-coded makensis lookup logic.
//
// Ownership summary:
// 1) centralize NSIS compiler discovery for maintained packaging scripts
// 2) keep machine-specific makensis probing out of individual packagers
// 3) make installer toolchain failures easier to isolate
//
// Boundary rule:
// packaging-only helper; runtime code should not import this file.
//
// File map for maintainers:
// 1) candidate makensis path list (built dynamically)
// 2) file existence helpers
// 3) NSIS resolution/export contract

const fs = require('fs');
const os = require('os');
const path = require('path');
const { spawnSync } = require('child_process');

function buildElectronBuilderCandidate() {
  const localAppData = process.env.LOCALAPPDATA;
  if (localAppData) {
    return path.join(
      localAppData,
      'electron-builder',
      'Cache',
      'nsis',
      'nsis-3.0.4.1',
      'Bin',
      'makensis.exe'
    );
  }
  // Fallback: construct from homedir when LOCALAPPDATA is not set
  return path.join(os.homedir(), 'AppData', 'Local', 'electron-builder', 'Cache', 'nsis', 'nsis-3.0.4.1', 'Bin', 'makensis.exe');
}

const NSIS_BIN_CANDIDATES = [
  'C:\\Program Files (x86)\\NSIS\\makensis.exe',
  'C:\\Program Files\\NSIS\\makensis.exe',
  buildElectronBuilderCandidate()
];

function fileExists(targetPath) {
  try {
    return fs.statSync(targetPath).isFile();
  } catch {
    return false;
  }
}

function resolveNSISBinary() {
  for (const candidate of NSIS_BIN_CANDIDATES) {
    if (fileExists(candidate)) {
      return candidate;
    }
  }

  if (process.platform === 'win32') {
    try {
      const whereResult = spawnSync('where.exe', ['makensis.exe'], {
        stdio: 'pipe',
        encoding: 'utf8',
        shell: false
      });
      if (!whereResult.error && whereResult.status === 0) {
        const resolved = String(whereResult.stdout || '')
          .split(/\r?\n/)
          .map((item) => item.trim())
          .find((item) => fileExists(item));
        if (resolved) {
          return resolved;
        }
      }
    } catch {
      // Fall through to explicit error.
    }
  }

  throw new Error('NSIS compiler not found: makensis.exe');
}

module.exports = {
  NSIS_BIN_CANDIDATES,
  resolveNSISBinary
};

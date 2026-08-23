// toolchain-owner: shared Wails build/release path resolver; marker=active-toolchain-wails-paths
// Shared Wails build/release path and binary resolution helpers.
// Current packaging scripts should converge here instead of each script
// implementing its own binary lookup or release sync logic.
//
// Ownership summary:
// 1) resolve current Wails build/release filesystem paths
// 2) resolve Node / npm / Wails executables for packaging scripts
// 3) centralize EXE copy-to-release behavior
//
// Boundary rule:
// product runtime code must not import this file. It exists only for the build
// and packaging toolchain.
//
// File map for maintainers:
// 1) repo/Wails directory and executable path derivation
// 2) toolchain resolution helpers for Node/npm/Wails
// 3) release EXE lookup and copy-to-release helpers

const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');

const repoRoot = path.resolve(__dirname, '..');
const wailsDir = path.join(repoRoot, 'wails-shell');
const buildBinDir = path.join(wailsDir, 'build', 'bin');
const buildExePath = path.join(buildBinDir, 'javflow.exe');
const releaseDir = path.join(wailsDir, 'release');
const releaseExePath = path.join(releaseDir, 'javflow.exe');

function fileExists(targetPath) {
  try {
    return fs.statSync(targetPath).isFile();
  } catch {
    return false;
  }
}

function ensureDirectory(targetPath) {
  fs.mkdirSync(targetPath, { recursive: true });
}

function resolveExecutable(defaultName, extraCandidates = []) {
  const candidates = extraCandidates.filter(Boolean);

  for (const candidate of candidates) {
    if (fileExists(candidate)) {
      return candidate;
    }
  }

  if (process.platform === 'win32') {
    try {
      const whereResult = spawnSync('where.exe', [defaultName], {
        cwd: repoRoot,
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
      // Fall through to PATH lookup by command name.
    }
  }

  return defaultName;
}

function resolveNpmBinary() {
  return resolveExecutable(process.platform === 'win32' ? 'npm.cmd' : 'npm', [
    path.join(process.env.ProgramFiles || 'C:\\Program Files', 'nodejs', 'npm.cmd')
  ]);
}

function resolveNodeBinary() {
  return resolveExecutable(process.platform === 'win32' ? 'node.exe' : 'node');
}

function resolveNodeExecutablePath() {
  const candidates = [
    path.join(process.env.ProgramFiles || 'C:\\Program Files', 'nodejs', 'node.exe')
  ];

  for (const candidate of candidates) {
    if (fileExists(candidate)) {
      return candidate;
    }
  }

  if (process.platform === 'win32') {
    try {
      const whereResult = spawnSync('where.exe', ['node'], {
        cwd: repoRoot,
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
      // Fall through to explicit error below.
    }
  }

  throw new Error('未找到 node.exe，无法生成 Wails 第一阶段运行时包。');
}

function resolveWailsBinary() {
  return resolveExecutable(process.platform === 'win32' ? 'wails.exe' : 'wails', [
    path.join(process.env.USERPROFILE || '', 'go', 'bin', 'wails.exe')
  ]);
}

function runCommand(command, args, options = {}) {
  const isWindowsCmd = process.platform === 'win32' && /\.(cmd|bat)$/i.test(String(command || ''));
  const spawnOptions = {
    stdio: 'inherit',
    shell: false,
    ...options
  };
  const result = isWindowsCmd
    ? spawnSync(`"${command}" ${args.join(' ')}`, {
        ...spawnOptions,
        shell: true
      })
    : spawnSync(command, args, spawnOptions);

  if (result.error) {
    throw result.error;
  }
  if (result.status !== 0) {
    throw new Error(`${command} ${args.join(' ')} failed with exit code ${result.status}`);
  }
}

function resolveReleaseExePath() {
  const configuredPath = String(process.env.JAVFLOW_RELEASE_EXE_PATH || '').trim();
  if (!configuredPath) {
    return releaseExePath;
  }
  return path.isAbsolute(configuredPath)
    ? path.normalize(configuredPath)
    : path.resolve(repoRoot, configuredPath);
}

function copyBuildExeToRelease() {
  if (!fileExists(buildExePath)) {
    throw new Error(`Wails build artifact not found: ${buildExePath}`);
  }

  const targetPath = resolveReleaseExePath();
  ensureDirectory(path.dirname(targetPath));
  try {
    fs.copyFileSync(buildExePath, targetPath);
    const buildSize = fs.statSync(buildExePath).size;
    const copiedSize = fs.statSync(targetPath).size;
    if (buildSize !== copiedSize) {
      throw new Error(`Copied Wails EXE size mismatch: ${buildSize} != ${copiedSize}`);
    }
    return {
      primaryPath: targetPath,
      actualPath: targetPath,
      fallbackUsed: false
    };
  } catch (error) {
    if (!error || error.code !== 'EBUSY') {
      throw error;
    }

    const timestamp = new Date()
      .toISOString()
      .replace(/[-:]/g, '')
      .replace(/\..+$/, '')
      .replace('T', '-');
    const fallbackExePath = path.join(path.dirname(targetPath), `javflow-${timestamp}.exe`);
    fs.copyFileSync(buildExePath, fallbackExePath);

    const buildSize = fs.statSync(buildExePath).size;
    const copiedSize = fs.statSync(fallbackExePath).size;
    if (buildSize !== copiedSize) {
      throw new Error(`Copied fallback Wails EXE size mismatch: ${buildSize} != ${copiedSize}`);
    }

    return {
      primaryPath: targetPath,
      actualPath: fallbackExePath,
      fallbackUsed: true
    };
  }
}

function resolveJavFlowExe() {
  if (fileExists(buildExePath)) {
    return buildExePath;
  }
  if (fileExists(releaseExePath)) {
    return releaseExePath;
  }
  throw new Error(`JavFlow EXE not found. Checked: ${buildExePath} and ${releaseExePath}`);
}

module.exports = {
  repoRoot,
  wailsDir,
  buildExePath,
  releaseDir,
  releaseExePath,
  fileExists,
  ensureDirectory,
  resolveNodeBinary,
  resolveNodeExecutablePath,
  resolveNpmBinary,
  resolveWailsBinary,
  runCommand,
  copyBuildExeToRelease,
  resolveJavFlowExe,
  resolveReleaseExePath
};

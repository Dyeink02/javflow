#!/usr/bin/env node

// packaging-owner: maintained portable-lite wrapper packager; marker=active-toolchain-build-lite-exe
// Portable-lite wrapper packager around the current release executable.
// This helper produces a direct-launch portable package and should stay
// separate from the main Wails build entry.
//
// Ownership summary:
// 1) wrap the already-built release EXE into a portable-lite delivery folder
// 2) keep post-build packaging separate from the primary Wails compile step
// 3) emit a simpler distribution target without changing runtime binaries
//
// Boundary rule:
// packaging-only helper; use after the main Wails EXE already exists.
//
// File map for maintainers:
// 1) package metadata/read helpers
// 2) output directory cleanup/copy helpers
// 3) portable package assembly in `main()`

const fs = require('fs');
const os = require('os');
const path = require('path');
const { spawnSync } = require('child_process');

function readPackageJson() {
  return JSON.parse(fs.readFileSync(path.join(process.cwd(), 'package.json'), 'utf8'));
}

function ensureCleanDirectory(targetPath) {
  fs.rmSync(targetPath, { recursive: true, force: true });
  fs.mkdirSync(targetPath, { recursive: true });
}

function copyDirectory(sourcePath, targetPath) {
  fs.cpSync(sourcePath, targetPath, {
    recursive: true,
    force: true
  });
}

function collectFiles(rootPath, relativePath = '') {
  const currentPath = path.join(rootPath, relativePath);
  const entries = fs.readdirSync(currentPath, { withFileTypes: true });
  const files = [];

  for (const entry of entries) {
    const entryRelativePath = path.join(relativePath, entry.name);
    if (entry.isDirectory()) {
      files.push(...collectFiles(rootPath, entryRelativePath));
      continue;
    }

    files.push(entryRelativePath);
  }

  return files;
}

function toSedPath(value) {
  return path.resolve(value).replace(/\\/g, '\\\\');
}

function buildSedFileContent(stagePath, outputPath, launchFileName) {
  const groupedFiles = new Map();

  for (const relativeFilePath of collectFiles(stagePath)) {
    const normalizedRelativePath = relativeFilePath.split(path.sep).join('\\');
    const relativeDir = path.dirname(normalizedRelativePath) === '.' ? '' : path.dirname(normalizedRelativePath);
    const fileName = path.basename(normalizedRelativePath);

    if (!groupedFiles.has(relativeDir)) {
      groupedFiles.set(relativeDir, []);
    }

    groupedFiles.get(relativeDir).push(fileName);
  }

  const sections = Array.from(groupedFiles.entries()).sort((left, right) => left[0].localeCompare(right[0]));
  const sourceFileHeader = ['[SourceFiles]'];
  const sourceFileSections = [];

  sections.forEach(([relativeDir, fileNames], index) => {
    const sectionName = `SourceFiles${index}`;
    const sourceDir = relativeDir ? path.join(stagePath, relativeDir) : stagePath;

    sourceFileHeader.push(`${sectionName}=${toSedPath(sourceDir)}`);
    sourceFileSections.push(`[${sectionName}]`);

    fileNames.sort().forEach((fileName) => {
      sourceFileSections.push(`${fileName}=`);
    });

    sourceFileSections.push('');
  });

  return [
    '[Version]',
    'Class=IEXPRESS',
    'SEDVersion=3',
    '[Options]',
    'PackagePurpose=InstallApp',
    'ShowInstallProgramWindow=0',
    'HideExtractAnimation=1',
    'UseLongFileName=1',
    'InsideCompressed=1',
    'CAB_FixedSize=0',
    'CAB_ResvCodeSigning=0',
    'RebootMode=N',
    'InstallPrompt=',
    'DisplayLicense=',
    'FinishMessage=',
    `TargetName=${toSedPath(outputPath)}`,
    'FriendlyName=JAV Auto Crawler Tool Lite Direct',
    `AppLaunched=${launchFileName}`,
    'PostInstallCmd=<None>',
    `AdminQuietInstCmd=${launchFileName}`,
    `UserQuietInstCmd=${launchFileName}`,
    'SourceFiles=SourceFiles',
    '',
    ...sourceFileHeader,
    '',
    ...sourceFileSections
  ].join('\r\n');
}

function writeLauncher(stagePath, executableName) {
  const launcherPath = path.join(stagePath, 'launch-lite.cmd');
  const lines = [
    '@echo off',
    'setlocal',
    'cd /d "%~dp0"',
    'set "launch_args="',
    'if exist "%TEMP%\\jav-lite-direct-launch.args" set /p launch_args=<"%TEMP%\\jav-lite-direct-launch.args"',
    `"${executableName}" %launch_args%`,
    'exit /b 0'
  ];

  fs.writeFileSync(launcherPath, lines.join('\r\n'), 'ascii');
  return path.basename(launcherPath);
}

function main() {
  const runtimePackage = readPackageJson();
  const version = runtimePackage.version;
  const sourceExecutableName = `JAV Auto Crawler Tool ${version}.exe`;
  const sourceExecutablePath = path.join(process.cwd(), 'release', sourceExecutableName);
  const outputDirectory = path.join(process.cwd(), 'release', 'lite-direct');
  const tempRoot = path.join(os.tmpdir(), 'jav-lite-direct-build');
  const stagePath = path.join(tempRoot, 'stage');
  const sedPath = path.join(tempRoot, 'lite-direct.sed');
  const tempOutputPath = path.join(tempRoot, `JAV Auto Crawler Tool Lite Direct ${version}.exe`);
  const outputPath = path.join(outputDirectory, `JAV Auto Crawler Tool Lite Direct ${version}.exe`);
  const executableName = 'JAV Auto Crawler Tool Portable.exe';

  if (!fs.existsSync(sourceExecutablePath)) {
    throw new Error(`Missing packaged portable executable at ${sourceExecutablePath}`);
  }

  ensureCleanDirectory(outputDirectory);
  ensureCleanDirectory(tempRoot);
  fs.mkdirSync(stagePath, { recursive: true });
  fs.copyFileSync(sourceExecutablePath, path.join(stagePath, executableName));

  const launchFileName = writeLauncher(stagePath, executableName);
  fs.writeFileSync(sedPath, buildSedFileContent(stagePath, tempOutputPath, launchFileName), 'ascii');

  const buildResult = spawnSync('iexpress.exe', ['/N', sedPath], {
    stdio: 'pipe',
    encoding: 'utf8',
    shell: false
  });

  if (buildResult.status !== 0 || !fs.existsSync(tempOutputPath)) {
    const output = [buildResult.stdout, buildResult.stderr].filter(Boolean).join('\n').trim();
    throw new Error(
      `IExpress packaging failed with exit code ${buildResult.status}.${output ? ` Output: ${output}` : ''}`
    );
  }

  fs.copyFileSync(tempOutputPath, outputPath);

  const outputStats = fs.statSync(outputPath);
  const sizeMb = (outputStats.size / (1024 * 1024)).toFixed(2);
  console.log(`Lite direct EXE created: ${outputPath} (${sizeMb} MB)`);
}

main();

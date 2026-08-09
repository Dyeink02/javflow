#!/usr/bin/env node

// compatibility-owner: current Wails package assembler while Node crawl-compat payload still ships; marker=compat-toolchain-wails-dual-packages
// Wails dual-package builder for the current desktop release family.
// This is a maintained Wails-era packaging helper, not an Electron builder
// script. It still assembles a Node-compatible runtime stage because the
// Cloudflare / age-check compatibility lane has not been fully removed yet.
//
// Maintenance boundary:
// - current EXE build entry is `scripts/run-wails-build.js`
// - this script packages that EXE into distributable variants
// - do not treat it as proof that `desktop/mainServices` / `desktop/sidecar`
//   are part of the primary frontend runtime; they are runtime payload only
// - keep these references visible until the Electron-era packaging helpers are
//   fully retired from the source tree
//
// Ownership summary:
// 1) assemble current distributable packages around the Wails-built EXE
// 2) preserve sidecar-compatible payload packaging while Cloudflare lane remains
// 3) keep installer/portable packaging logic out of runtime code
//
// File map for maintainers:
// 1) packaging constants and asset/source path derivation
// 2) copy/stage helpers for EXE, sidecar payload, and assets
// 3) installer/portable package assembly entrypoints

const fs = require('fs');
const os = require('os');
const path = require('path');
const { spawnSync } = require('child_process');
const { resolveNSISBinary } = require('./nsis-paths.js');
const { resolveNodeExecutablePath, resolveNpmBinary } = require('./wails-paths.js');

const REPO_ROOT = process.cwd();
const WAILS_RELEASE_DIR = path.join(REPO_ROOT, 'wails-shell', 'release');
const SOURCE_EXE_PATH = path.join(WAILS_RELEASE_DIR, 'javflow.exe');
const PACKAGE_STAGE_ROOT = path.join(WAILS_RELEASE_DIR, 'package-stage');
const BASE_STAGE_PATH = path.join(PACKAGE_STAGE_ROOT, 'base-runtime');
const LITE_STAGE_PATH = path.join(PACKAGE_STAGE_ROOT, 'lite-runtime');
const INSTALLER_STAGE_PATH = path.join(PACKAGE_STAGE_ROOT, 'installer-runtime');
const OUTPUT_DIR = path.join(WAILS_RELEASE_DIR, 'packages');
const TEMP_ROOT = path.join(os.tmpdir(), 'javflow-wails-dual-packages');
const BUNDLED_FFMPEG_PATH = path.join(REPO_ROOT, 'desktop', 'resources', 'ffmpeg', 'win-x64', 'ffmpeg.exe');
const PRODUCT_NAME = 'JavFlow';
const SHORTCUT_NAME = 'JavFlow';
const EXECUTABLE_NAME = 'javflow.exe';
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

  if (result.status !== 0) {
    const output = [result.stdout, result.stderr].filter(Boolean).join('\n').trim();
    throw new Error(`${command} ${args.join(' ')} failed with exit code ${result.status}.${output ? `\n${output}` : ''}`);
  }

  return result.stdout || '';
}

function copyDirectory(sourcePath, targetPath) {
  ensureDirectory(targetPath);
  const result = spawnSync(
    'robocopy',
    [
      sourcePath,
      targetPath,
      '/E',
      '/NFL',
      '/NDL',
      '/NJH',
      '/NJS',
      '/NC',
      '/NS',
      '/NP'
    ],
    {
      stdio: 'pipe',
      encoding: 'utf8',
      shell: false
    }
  );

  if (result.status > 7) {
    const output = [result.stdout, result.stderr].filter(Boolean).join('\n').trim();
    throw new Error(`robocopy ${sourcePath} -> ${targetPath} failed with exit code ${result.status}.${output ? `\n${output}` : ''}`);
  }
}

function copyRuntimeDirectory(sourcePath, targetPath) {
  copyDirectory(sourcePath, targetPath);
  fs.rmSync(path.join(targetPath, 'resources', 'ffmpeg'), { recursive: true, force: true });
}

function runNpmCI(npmBinary, cwd) {
  const result = spawnSync(`"${npmBinary}" ci --omit=dev --ignore-scripts`, {
    cwd,
    stdio: 'pipe',
    encoding: 'utf8',
    shell: true
  });

  if (result.error) {
    throw result.error;
  }

  if (result.status !== 0) {
    const output = [result.stdout, result.stderr].filter(Boolean).join('\n').trim();
    throw new Error(`npm ci --omit=dev --ignore-scripts failed with exit code ${result.status}.${output ? `\n${output}` : ''}`);
  }
}

function escapeNSIS(value) {
  return String(value || '').replace(/\\/g, '\\\\').replace(/"/g, '$\\"');
}

function collectStageFiles(rootPath, currentPath = rootPath) {
  const files = [];
  const entries = fs.readdirSync(currentPath, { withFileTypes: true })
    .sort((left, right) => left.name.localeCompare(right.name, 'zh-CN'));
  for (const entry of entries) {
    const absolutePath = path.join(currentPath, entry.name);
    if (entry.isDirectory()) {
      files.push(...collectStageFiles(rootPath, absolutePath));
      continue;
    }
    files.push({ absolutePath, relativePath: path.relative(rootPath, absolutePath) });
  }
  return files;
}

function buildTransparentInstallCommands(stagePath) {
  const commands = ['  SetDetailsPrint listonly'];
  let activeDirectory = null;
  for (const file of collectStageFiles(stagePath)) {
    const relativeDirectory = path.dirname(file.relativePath);
    if (relativeDirectory !== activeDirectory) {
      activeDirectory = relativeDirectory;
      const outputDirectory = relativeDirectory === '.'
        ? '$INSTDIR'
        : `$INSTDIR\\${relativeDirectory}`;
      commands.push(`  SetOutPath "${escapeNSIS(outputDirectory)}"`);
    }
    commands.push(`  DetailPrint "正在解压：${escapeNSIS(file.relativePath)}"`);
    commands.push(`  File "/oname=${escapeNSIS(path.basename(file.relativePath))}" "${escapeNSIS(file.absolutePath)}"`);
  }
  commands.push('  SetDetailsPrint both');
  return commands;
}

function prepareBaseStage() {
  if (!fileExists(SOURCE_EXE_PATH)) {
    throw new Error(`未找到 Wails 可执行文件：${SOURCE_EXE_PATH}`);
  }

  const nodeBinary = resolveNodeExecutablePath();
  const npmBinary = resolveNpmBinary(nodeBinary);

  ensureCleanDirectory(BASE_STAGE_PATH);
  copyFile(SOURCE_EXE_PATH, path.join(BASE_STAGE_PATH, EXECUTABLE_NAME));
  copyRuntimeDirectory(path.join(REPO_ROOT, 'desktop'), path.join(BASE_STAGE_PATH, 'desktop'));
  copyDirectory(path.join(REPO_ROOT, 'dist'), path.join(BASE_STAGE_PATH, 'dist'));
  copyFile(path.join(REPO_ROOT, 'package.json'), path.join(BASE_STAGE_PATH, 'package.json'));
  copyFile(path.join(REPO_ROOT, 'package-lock.json'), path.join(BASE_STAGE_PATH, 'package-lock.json'));
  copyFile(nodeBinary, path.join(BASE_STAGE_PATH, 'runtime', 'node', 'node.exe'));

  runNpmCI(npmBinary, BASE_STAGE_PATH);
}

function prepareDerivedStages() {
  ensureCleanDirectory(LITE_STAGE_PATH);
  ensureCleanDirectory(INSTALLER_STAGE_PATH);

  copyDirectory(BASE_STAGE_PATH, LITE_STAGE_PATH);
  copyDirectory(BASE_STAGE_PATH, INSTALLER_STAGE_PATH);

  const bundledFFmpegIncluded = fileExists(BUNDLED_FFMPEG_PATH);
  if (bundledFFmpegIncluded) {
    copyFile(BUNDLED_FFMPEG_PATH, path.join(INSTALLER_STAGE_PATH, 'tools', 'ffmpeg', 'ffmpeg.exe'));
  } else {
    console.log(`未找到内置 FFmpeg：${BUNDLED_FFMPEG_PATH}`);
  }

  // Install transparency artifact: write a human-readable manifest so users can
  // verify what is installed from the wizard's details pane and post-install files.
  const manifestPath = path.join(INSTALLER_STAGE_PATH, '安装内容清单.txt');
  const manifestLines = [
    'JavFlow - 安装内容清单',
    '================================',
    '',
    '说明：该清单用于安装过程公开透明展示，列出安装包内主要内容。',
    '',
    '核心程序：',
    '- javflow.exe（演员资料与作品分页功能）',
    '- Uninstall.exe（安装后生成）',
    '',
    '运行时与依赖：',
    '- runtime\\node\\node.exe',
    '- package.json',
    '- package-lock.json',
    '',
    '业务与兼容资源：',
    '- desktop\\*',
    '- dist\\*',
    bundledFFmpegIncluded
      ? '- tools\\ffmpeg\\ffmpeg.exe'
      : '- FFmpeg 未内置；需要时由软件的依赖管理功能安装。',
    '',
    '注：首次安装后可在安装目录查看本清单文件。'
  ];
  fs.writeFileSync(manifestPath, `\uFEFF${manifestLines.join('\r\n')}\r\n`, 'utf8');
}

function buildInstallerScript(stagePath, outputPath) {
  const iconPath = path.join(REPO_ROOT, 'build', 'icon.ico');
  const transparentInstallCommands = buildTransparentInstallCommands(stagePath);
  return [
    'Unicode true',
    '!include "MUI2.nsh"',
    '!define MUI_ABORTWARNING',
    `!define MUI_ICON "${escapeNSIS(iconPath)}"`,
    `!define MUI_UNICON "${escapeNSIS(iconPath)}"`,
    '!define MUI_WELCOMEPAGE_TITLE "欢迎使用 JavFlow 安装向导"',
    '!define MUI_WELCOMEPAGE_TEXT "本向导将引导你完成安装。\\r\\n\\r\\n安装过程中会显示详细安装内容与步骤，确保公开透明。"',
    '!define MUI_DIRECTORYPAGE_TEXT_TOP "请选择安装路径。你可以按需更改安装目录。"',
    '!define MUI_INSTFILESPAGE_FINISHHEADER_TEXT "安装完成"',
    '!define MUI_INSTFILESPAGE_FINISHHEADER_SUBTEXT "JavFlow 已安装完成。"',
    '!define MUI_UNCONFIRMPAGE_TEXT_TOP "确认卸载 JavFlow。"',
    'RequestExecutionLevel user',
    'SetCompressor /SOLID lzma',
    '!insertmacro MUI_PAGE_WELCOME',
    '!insertmacro MUI_PAGE_DIRECTORY',
    '!insertmacro MUI_PAGE_INSTFILES',
    '!insertmacro MUI_PAGE_FINISH',
    '!insertmacro MUI_UNPAGE_CONFIRM',
    '!insertmacro MUI_UNPAGE_INSTFILES',
    '!insertmacro MUI_LANGUAGE "SimpChinese"',
    `OutFile "${escapeNSIS(outputPath)}"`,
    `InstallDir "$LOCALAPPDATA\\\\${escapeNSIS(PRODUCT_NAME)}"`,
    `Name "${escapeNSIS(PRODUCT_NAME)}"`,
    'ShowInstDetails show',
    'ShowUninstDetails show',
    '',
    'Section "安装程序" SEC_INSTALL',
    '  SetShellVarContext current',
    '  DetailPrint "【步骤 1/7】准备安装目录..."',
    '  SetOutPath "$INSTDIR"',
    '  DetailPrint "【步骤 2/7】正在复制核心程序与运行依赖..."',
    ...transparentInstallCommands,
    '  DetailPrint "【步骤 3/7】写入卸载程序..."',
    '  WriteUninstaller "$INSTDIR\\\\Uninstall.exe"',
    '  DetailPrint "【步骤 4/7】创建开始菜单目录..."',
    `  CreateDirectory "$SMPROGRAMS\\\\${escapeNSIS(SHORTCUT_NAME)}"`,
    '  DetailPrint "【步骤 5/7】创建桌面快捷方式..."',
    `  CreateShortCut "$DESKTOP\\\\${escapeNSIS(PRODUCT_NAME)}.lnk" "$INSTDIR\\\\${escapeNSIS(EXECUTABLE_NAME)}"`,
    '  DetailPrint "【步骤 6/7】创建开始菜单快捷方式..."',
    `  CreateShortCut "$SMPROGRAMS\\\\${escapeNSIS(SHORTCUT_NAME)}\\\\${escapeNSIS(PRODUCT_NAME)}.lnk" "$INSTDIR\\\\${escapeNSIS(EXECUTABLE_NAME)}"`,
    `  CreateShortCut "$SMPROGRAMS\\\\${escapeNSIS(SHORTCUT_NAME)}\\\\卸载 ${escapeNSIS(PRODUCT_NAME)}.lnk" "$INSTDIR\\\\Uninstall.exe"`,
    '  DetailPrint "【步骤 7/7】安装完成。已写入安装内容清单：$INSTDIR\\\\安装内容清单.txt"',
    '  DetailPrint "正在登记 Windows 卸载信息..."',
    `  WriteRegStr HKCU "Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\${escapeNSIS(PRODUCT_NAME)}" "DisplayName" "${escapeNSIS(PRODUCT_NAME)}"`,
    `  WriteRegStr HKCU "Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\${escapeNSIS(PRODUCT_NAME)}" "DisplayVersion" "${escapeNSIS(readPackageJson().version)}"`,
    `  WriteRegStr HKCU "Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\${escapeNSIS(PRODUCT_NAME)}" "Publisher" "raawaa"`,
    `  WriteRegStr HKCU "Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\${escapeNSIS(PRODUCT_NAME)}" "DisplayIcon" "$INSTDIR\\${escapeNSIS(EXECUTABLE_NAME)}"`,
    `  WriteRegStr HKCU "Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\${escapeNSIS(PRODUCT_NAME)}" "UninstallString" "$\\\"$INSTDIR\\Uninstall.exe$\\\""`,
    `  WriteRegDWORD HKCU "Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\${escapeNSIS(PRODUCT_NAME)}" "NoModify" 1`,
    `  WriteRegDWORD HKCU "Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\${escapeNSIS(PRODUCT_NAME)}" "NoRepair" 1`,
    'SectionEnd',
    '',
    // NSIS reserves the exact section name "Uninstall" for uninstaller code.
    // Translating it makes these delete commands run during normal install.
    'Section "Uninstall" SEC_UNINSTALL',
    '  SetShellVarContext current',
    '  DetailPrint "正在删除快捷方式..."',
    `  Delete "$DESKTOP\\\\${escapeNSIS(PRODUCT_NAME)}.lnk"`,
    `  Delete "$SMPROGRAMS\\\\${escapeNSIS(SHORTCUT_NAME)}\\\\${escapeNSIS(PRODUCT_NAME)}.lnk"`,
    `  Delete "$SMPROGRAMS\\\\${escapeNSIS(SHORTCUT_NAME)}\\\\卸载 ${escapeNSIS(PRODUCT_NAME)}.lnk"`,
    `  RMDir "$SMPROGRAMS\\\\${escapeNSIS(SHORTCUT_NAME)}"`,
    '  DetailPrint "正在删除安装登记..."',
    `  DeleteRegKey HKCU "Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\${escapeNSIS(PRODUCT_NAME)}"`,
    '  DetailPrint "正在删除安装目录..."',
    '  RMDir /r "$INSTDIR"',
    '  DetailPrint "卸载完成。"',
    'SectionEnd',
    ''
  ].join('\r\n');
}

function buildLiteDirectScript(stagePath, outputPath, version) {
  const liteDirName = `JavFlow-Lite-${String(version || '').trim() || 'dev'}`;
  return [
    'Unicode true',
    'RequestExecutionLevel user',
    'SetCompressor /SOLID lzma',
    'SilentInstall silent',
    'AutoCloseWindow true',
    `OutFile "${escapeNSIS(outputPath)}"`,
    `InstallDir "$TEMP\\\\${escapeNSIS(liteDirName)}"`,
    `Name "${escapeNSIS(`${PRODUCT_NAME} 直开版`)}"`,
    '',
    'Section "Launch"',
    '  RMDir /r "$INSTDIR"',
    '  SetOutPath "$INSTDIR"',
    `  File /r "${escapeNSIS(path.join(stagePath, '*'))}"`,
    `  Exec '"$INSTDIR\\\\${escapeNSIS(EXECUTABLE_NAME)}"'`,
    'SectionEnd',
    ''
  ].join('\r\n');
}

function buildNSISPackage(scriptContent, outputPath, tempFolderName) {
  const tempWorkDir = path.join(TEMP_ROOT, tempFolderName);
  ensureCleanDirectory(tempWorkDir);

  const scriptPath = path.join(tempWorkDir, 'package.nsi');
  fs.writeFileSync(scriptPath, `\uFEFF${scriptContent}`, 'utf8');
  run(resolveNSISBinary(), [scriptPath], { cwd: tempWorkDir });

  if (!fileExists(outputPath)) {
    throw new Error(`NSIS 构建已结束，但未生成目标文件：${outputPath}`);
  }
}

function formatSize(filePath) {
  const stats = fs.statSync(filePath);
  return `${(stats.size / (1024 * 1024)).toFixed(2)} MB`;
}

function main() {
  const pkg = readPackageJson();
  const version = String(pkg.version || '0.0.0').trim();
  const liteOutputPath = path.join(OUTPUT_DIR, `JavFlow-Lite-Direct-${version}.exe`);
  const installerOutputPath = path.join(OUTPUT_DIR, `JavFlow-Installer-${version}.exe`);

  ensureCleanDirectory(OUTPUT_DIR);
  ensureCleanDirectory(TEMP_ROOT);

  prepareBaseStage();
  prepareDerivedStages();

  buildNSISPackage(
    buildLiteDirectScript(LITE_STAGE_PATH, liteOutputPath, version),
    liteOutputPath,
    'nsis-lite-direct'
  );
  buildNSISPackage(
    buildInstallerScript(INSTALLER_STAGE_PATH, installerOutputPath),
    installerOutputPath,
    'nsis-installer'
  );

  console.log('Wails 双分发打包完成：');
  console.log(`直开版：${liteOutputPath} (${formatSize(liteOutputPath)})`);
  console.log(`安装包：${installerOutputPath} (${formatSize(installerOutputPath)})`);
  console.log(`直开版运行时目录：${LITE_STAGE_PATH}`);
  console.log(`安装版运行时目录：${INSTALLER_STAGE_PATH}`);
}

try {
  main();
} catch (error) {
  const message = error instanceof Error ? error.stack || error.message : String(error);
  console.error(message);
  process.exit(1);
}

// Legacy Electron demo packager kept for archive/demo distribution work.
// deprecated: archived packaging helper only; marker=archived-electron-demo-packager
// The active desktop release chain is Wails-based and should use
// `scripts/run-wails-build.js` plus Wails packaging helpers instead.
//
// Maintenance boundary:
// - archive/demo only
// - not part of the current product release path
// - if a packaging bug reproduces in the Wails build, do not start here
//
// Ownership summary:
// 1) package archived demo variants for historical distribution work
// 2) keep demo-only package mutations outside the active Wails release path
// 3) make its archive-only status explicit to maintainers
//
// File map for maintainers:
// 1) package.json variant definitions
// 2) temporary package mutation/write helpers
// 3) archived electron-builder invocation and restore flow
const fs = require('fs');
const path = require('path');
const { execFileSync } = require('child_process');

const rootDir = path.resolve(__dirname, '..');
const packagePath = path.join(rootDir, 'package.json');
const originalPackageText = fs.readFileSync(packagePath, 'utf8');
const originalPackageJson = JSON.parse(originalPackageText);

const variants = [
  {
    key: 'base',
    demoMode: 'base',
    demoLabel: 'Demo Base',
    productName: 'JAV Auto Crawler Tool Demo Base',
    executableName: 'JAV Auto Crawler Tool Demo Base'
  },
  {
    key: 'ae',
    demoMode: 'ae',
    demoLabel: 'Demo AE',
    productName: 'JAV Auto Crawler Tool Demo AE',
    executableName: 'JAV Auto Crawler Tool Demo AE'
  },
  {
    key: 'aed',
    demoMode: 'aed',
    demoLabel: 'Demo AED',
    productName: 'JAV Auto Crawler Tool Demo AED',
    executableName: 'JAV Auto Crawler Tool Demo AED'
  }
];

const consolidatedOutputDir = path.join(rootDir, 'release', 'demo-variants');

function ensureDirectory(targetPath) {
  fs.mkdirSync(targetPath, { recursive: true });
}

function resetDirectory(targetPath) {
  fs.rmSync(targetPath, { recursive: true, force: true });
  fs.mkdirSync(targetPath, { recursive: true });
}

function writePackageJson(nextPackageJson) {
  fs.writeFileSync(packagePath, `${JSON.stringify(nextPackageJson, null, 2)}\n`, 'utf8');
}

function copyArtifacts(sourceDir, targetDir) {
  ensureDirectory(targetDir);

  for (const entry of fs.readdirSync(sourceDir, { withFileTypes: true })) {
    if (!entry.isFile() || !entry.name.toLowerCase().endsWith('.exe')) {
      continue;
    }

    fs.copyFileSync(path.join(sourceDir, entry.name), path.join(targetDir, entry.name));
  }
}

try {
  resetDirectory(consolidatedOutputDir);

  for (const variant of variants) {
    const variantOutputDir = path.join(rootDir, 'release', `demo-${variant.key}`);
    resetDirectory(variantOutputDir);
    const variantPackageJson = {
      ...originalPackageJson,
      name: `javflow-demo-${variant.key}`,
      build: {
        ...originalPackageJson.build,
        productName: variant.productName,
        directories: {
          ...(originalPackageJson.build?.directories || {}),
          output: variantOutputDir
        },
        extraMetadata: {
          ...(originalPackageJson.build?.extraMetadata || {}),
          main: 'desktop/main.js',
          name: `javflow-demo-${variant.key}`,
          demoMode: variant.demoMode,
          demoLabel: variant.demoLabel,
          productDisplayName: 'JavFlow Demo'
        },
        win: {
          ...(originalPackageJson.build?.win || {}),
          executableName: variant.executableName
        },
        nsis: {
          ...(originalPackageJson.build?.nsis || {}),
          shortcutName: `${variant.executableName}`,
          uninstallDisplayName: `${variant.executableName}`
        }
      }
    };

    console.log(`\n=== Building ${variant.demoLabel} ===`);
    writePackageJson(variantPackageJson);

    const command = process.platform === 'win32' ? 'cmd.exe' : 'npx';
    const args =
      process.platform === 'win32'
        ? ['/c', 'npx', 'electron-builder', '--win', 'portable', 'nsis', '--publish', 'never']
        : ['electron-builder', '--win', 'portable', 'nsis', '--publish', 'never'];

    execFileSync(command, args, {
      cwd: rootDir,
      stdio: 'inherit',
      env: {
        ...process.env,
        CSC_IDENTITY_AUTO_DISCOVERY: 'false'
      }
    });

    copyArtifacts(variantOutputDir, consolidatedOutputDir);
  }
} finally {
  fs.writeFileSync(packagePath, originalPackageText, 'utf8');
}

import { rcedit } from 'rcedit';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, '..');
const releaseDir = path.join(repoRoot, 'wails-shell', 'release');
const packageJsonPath = path.join(repoRoot, 'package.json');

// Keep EXE metadata tied to the source package version. A hard-coded version
// here can silently produce an executable whose Windows properties disagree
// with the UI and Wails manifest after the next internal version bump.
const packageInfo = JSON.parse(fs.readFileSync(packageJsonPath, 'utf8'));
const packageVersion = String(packageInfo.version || '').trim();
const metadataVersion = String(process.env.JAVFLOW_BUILD_VERSION || packageVersion).trim();
if (!/^\d+\.\d+\.\d+$/.test(metadataVersion)) {
  throw new Error(`JAVFLOW_BUILD_VERSION/package.json version is invalid: ${metadataVersion || '(empty)'}`);
}

const iconPath = path.join(repoRoot, 'build', 'icon.ico');
// Timestamped EXEs are historical handoff artifacts. They must keep the
// metadata they had when produced, so a new build only patches its active
// build output and the stable release handoff path below.
const configuredReleasePath = String(process.env.JAVFLOW_RELEASE_EXE_PATH || '').trim();
const activeReleasePath = configuredReleasePath
  ? (path.isAbsolute(configuredReleasePath) ? path.normalize(configuredReleasePath) : path.resolve(repoRoot, configuredReleasePath))
  : path.join(releaseDir, 'javflow.exe');
const exePaths = [
  path.join(repoRoot, 'wails-shell', 'build', 'bin', 'javflow.exe'),
  activeReleasePath
];

const options = {
  'version-string': {
    FileDescription: 'JavFlow',
    ProductName: 'JavFlow',
    CompanyName: 'raawaa',
    LegalCopyright: 'Based on raawaa/jav-scrapy',
    Comments: 'JavFlow - JAV media library automation workflow'
  },
  'file-version': metadataVersion,
  'product-version': metadataVersion,
  icon: iconPath
};

for (const exePath of exePaths) {
  try {
    await rcedit(exePath, options);
    console.log(`Patched EXE metadata: ${exePath}`);
  } catch (err) {
    // Missing intermediate EXE is acceptable; release EXE may be locked by a running process.
    if (err.code === 'ENOENT') {
      console.log(`Skipped missing EXE: ${exePath}`);
      continue;
    }
    console.warn(`Failed to patch EXE metadata (will continue): ${exePath} - ${err.message || err}`);
  }
}

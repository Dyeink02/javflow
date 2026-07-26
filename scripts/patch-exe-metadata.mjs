import { rcedit } from 'rcedit';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, '..');
const releaseDir = path.join(repoRoot, 'wails-shell', 'release');

const iconPath = path.join(repoRoot, 'build', 'icon.ico');
const fallbackExes = fs.existsSync(releaseDir)
  ? fs.readdirSync(releaseDir)
      .filter(name => /^javflow-\d{8}-\d{6}\.exe$/i.test(name))
      .map(name => path.join(releaseDir, name))
  : [];
const exePaths = [
  path.join(repoRoot, 'wails-shell', 'build', 'bin', 'javflow.exe'),
  path.join(releaseDir, 'javflow.exe'),
  ...fallbackExes
];

const options = {
  'version-string': {
    FileDescription: 'JavFlow',
    ProductName: 'JavFlow',
    CompanyName: 'raawaa',
    LegalCopyright: 'Based on raawaa/jav-scrapy',
    Comments: 'JavFlow - JAV media library automation workflow'
  },
  'file-version': '0.4.0',
  'product-version': '0.4.0',
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

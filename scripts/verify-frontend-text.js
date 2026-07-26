#!/usr/bin/env node

// toolchain-owner: active frontend text integrity verifier; marker=active-toolchain-verify-frontend-text
// Guard active frontend text against mojibake and missing required UI labels.
// This script exists because valid UTF-8 bytes alone do not guarantee that the
// active renderer strings were preserved correctly through past migrations.
//
// Ownership summary:
// 1) verify active desktop frontend text/content survives bundling correctly
// 2) catch missing required UI labels and known mojibake regressions early
// 3) keep text/asset integrity checks out of runtime code paths
//
// Boundary rule:
// verification-only helper; runtime code should not import this file.
//
// File map for maintainers:
// 1) generated artifact + source file read set
// 2) required snippet/corruption guard definitions
// 3) verification runners and failure reporting

const fs = require('fs');
const path = require('path');
const {
  generatedIndexHtmlPath,
  generatedBundleJsPath,
  generatedStylesCssPath,
  desktopDir,
  wailsFrontendRendererDir
} = require('./frontend-paths');
const { FRONTEND_BUNDLE_FILES } = require('./frontend-bundle-files');

const REQUIRED_INDEX_SNIPPETS = [
  '\u72b6\u6001\u8bf4\u660e',
  '\u8fc7\u6ee4\u5f71\u7247\u756a\u53f7'
];

const REQUIRED_BUNDLE_SNIPPETS = [
  '\u6293\u53d6\u9636\u6bb5\u4e0e\u7ed3\u679c\u5165\u53e3',
  '\u6293\u53d6\u4ea7\u7269\u4e0e\u590d\u76d8\u5165\u53e3',
  '\u5c1a\u672a\u751f\u6210\u590d\u76d8\u6458\u8981',
  '\u7b49\u5f85\u6293\u53d6\u5b8c\u6210\u540e\u751f\u6210\u590d\u76d8\u6458\u8981\u3002'
];

const REQUIRED_ACTIVE_SOURCE_SNIPPETS = [
  {
    relativePath: ['common', 'text', 'appInfo.js'],
    snippets: [
      'JavFlow',
      '\u57fa\u4e8e\u5f00\u6e90\u9879\u76ee\uff1araawaa',
      '\u8fd0\u884c\u65e5\u5fd7'
    ]
  },
  {
    relativePath: ['common', 'text', 'serviceText.js'],
    snippets: [
      '\u672a\u627e\u5230\u53ef\u7528\u7684 Chrome / Edge \u6d4f\u89c8\u5668\uff0c\u65e0\u6cd5\u62c9\u53d6\u53c2\u8003\u699c\u5355\u3002',
      '\u6700\u65b0\u699c\u5355',
      '\u672a\u80fd\u5b9a\u4f4d\u5973\u4f18\u76ee\u5f55\u3002'
    ]
  },
  {
    relativePath: ['common', 'text', 'versionHistory.js'],
    snippets: [
      '\u4fee\u590d\u5b89\u88c5\u6d41\u7a0b\uff0c\u5b9e\u73b0\u57fa\u7840\u8fd0\u884c\u80fd\u529b',
      '\u6838\u5fc3\u67b6\u6784\u8fc1\u79fb\u81f3Go\u8bed\u8a00\u5f00\u53d1'
    ]
  },
  {
    relativePath: ['renderer', 'uiText.js'],
    snippets: [
      'JavFlow',
      '\u7248\u672c\u66f4\u65b0',
      '\u6293\u53d6\u8bbe\u7f6e'
    ]
  },
  {
    relativePath: ['renderer', 'partials', 'crawler-hero.html'],
    snippets: ['\u57fa\u4e8e\u5f00\u6e90\u9879\u76ee\uff1a']
  }
];

// Guard against the exact failure class that caused the red-box UI mojibake:
// files were valid UTF-8, but the text literals inside them were already
// corrupted from an older Electron/encoding pass. UTF-8 checks alone cannot
// catch that, so this list blocks known mojibake fragments in active UI paths.
//
// Important diagnostic note:
// PowerShell console output can itself mis-render UTF-8 Chinese on this
// workspace, so source validation here reads bytes with Node and verifies the
// actual file content instead of trusting terminal rendering alone.
const FORBIDDEN_MOJIBAKE_SNIPPETS = [
  '\u93b6\u64b3\u5f47',
  '\u95c3\u8235',
  '\u7edb\u590a\u7ddf',
  '\u95be\u6350\u77fe',
  '\u699b\u6a3f',
  'AV \u7481',
  '\u6fc2\u5145\u7d2d',
  '\u9354\u72ba\u6d47',
  '\u8930\u64b3\u58a0',
  '\u7f01\u64b4\u7049\u934f',
  '\u6fb6\u5d87\u6d0f',
  '\u9422\u71b8\u579a',
  '\u6d93\u5ea3\u7ca8'
];

const ACTIVE_TEXT_FILE_EXTENSIONS = new Set(['.js', '.html']);

function readUtf8(filePath) {
  const content = fs.readFileSync(filePath, 'utf8');
  return content.charCodeAt(0) === 0xfeff ? content.slice(1) : content;
}

function assertFileContains(filePath, snippets) {
  if (!fs.existsSync(filePath)) {
    throw new Error(`missing frontend artifact: ${filePath}`);
  }

  const content = readUtf8(filePath);
  const missing = snippets.filter((snippet) => !content.includes(snippet));
  if (missing.length > 0) {
    throw new Error(`${filePath} missing snippets: ${missing.join(', ')}`);
  }
}

function assertFileDoesNotContain(filePath, snippets) {
  if (!fs.existsSync(filePath)) {
    throw new Error(`missing frontend artifact: ${filePath}`);
  }

  const content = readUtf8(filePath);
  const found = snippets.filter((snippet) => content.includes(snippet));
  if (found.length > 0) {
    throw new Error(`${filePath} contains mojibake snippets: ${found.join(', ')}`);
  }
}

function collectFiles(rootDir, extensions) {
  const results = [];
  const pending = [rootDir];

  while (pending.length > 0) {
    const currentDir = pending.pop();
    const entries = fs.readdirSync(currentDir, { withFileTypes: true });

    for (const entry of entries) {
      const absolutePath = path.join(currentDir, entry.name);
      if (entry.isDirectory()) {
        if (entry.name === '.generated' || entry.name === 'assets') {
          continue;
        }
        pending.push(absolutePath);
        continue;
      }

      if (extensions.has(path.extname(entry.name).toLowerCase())) {
        results.push(absolutePath);
      }
    }
  }

  return results.sort();
}

function main() {
  const activeBundleSourcePaths = FRONTEND_BUNDLE_FILES.map((relativePath) => path.join(desktopDir, relativePath));
  const activeTextSourcePaths = [
    ...new Set([
      ...activeBundleSourcePaths,
      ...collectFiles(path.join(desktopDir, 'common'), ACTIVE_TEXT_FILE_EXTENSIONS),
      ...collectFiles(path.join(desktopDir, 'renderer'), ACTIVE_TEXT_FILE_EXTENSIONS)
    ])
  ];
  const wailsBundlePath = path.join(wailsFrontendRendererDir, 'bundle.js');
  const wailsIndexPath = path.join(wailsFrontendRendererDir, 'index.html');

  assertFileContains(generatedIndexHtmlPath, REQUIRED_INDEX_SNIPPETS);
  assertFileContains(generatedBundleJsPath, REQUIRED_BUNDLE_SNIPPETS);
  assertFileContains(generatedStylesCssPath, ['state-card', 'chip-filtered']);
  assertFileContains(wailsIndexPath, REQUIRED_INDEX_SNIPPETS);
  assertFileContains(wailsBundlePath, REQUIRED_BUNDLE_SNIPPETS);
  for (const requiredSource of REQUIRED_ACTIVE_SOURCE_SNIPPETS) {
    assertFileContains(path.join(desktopDir, ...requiredSource.relativePath), requiredSource.snippets);
  }

  for (const sourcePath of activeTextSourcePaths) {
    assertFileDoesNotContain(sourcePath, FORBIDDEN_MOJIBAKE_SNIPPETS);
  }
  assertFileDoesNotContain(generatedBundleJsPath, FORBIDDEN_MOJIBAKE_SNIPPETS);
  assertFileDoesNotContain(wailsBundlePath, FORBIDDEN_MOJIBAKE_SNIPPETS);
  console.log('[frontend-text-check] OK');
}

main();

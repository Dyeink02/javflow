#!/usr/bin/env node

// toolchain-owner: repository UTF-8 hygiene guard; marker=active-toolchain-check-encoding
// Repository-wide UTF-8 guard for source and docs under active maintenance.
// This catches invalid byte sequences early, but it does not replace higher-
// level mojibake checks for already-corrupted text literals.
//
// It also guards against a more subtle failure mode encountered in this repo:
// a UTF-8 BOM copied into the middle of a file during manual header/comment
// edits. That case still looks like "valid UTF-8" to many tools, but it can
// silently corrupt tokens such as `const` into `锘縞onst`.
//
// Ownership summary:
// 1) scan active source/docs for invalid UTF-8 and embedded BOM corruption
// 2) fail early before mojibake reaches runtime/package outputs
// 3) keep repository-wide encoding hygiene checks in one toolchain script
//
// File map for maintainers:
// 1) directory/extension allow-skip configuration
// 2) UTF-8 decode + BOM anomaly helpers
// 3) repository walk and violation reporting

const fs = require('fs');
const path = require('path');
const { TextDecoder } = require('util');

const ROOT_DIR = process.cwd();
const decoder = new TextDecoder('utf-8', { fatal: true });

const SKIP_DIRS = new Set([
  '.git',
  '.husky/_',
  '.tmp-organizer-contract-check',
  '.tmp-organizer-source-meta-check',
  '.reasonix',
  '.qoder',
  '.claude',
  '.wails-dev',
  '.generated',
  'AV订阅',
  'backups',
  'node_modules',
  'dist',
  'release',
  'build',
  'runtime',
  'ffmpeg',
  'tmp',
  'tmp-actor-filter-check',
  '.idea',
  '.vscode'
]);

const CHECK_EXTENSIONS = new Set([
  '.js',
  '.cjs',
  '.mjs',
  '.ts',
  '.tsx',
  '.go',
  '.json',
  '.md',
  '.html',
  '.css',
  '.scss',
  '.less',
  '.yml',
  '.yaml',
  '.txt',
  '.xml',
  '.ini',
  '.nsh'
]);

const CHECK_FILENAMES = new Set(['.editorconfig', '.gitignore', '.npmignore', 'LICENSE']);

function shouldSkipDirectory(relativeDir) {
  const normalized = relativeDir.replace(/\\/g, '/');
  if (!normalized) {
    return false;
  }

  // Encoding checks should validate the active source tree only. Local
  // snapshots, generated upload copies, and dependency folders often preserve
  // historical files that are useful for recovery but should not fail current
  // maintenance verification.

  // Check if any path segment matches a skip directory entry.
  // This correctly excludes nested paths like wails-shell/release/...,
  // wails-shell/build/..., and their contained node_modules.
  const segments = normalized.split('/');
  for (const seg of segments) {
    if (SKIP_DIRS.has(seg)) {
      return true;
    }
  }

  const basename = path.basename(normalized);
  if (
    basename.startsWith('.tmp-') ||
    basename.startsWith('tmp-') ||
    basename.includes('github-upload')
  ) {
    return true;
  }

  return false;
}

function shouldCheckFile(filePath) {
  const basename = path.basename(filePath);
  if (CHECK_FILENAMES.has(basename)) {
    return true;
  }
  return CHECK_EXTENSIONS.has(path.extname(filePath).toLowerCase());
}

function walk(dirPath, relativeDir = '') {
  const output = [];
  const entries = fs.readdirSync(dirPath, { withFileTypes: true });

  entries.forEach((entry) => {
    const absolutePath = path.join(dirPath, entry.name);
    const relativePath = path.join(relativeDir, entry.name);

    if (entry.isDirectory()) {
      if (shouldSkipDirectory(relativePath)) {
        return;
      }
      output.push(...walk(absolutePath, relativePath));
      return;
    }

    if (entry.isFile() && shouldCheckFile(absolutePath)) {
      output.push(absolutePath);
    }
  });

  return output;
}

function isUtf8(buffer) {
  try {
    decoder.decode(buffer);
    return true;
  } catch {
    return false;
  }
}

function hasUtf8BomAt(buffer, offset) {
  return buffer.length >= offset + 3 && buffer[offset] === 0xef && buffer[offset + 1] === 0xbb && buffer[offset + 2] === 0xbf;
}

function findInvalidBomOffset(buffer) {
  if (hasUtf8BomAt(buffer, 0)) {
    if (hasUtf8BomAt(buffer, 3)) {
      return 3;
    }
  }

  for (let offset = hasUtf8BomAt(buffer, 0) ? 3 : 0; offset < buffer.length - 2; offset += 1) {
    if (hasUtf8BomAt(buffer, offset)) {
      return offset;
    }
  }

  return -1;
}

function main() {
  const files = walk(ROOT_DIR);
  const failures = [];

  files.forEach((filePath) => {
    try {
      const buffer = fs.readFileSync(filePath);
      if (!isUtf8(buffer)) {
        failures.push(path.relative(ROOT_DIR, filePath));
        return;
      }

      const invalidBomOffset = findInvalidBomOffset(buffer);
      if (invalidBomOffset >= 0) {
        failures.push(`${path.relative(ROOT_DIR, filePath)} (unexpected UTF-8 BOM at byte ${invalidBomOffset})`);
      }
    } catch (error) {
      failures.push(`${path.relative(ROOT_DIR, filePath)} (read failed: ${error.message})`);
    }
  });

  if (failures.length > 0) {
    console.error('[encoding-check] Found non-UTF8 files:');
    failures.forEach((item) => console.error(`- ${item}`));
    process.exit(1);
  }

  console.log(`[encoding-check] OK (${files.length} files checked as UTF-8).`);
}

main();

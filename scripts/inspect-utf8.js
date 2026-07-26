#!/usr/bin/env node

// toolchain-owner: UTF-8 diagnosis helper for maintainers; marker=active-toolchain-inspect-utf8
// UTF-8 source-of-truth inspector for active maintenance work.
// Use this when PowerShell/Get-Content appears to show mojibake. The script
// reads the file as UTF-8 and prints both raw text and ASCII-safe escaped text
// so maintainers can distinguish display-layer issues from real source damage.
//
// Ownership summary:
// 1) inspect UTF-8 source text without relying on terminal display behavior
// 2) render escaped diagnostics around suspicious lines/offsets
// 3) keep encoding diagnosis outside product runtime logic
//
// Boundary rule:
// maintenance-only CLI helper; runtime code should not import this file.
//
// File map for maintainers:
// 1) CLI argument/offset parsing helpers
// 2) ASCII-safe escape rendering helpers
// 3) file read + inspection window printing

const fs = require('fs');
const path = require('path');

function parsePositiveInteger(value, fallback) {
  const parsed = Number.parseInt(String(value || ''), 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

function escapeForASCII(text) {
  let output = '';

  for (const char of String(text || '')) {
    const codePoint = char.codePointAt(0);
    if (codePoint >= 0x20 && codePoint <= 0x7e && char !== '\\') {
      output += char;
      continue;
    }

    if (char === '\\') {
      output += '\\\\';
      continue;
    }

    if (codePoint <= 0xffff) {
      output += `\\u${codePoint.toString(16).padStart(4, '0')}`;
      continue;
    }

    const adjusted = codePoint - 0x10000;
    const high = 0xd800 + (adjusted >> 10);
    const low = 0xdc00 + (adjusted & 0x3ff);
    output += `\\u${high.toString(16).padStart(4, '0')}\\u${low.toString(16).padStart(4, '0')}`;
  }

  return output;
}

function resolveTargetPath(inputPath) {
  if (!inputPath) {
    throw new Error('Usage: node scripts/inspect-utf8.js <file> [startLine] [endLine]');
  }

  return path.resolve(process.cwd(), inputPath);
}

function main() {
  const [, , targetArg, startArg, endArg] = process.argv;
  const targetPath = resolveTargetPath(targetArg);

  if (!fs.existsSync(targetPath)) {
    throw new Error(`File not found: ${targetPath}`);
  }

  const text = fs.readFileSync(targetPath, 'utf8');
  const lines = text.split(/\r?\n/);
  const startLine = parsePositiveInteger(startArg, 1);
  const endLine = parsePositiveInteger(endArg, Math.min(lines.length, startLine + 19));
  const from = Math.max(1, Math.min(startLine, lines.length));
  const to = Math.max(from, Math.min(endLine, lines.length));

  console.log(`FILE ${targetPath}`);
  console.log(`RANGE ${from}-${to} / ${lines.length}`);

  for (let lineNumber = from; lineNumber <= to; lineNumber += 1) {
    const line = lines[lineNumber - 1];
    const label = String(lineNumber).padStart(4, ' ');
    console.log(`${label} RAW: ${line}`);
    console.log(`${label} ESC: ${escapeForASCII(line)}`);
  }
}

try {
  main();
} catch (error) {
  console.error(`inspect-utf8 failed: ${error.message}`);
  process.exit(1);
}

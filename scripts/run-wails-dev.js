#!/usr/bin/env node

// toolchain-owner: active Wails dev launcher; marker=active-toolchain-run-wails-dev
// Primary desktop dev entry for the current Wails runtime.
// When the live desktop UI behaves differently from the packaged build, debug
// this path before checking archived Electron helpers.
//
// Ownership summary:
// 1) launch the maintained Wails dev workflow with the current frontend assets
// 2) keep dev toolchain resolution out of ad hoc shell commands
// 3) provide one predictable entry when desktop dev boot behavior regresses
//
// Boundary rule:
// toolchain/dev-only helper; runtime code should not import this file.
//
// File map for maintainers:
// 1) toolchain/path imports from `wails-paths`
// 2) dev command assembly in `main()`
// 3) process exit propagation

const {
  repoRoot,
  wailsDir,
  resolveNpmBinary,
  resolveWailsBinary,
  runCommand
} = require('./wails-paths');

function main() {
  const npmBinary = resolveNpmBinary();
  const wailsBinary = resolveWailsBinary();

  runCommand(npmBinary, ['run', 'build:desktop-frontend'], { cwd: repoRoot });
  runCommand(npmBinary, ['run', 'sync:wails-frontend'], { cwd: repoRoot });
  runCommand(wailsBinary, ['dev'], { cwd: wailsDir });
}

main();

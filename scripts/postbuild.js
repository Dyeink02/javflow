#!/usr/bin/env node

// packaging-owner: maintained Wails postbuild release sync; marker=active-toolchain-postbuild
// Release-path sync step for the current Wails build chain.
// Keep `wails-shell/release/javflow.exe` aligned with the actual Wails
// output in `wails-shell/build/bin/javflow.exe`.
// Packaging scripts read from `release/`, while manual testing usually targets
// `build/bin/`, so this file keeps both paths synchronized.
//
// Ownership summary:
// 1) sync the built Wails EXE into the maintained release handoff path
// 2) keep release-path alignment out of larger build/package scripts
// 3) provide one tiny post-build step maintainers can invoke directly
//
// File map for maintainers:
// 1) release sync import from `wails-paths`
// 2) minimal orchestration in `main()`
// 3) success-path reporting

const { copyBuildExeToRelease } = require('./wails-paths');

function main() {
  const releaseResult = copyBuildExeToRelease();
  console.log('OK: synced Wails EXE into release directory');
  console.log(`    ${releaseResult.actualPath}`);
}

main();

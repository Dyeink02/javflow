#!/usr/bin/env node

// toolchain-owner: active Wails frontend sync step; marker=active-toolchain-sync-wails-frontend
// Frontend artifact sync step for the current Wails build chain.
// Scope boundary:
// - copies already-generated renderer artifacts into the Wails tree
// - does not bundle JS/CSS/HTML itself
// - does not own desktop runtime or sidecar behavior
//
// Workflow map:
// 1) verify generated frontend artifacts already exist
// 2) clear the target Wails renderer directory
// 3) copy html/css/js
// 4) mirror assets required by generated paths
//
// Ownership summary:
// 1) copy already-generated desktop artifacts into the Wails frontend tree
// 2) keep sync-time filesystem cleanup separate from bundle generation logic
// 3) mirror static assets needed by generated frontend paths
//
// File map for maintainers:
// 1) generated artifact existence guards
// 2) target cleanup/copy helpers
// 3) Wails renderer + asset sync execution

const fs = require('fs');
const path = require('path');
const {
  rendererAssetsDir,
  generatedIndexHtmlPath,
  generatedStylesCssPath,
  generatedBundleJsPath,
  wailsFrontendRendererDir
} = require('./frontend-paths');

// Copies generated desktop frontend artifacts into the Wails frontend tree.
// This is part of the current build chain; if the packaged UI shows stale
// content, verify this sync step before touching compatibility runtimes.

const targetRenderer = wailsFrontendRendererDir;
const targetDesktopDir = path.dirname(targetRenderer);

function ensureDirectory(targetPath) {
  fs.mkdirSync(targetPath, { recursive: true });
}

function removeIfExists(targetPath) {
  if (fs.existsSync(targetPath)) {
    fs.rmSync(targetPath, { recursive: true, force: true });
  }
}

function copyFile(sourcePath, targetPath) {
  ensureDirectory(path.dirname(targetPath));
  fs.copyFileSync(sourcePath, targetPath);
}

function copyDirectory(sourceDir, targetDir) {
  if (!fs.existsSync(sourceDir)) {
    return;
  }

  ensureDirectory(targetDir);

  const entries = fs.readdirSync(sourceDir, { withFileTypes: true });
  for (const entry of entries) {
    const sourcePath = path.join(sourceDir, entry.name);
    const targetPath = path.join(targetDir, entry.name);

    if (entry.isDirectory()) {
      copyDirectory(sourcePath, targetPath);
      continue;
    }

    copyFile(sourcePath, targetPath);
  }
}

function copyAssetsIfPresent() {
  if (!fs.existsSync(rendererAssetsDir)) {
    return;
  }

  // `.generated/styles.css` uses `../assets/...`, so the Wails frontend
  // needs assets under `frontend/desktop/assets`. We also keep a mirrored
  // copy in `renderer/assets` for compatibility with any future `./assets/...`
  // references from renderer-root files.
  copyDirectory(rendererAssetsDir, path.join(targetDesktopDir, 'assets'));
  copyDirectory(rendererAssetsDir, path.join(targetRenderer, 'assets'));
}

function main() {
  const requiredArtifacts = [
    { source: generatedIndexHtmlPath, target: path.join(targetRenderer, 'index.html') },
    { source: generatedStylesCssPath, target: path.join(targetRenderer, 'styles.css') },
    { source: generatedBundleJsPath, target: path.join(targetRenderer, 'bundle.js') }
  ];

  for (const artifact of requiredArtifacts) {
    if (!fs.existsSync(artifact.source)) {
      throw new Error(`Missing frontend artifact: ${artifact.source}\nRun npm run build:desktop-frontend first.`);
    }
  }

  ensureDirectory(path.dirname(targetRenderer));
  removeIfExists(targetRenderer);
  ensureDirectory(targetRenderer);

  for (const artifact of requiredArtifacts) {
    copyFile(artifact.source, artifact.target);
  }

  copyAssetsIfPresent();
  console.log(`Wails frontend artifacts synced from desktop/renderer/.generated to: ${targetRenderer}`);
}

main();

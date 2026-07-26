#!/usr/bin/env node

// compatibility-owner: active compatibility-lane sidecar bootstrap smoke; marker=compat-toolchain-smoke-phase1-bootstrap
// Node-side sidecar bootstrap smoke for the current compatibility lane.
// Scope boundary:
// - verifies sidecar boot + bridge ping only
// - does not validate packaged UI rendering
// - does not validate crawl correctness or Cloudflare behavior
//
// Ownership summary:
// 1) verify the compatibility sidecar can boot and answer a basic bridge ping
// 2) keep bootstrap smoke coverage separate from crawl/UI correctness checks
// 3) make the archived compatibility lane easier to isolate when it regresses
//
// File map for maintainers:
// 1) sidecar entry path setup
// 2) child-process launch and message parsing
// 3) bootstrap timeout and exit handling

const { spawn } = require('child_process');
const path = require('path');

// Minimal sidecar bootstrap smoke for the Wails compatibility lane.
// Use this only to verify the Node sidecar can boot and answer bridge pings;
// it is not the main desktop UI smoke path.

const repoRoot = path.resolve(__dirname, '..');
const sidecarEntry = path.join(repoRoot, 'desktop', 'sidecar', 'index.js');

function runSmoke() {
  return new Promise((resolve, reject) => {
    const child = spawn('node', [sidecarEntry], {
      cwd: repoRoot,
      stdio: ['pipe', 'pipe', 'pipe']
    });

    let resolved = false;

    child.stderr.on('data', (chunk) => {
      if (!resolved) {
        reject(new Error(`sidecar stderr: ${chunk.toString('utf8')}`));
        resolved = true;
        child.kill();
      }
    });

    child.stdout.on('data', (chunk) => {
      const lines = chunk
        .toString('utf8')
        .split(/\r?\n/)
        .map((item) => item.trim())
        .filter(Boolean);

      for (const line of lines) {
        const packet = JSON.parse(line);

        if (packet.kind === 'event' && packet.event === 'sidecar.lifecycle' && packet.data && packet.data.state === 'starting') {
          child.stdin.write(
            `${JSON.stringify({
              version: 'bridge.v1',
              kind: 'command',
              id: 'smoke-bootstrap',
              domain: 'system',
              action: 'bootstrap',
              timestamp: new Date().toISOString(),
              payload: {
                runtimeContext: {
                  repoRoot,
                  appPath: repoRoot,
                  resourcesPath: repoRoot,
                  userData: path.join(repoRoot, '.wails-dev', 'userData'),
                  documents: path.join(repoRoot, '.wails-dev', 'documents'),
                  temp: path.join(repoRoot, '.wails-dev', 'temp')
                }
              }
            })}\n`
          );
          continue;
        }

        if (packet.kind === 'result' && packet.id === 'smoke-bootstrap') {
          child.stdin.write(
            `${JSON.stringify({
              version: 'bridge.v1',
              kind: 'command',
              id: 'smoke-ping',
              domain: 'system',
              action: 'ping',
              timestamp: new Date().toISOString(),
              payload: {}
            })}\n`
          );
          continue;
        }

        if (packet.kind === 'result' && packet.id === 'smoke-ping') {
          if (!packet.ok) {
            reject(new Error(packet.error ? packet.error.message : 'smoke ping failed'));
          } else {
            resolved = true;
            child.kill();
            resolve(packet.data);
          }
        }
      }
    });

    child.on('exit', (code) => {
      if (!resolved && code !== 0) {
        reject(new Error(`sidecar exited with code ${code}`));
      }
    });
  });
}

runSmoke()
  .then((data) => {
    console.log('Wails Phase 1 smoke passed:', JSON.stringify(data));
  })
  .catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
  });

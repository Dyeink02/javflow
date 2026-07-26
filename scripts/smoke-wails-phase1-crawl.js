#!/usr/bin/env node

// compatibility-owner: active compatibility-lane crawl-start smoke; marker=compat-toolchain-smoke-phase1-crawl-start
// Node-side crawl-launch smoke for the current compatibility lane.
// Scope boundary:
// - verifies the sidecar can start a crawl task and emit first state
// - does not validate full crawl completeness
// - does not validate packaged UI wiring
//
// Ownership summary:
// 1) verify the compatibility sidecar can launch a crawl and emit initial state
// 2) keep crawl-start smoke coverage separate from packaged UI smoke coverage
// 3) document that this is a narrow compatibility-lane guard only
//
// File map for maintainers:
// 1) sidecar entry/output path setup
// 2) child-process launch and stdout/stderr capture
// 3) crawl-start success/failure timeout handling

const { spawn } = require('child_process');
const path = require('path');

// Crawl-start smoke for the Node sidecar compatibility lane.
// This is useful when validating that the sidecar bridge still launches a crawl
// task, but packaged desktop UI regressions should be debugged elsewhere first.

const repoRoot = path.resolve(__dirname, '..');
const sidecarEntry = path.join(repoRoot, 'desktop', 'sidecar', 'index.js');
const outputDir = path.join(repoRoot, 'tmp', 'smoke-wails-phase1-crawl');

function parsePacket(line) {
  const trimmed = String(line || '').trim();
  if (!trimmed.startsWith('{')) {
    return null;
  }

  try {
    return JSON.parse(trimmed);
  } catch (error) {
    return null;
  }
}

function createCommand(id, domain, action, payload) {
  return {
    version: 'bridge.v1',
    kind: 'command',
    id,
    domain,
    action,
    taskId: '',
    timestamp: new Date().toISOString(),
    payload
  };
}

function runSmoke() {
  return new Promise((resolve, reject) => {
    const child = spawn('node', [sidecarEntry], {
      cwd: repoRoot,
      stdio: ['pipe', 'pipe', 'pipe']
    });

    let stdoutBuffer = '';
    let stderrBuffer = '';
    let ready = false;
    let startResultOk = false;
    let stateObserved = false;
    let settled = false;

    const timeout = setTimeout(() => {
      if (settled) {
        return;
      }

      settled = true;
      child.kill();
      reject(new Error('Wails Phase 1 crawl smoke timed out while waiting for the first crawl state event.'));
    }, 45000);

    function finish(error, data) {
      if (settled) {
        return;
      }

      settled = true;
      clearTimeout(timeout);
      child.kill();

      if (error) {
        reject(error);
        return;
      }

      resolve(data);
    }

    function maybeComplete(packet) {
      if (!ready || !startResultOk || !stateObserved) {
        return;
      }

      finish(null, packet && packet.data ? packet.data : { ok: true });
    }

    function send(packet) {
      child.stdin.write(`${JSON.stringify(packet)}\n`);
    }

    child.stderr.on('data', (chunk) => {
      stderrBuffer += chunk.toString('utf8');
    });

    child.stdout.on('data', (chunk) => {
      stdoutBuffer += chunk.toString('utf8');
      const lines = stdoutBuffer.split(/\r?\n/);
      stdoutBuffer = lines.pop() || '';

      lines.forEach((line) => {
        const packet = parsePacket(line);
        if (!packet) {
          return;
        }

        if (
          packet.kind === 'event' &&
          packet.event === 'sidecar.lifecycle' &&
          packet.data &&
          packet.data.state === 'starting'
        ) {
          send(
            createCommand('smoke-bootstrap', 'system', 'bootstrap', {
              runtimeContext: {
                repoRoot,
                appPath: repoRoot,
                resourcesPath: repoRoot,
                userData: path.join(repoRoot, '.wails-dev', 'userData'),
                documents: path.join(repoRoot, '.wails-dev', 'documents'),
                temp: path.join(repoRoot, '.wails-dev', 'temp')
              }
            })
          );
          return;
        }

        if (packet.kind === 'result' && packet.id === 'smoke-bootstrap') {
          ready = true;
          send(
            createCommand('smoke-start', 'crawl', 'start', {
              base: 'https://www.javbus.com/star/wc8',
              output: outputDir,
              limit: 1,
              totalPages: 1,
              itemsPerPage: 30,
              parallel: 1,
              delay: 1,
              timeout: 30000,
              proxy: '',
              magnetExcludeKeywords: '',
              magnetContentValidation: false,
              cloudflare: false,
              secondValidation: true,
              nomag: false,
              allmag: false,
              nopic: true,
              taskTemplate: 'balanced'
            })
          );
          return;
        }

        if (packet.kind === 'result' && packet.id === 'smoke-start') {
          if (!packet.ok) {
            finish(new Error(packet.error && packet.error.message ? packet.error.message : 'start-crawl failed'));
            return;
          }

          startResultOk = true;
          maybeComplete(packet);
          return;
        }

        if (packet.kind === 'event' && packet.event === 'crawl.state' && packet.data) {
          stateObserved = true;
          maybeComplete(packet);
        }
      });
    });

    child.on('exit', (code) => {
      if (!settled && code !== 0) {
        const stderrMessage = stderrBuffer.trim();
        finish(
          new Error(
            stderrMessage
              ? `sidecar exited unexpectedly with code ${code}: ${stderrMessage}`
              : `sidecar exited unexpectedly with code ${code}`
          )
        );
      }
    });
  });
}

runSmoke()
  .then((data) => {
    console.log('Wails Phase 1 crawl smoke passed:', JSON.stringify(data));
  })
  .catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
  });

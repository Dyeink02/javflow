// toolchain-owner: vr filter smoke test; marker=verify-vr-filter-smoke
// Ownership summary:
//   Smoke test for the VR-filter sidecar path: exercise sidecar commands and verify outputs.
//
// File map for maintainers:
//   1) Sidecar spawn and bridge packet helpers.
//   2) VR-filter command sequences.
//   3) Output assertion and cleanup.
//
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const repoRoot = path.resolve(__dirname, '..');
const sidecarEntry = path.join(repoRoot, 'desktop', 'sidecar', 'index.js');
const outputDir = path.join(repoRoot, 'tmp', 'verify-vr-filter-smoke');

function parsePacket(line) {
  const trimmed = String(line || '').trim();
  if (!trimmed.startsWith('{')) return null;
  try {
    return JSON.parse(trimmed);
  } catch {
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

function hasFilmCode(text, code) {
  const normalized = String(text || '').toLowerCase();
  return normalized.includes(code.toLowerCase());
}

function runSmoke() {
  return new Promise((resolve, reject) => {
    fs.rmSync(outputDir, { recursive: true, force: true });
    fs.mkdirSync(outputDir, { recursive: true });

    const child = spawn('node', [sidecarEntry], {
      cwd: repoRoot,
      stdio: ['pipe', 'pipe', 'pipe']
    });

    let stdoutBuffer = '';
    let stderrBuffer = '';
    let settled = false;
    let ready = false;
    let startResultOk = false;
    let stateObserved = false;
    let completionPacket = null;
    const filterMatches = [];

    const timeout = setTimeout(() => {
      if (settled) return;
      settled = true;
      child.kill();
      reject(new Error('VR filter smoke timed out'));
    }, 240000);

    function finish(error, data) {
      if (settled) return;
      settled = true;
      clearTimeout(timeout);
      child.kill();
      if (error) return reject(error);
      resolve(data);
    }

    function send(packet) {
      child.stdin.write(`${JSON.stringify(packet)}\n`);
    }

    function isFinalState(payload) {
      if (!payload) return false;
      const status = String(payload.status || '').toLowerCase();
      return status === 'completed' || status === 'stopped' || status === 'failed' || status === 'incomplete';
    }

    child.stderr.on('data', (chunk) => {
      stderrBuffer += chunk.toString('utf8');
    });

    child.stdout.on('data', (chunk) => {
      stdoutBuffer += chunk.toString('utf8');
      const lines = stdoutBuffer.split(/\r?\n/);
      stdoutBuffer = lines.pop() || '';

      for (const line of lines) {
        const packet = parsePacket(line);
        if (!packet) continue;

        if (
          packet.kind === 'event' &&
          packet.event === 'sidecar.lifecycle' &&
          packet.data &&
          packet.data.state === 'starting'
        ) {
          send(
            createCommand('vr-bootstrap', 'system', 'bootstrap', {
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
          continue;
        }

        if (packet.kind === 'result' && packet.id === 'vr-bootstrap') {
          ready = true;
          send(
            createCommand('vr-start', 'crawl', 'start', {
              base: 'https://www.javbus.com/star/vb3',
              output: outputDir,
              limit: 30,
              totalPages: 1,
              itemsPerPage: 30,
              parallel: 2,
              delay: 1,
              timeout: 30000,
              proxy: 'http://127.0.0.1:7897',
              magnetExcludeKeywords: '',
              magnetContentValidation: true,
              cloudflare: true,
              secondValidation: true,
              nomag: false,
              allmag: false,
              nopic: true,
              taskTemplate: 'balanced',
              actressCountFilterThreshold: 0,
              filmCodeFilterThreshold: 'VR'
            })
          );
          continue;
        }

        if (packet.kind === 'result' && packet.id === 'vr-start') {
          if (!packet.ok) {
            finish(new Error(packet.error && packet.error.message ? packet.error.message : 'start-crawl failed'));
            return;
          }
          startResultOk = true;
          continue;
        }

        if (packet.kind === 'event' && packet.event === 'crawl.state' && packet.data) {
          stateObserved = true;
          const payload = packet.data;
          if (
            payload &&
            payload.logMessage &&
            (payload.logMessage.includes('film code filter matched') || payload.logMessage.includes('番号过滤命中'))
          ) {
            filterMatches.push(payload.logMessage);
          }
          if (isFinalState(payload)) {
            completionPacket = payload;
            finish(null, { completionPacket, filterMatches });
          }
        }
      }
    });

    child.on('exit', (code) => {
      if (!settled) {
        finish(new Error(`sidecar exited unexpectedly with code ${code}: ${stderrBuffer.trim()}`));
      }
    });
  });
}

async function main() {
  const result = await runSmoke();
  console.log('Smoke finished');
  console.log('Filter matches:', result.filterMatches.length, result.filterMatches.slice(0, 10));

  const magnetPath = path.join(outputDir, 'magnet-links.txt');
  const filmDataPath = path.join(outputDir, 'filmData.json');
  const logPath = path.join(outputDir, 'logs', 'latest-log.txt');

  if (!fs.existsSync(magnetPath)) {
    console.log('magnet-links.txt not generated');
    process.exit(1);
  }

  const magnets = fs.readFileSync(magnetPath, 'utf8').split('\n').filter(Boolean);
  const vrMagnets = magnets.filter((line) => hasFilmCode(line, 'VR'));
  console.log('Total magnets:', magnets.length);
  console.log('VR magnets:', vrMagnets.length);

  if (vrMagnets.length > 0) {
    console.log('FAIL: found VR magnets in output');
    console.log(vrMagnets.slice(0, 10).join('\n'));
    process.exit(1);
  }

  const filmData = JSON.parse(fs.readFileSync(filmDataPath, 'utf8'));
  const vrRecords = filmData.filter((r) => hasFilmCode(r.title, 'VR'));
  console.log('filmData records:', filmData.length, 'VR records:', vrRecords.length);

  if (result.filterMatches.length === 0 && vrRecords.length > 0) {
    console.log('WARN: no filter-match logs but VR records present');
    process.exit(1);
  }

  console.log('PASS: no VR content in magnet output');
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});

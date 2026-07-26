// Entry point for the Node sidecar process used by the Wails compatibility
// path.
// compatibility-owner: active crawl-compatible sidecar entrypoint; marker=compat-sidecar-entry
//
// This is not the primary application runtime anymore. It remains in place for
// compatibility flows, mainly legacy Node/Puppeteer-assisted operations such as
// Cloudflare / age-check bypass. When debugging non-sidecar Wails behavior,
// start with the Go bridge/runtime path first.
//
// Ownership summary:
// 1) read bridge command packets from stdin
// 2) route them through the sidecar command router
// 3) write result/event packets back to stdout
// 4) fail closed on process-level errors
//
// File map for maintainers:
// 1) packet/result serialization
// 2) sidecar bootstrap wiring
// 3) stdin command loop + process-level failure handling
//
// Domain behavior belongs in the router/services, not in this entrypoint.

const readline = require('readline');

const { BRIDGE_VERSION } = require('../common/bridgeProtocol.js');
const { createEventBus } = require('./services/eventBus.js');
const { createCommandRouter } = require('./commandRouter.js');

function writePacket(packet) {
  process.stdout.write(`${JSON.stringify(packet)}\n`);
}

function createResultPacket(packet, ok, data, error) {
  return {
    version: BRIDGE_VERSION,
    kind: 'result',
    id: String(packet.id || ''),
    domain: String(packet.domain || ''),
    action: String(packet.action || ''),
    taskId: String(packet.taskId || ''),
    ok,
    timestamp: new Date().toISOString(),
    data: ok ? data ?? null : null,
    error: ok
      ? null
      : {
          code: 'SIDECAR_ERROR',
          message: error instanceof Error ? error.message : String(error),
          retriable: false
        }
  };
}

const eventBus = createEventBus({ writePacket });
const router = createCommandRouter({
  fs: require('fs'),
  eventBus
});

// Process-level failures should be classified before patching:
// 1) malformed packet / wrong domain routing -> inspect `commandRouter.js`
// 2) lifecycle/log/state packet shape issue -> inspect `services/eventBus.js`
// 3) child-process bootstrap/stdin-stdout wiring issue -> inspect this file
eventBus.emitLifecycle('starting', 'Node sidecar starting');

process.on('uncaughtException', (error) => {
  eventBus.emitLifecycle('error', error instanceof Error ? error.message : String(error));
  // After an uncaught exception the Node process is no longer trustworthy.
  // Exit immediately so later crawler sessions do not inherit damaged state.
  process.exit(1);
});

process.on('unhandledRejection', (error) => {
  eventBus.emitLifecycle('error', error instanceof Error ? error.message : String(error));
  process.exit(1);
});

const rl = readline.createInterface({
  input: process.stdin,
  crlfDelay: Infinity
});

let queue = Promise.resolve();

rl.on('line', (line) => {
  const rawLine = String(line || '').trim();
  if (!rawLine) {
    return;
  }

  queue = queue
    // Packet handling stays serialized so archived JS runtime components and
    // sidecar-local compatibility state do not receive overlapping mutations.
    .then(async () => {
      let packet = null;
      try {
        packet = JSON.parse(rawLine);
      } catch (error) {
        writePacket(
          createResultPacket(
            {
              id: '',
              domain: 'system',
              action: 'invalid-json',
              taskId: ''
            },
            false,
            null,
            error
          )
        );
        return;
      }

      if (packet.kind !== 'command') {
        writePacket(createResultPacket(packet, false, null, new Error('Only command packets are supported.')));
        return;
      }

      try {
        const result = await router.handle(packet);
        writePacket(createResultPacket(packet, true, result, null));

        if (packet.domain === 'system' && packet.action === 'shutdown') {
          rl.close();
          process.exit(0);
        }
      } catch (error) {
        writePacket(createResultPacket(packet, false, null, error));
      }
    })
    .catch((error) => {
      eventBus.emitLifecycle('error', error instanceof Error ? error.message : String(error));
    });
});

rl.on('close', () => {
  eventBus.emitLifecycle('stopped', 'Sidecar stdin closed; exiting');
  process.exit(0);
});

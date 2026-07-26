const assert = require('assert');

const { BRIDGE_VERSION, BRIDGE_EVENTS } = require('../desktop/common/bridgeProtocol.js');
const { createEventBus } = require('../desktop/sidecar/services/eventBus.js');

describe('sidecar eventBus protocol shaping', () => {
  it('emits lifecycle, log, state, and log-context packets with bridge metadata', () => {
    const packets = [];
    const eventBus = createEventBus({
      writePacket(packet) {
        packets.push(packet);
      }
    });

    eventBus.emitLifecycle('ready', 'Node sidecar ready', { taskId: 'task-1' });
    eventBus.emitLog(
      'crawl',
      {
        level: 'warn',
        message: 'crawl-log',
        timestamp: '2026-05-08T00:00:00.000Z'
      },
      { action: 'start', taskId: 'task-2' }
    );
    eventBus.emitState(
      'organizer',
      {
        status: 'running',
        message: 'organizer-state',
        timestamp: '2026-05-08T00:00:01.000Z'
      },
      { taskId: 'task-3' }
    );
    eventBus.emitLogContext(
      {
        latestLogFile: 'latest-log.txt'
      },
      { taskId: 'task-4' }
    );

    assert.strictEqual(packets.length, 4);

    assert.deepStrictEqual(
      {
        version: packets[0].version,
        kind: packets[0].kind,
        event: packets[0].event,
        domain: packets[0].domain,
        taskId: packets[0].taskId,
        state: packets[0].data.state,
        pidType: typeof packets[0].data.pid,
        message: packets[0].data.message
      },
      {
        version: BRIDGE_VERSION,
        kind: 'event',
        event: BRIDGE_EVENTS.sidecarLifecycle,
        domain: 'system',
        taskId: 'task-1',
        state: 'ready',
        pidType: 'number',
        message: 'Node sidecar ready'
      }
    );

    assert.deepStrictEqual(
      {
        event: packets[1].event,
        domain: packets[1].domain,
        action: packets[1].action,
        taskId: packets[1].taskId,
        level: packets[1].data.level,
        message: packets[1].data.message,
        timestamp: packets[1].data.timestamp
      },
      {
        event: BRIDGE_EVENTS.crawlLog,
        domain: 'crawl',
        action: 'start',
        taskId: 'task-2',
        level: 'warn',
        message: 'crawl-log',
        timestamp: '2026-05-08T00:00:00.000Z'
      }
    );

    assert.deepStrictEqual(
      {
        event: packets[2].event,
        domain: packets[2].domain,
        taskId: packets[2].taskId,
        status: packets[2].data.status,
        message: packets[2].data.message,
        timestamp: packets[2].data.timestamp
      },
      {
        event: BRIDGE_EVENTS.organizerState,
        domain: 'organizer',
        taskId: 'task-3',
        status: 'running',
        message: 'organizer-state',
        timestamp: '2026-05-08T00:00:01.000Z'
      }
    );

    assert.deepStrictEqual(
      {
        event: packets[3].event,
        domain: packets[3].domain,
        taskId: packets[3].taskId,
        latestLogFile: packets[3].data.latestLogFile
      },
      {
        event: BRIDGE_EVENTS.crawlLogContext,
        domain: 'crawl',
        taskId: 'task-4',
        latestLogFile: 'latest-log.txt'
      }
    );
  });
});

const assert = require('assert');

const { createOrganizerEventMirror } = require('../desktop/sidecar/services/organizerEventMirror.js');

describe('sidecar organizerEventMirror compatibility helper', () => {
  it('normalizes organizer log/state event payloads and timestamps', () => {
    const emitted = [];
    const mirror = createOrganizerEventMirror({
      eventBus: {
        emitEvent(event, domain, data, extra) {
          emitted.push({ event, domain, data, extra });
        }
      }
    });

    mirror.emitOrganizerLog(
      {
        level: 'warn',
        message: 'log-message',
        timestamp: '2026-05-08T01:02:03.000Z'
      },
      { taskId: 'organizer-1' }
    );
    mirror.emitOrganizerState(
      {
        status: 'running',
        message: 'state-message'
      },
      { taskId: 'organizer-2' }
    );
    const timed = mirror.createTimedOrganizerLog('info', 'timed-message', '2026-05-08T04:05:06.000Z');

    assert.deepStrictEqual(
      {
        event: emitted[0].event,
        domain: emitted[0].domain,
        level: emitted[0].data.level,
        message: emitted[0].data.message,
        timestamp: emitted[0].data.timestamp,
        taskId: emitted[0].extra.taskId
      },
      {
        event: 'organizer.log',
        domain: 'organizer',
        level: 'warn',
        message: 'log-message',
        timestamp: '2026-05-08T01:02:03.000Z',
        taskId: 'organizer-1'
      }
    );

    assert.strictEqual(emitted[1].event, 'organizer.state');
    assert.strictEqual(emitted[1].domain, 'organizer');
    assert.strictEqual(emitted[1].data.status, 'running');
    assert.strictEqual(emitted[1].data.message, 'state-message');
    assert.ok(typeof emitted[1].data.timestamp === 'string' && emitted[1].data.timestamp.length > 0);
    assert.strictEqual(emitted[1].extra.taskId, 'organizer-2');

    assert.deepStrictEqual(timed, {
      level: 'info',
      message: 'timed-message',
      timestamp: '2026-05-08T04:05:06.000Z'
    });
  });
});

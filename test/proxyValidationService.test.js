const assert = require('assert');

const { createProxyValidationService } = require('../desktop/mainServices/proxyValidationService');

describe('proxyValidationService', () => {
  it('returns empty status when the proxy input is blank', async () => {
    const service = createProxyValidationService();
    const result = await service.validateProxy('');

    assert.strictEqual(result.status, 'empty');
    assert.strictEqual(result.message, 'Proxy not set');
  });

  it('normalizes host:port proxy input to an http URL', () => {
    const service = createProxyValidationService();

    assert.strictEqual(service.normalizeProxyValue('127.0.0.1:7897'), 'http://127.0.0.1:7897');
  });

  it('returns invalid status when the proxy format is not valid', async () => {
    const service = createProxyValidationService();
    const result = await service.validateProxy('213');

    assert.strictEqual(result.status, 'invalid');
    assert.strictEqual(result.message, 'Proxy validation failed');
  });
});

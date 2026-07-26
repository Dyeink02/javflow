const assert = require('assert');

const RequestHandler = require('../dist/core/requestHandler').default;

describe('RequestHandler', () => {
  function createHandler(overrides = {}) {
    return new RequestHandler({
      retryCount: 1,
      retryDelay: 10,
      BASE_URL: 'https://www.javbus.com',
      baseUrl: 'https://www.javbus.com',
      parallel: 1,
      headers: {
        Referer: 'https://www.javbus.com',
        Cookie: ''
      },
      output: 'C:/temp',
      search: null,
      base: 'https://www.javbus.com',
      nomag: false,
      allmag: false,
      nopic: true,
      timeout: 30000,
      searchUrl: '',
      limit: 0,
      delay: 1,
      ...overrides
    });
  }

  it('returns null instead of throwing when ajax body has no magnet links', async () => {
    const handler = createHandler();
    handler.loadAntiBlockBaseOrigins = () => [];
    handler.getXMLHttpRequest = async () => ({
      statusCode: 200,
      body: '<html><body><div>empty result</div></body></html>'
    });

    const result = await handler.fetchMagnet({
      title: 'ABF-001',
      gid: '123456',
      uc: '0',
      img: '/images/cover.jpg',
      category: [],
      actress: []
    });

    assert.strictEqual(result, null);
  });

  it('normalizes absolute image url before building ajax request', async () => {
    const handler = createHandler();
    handler.loadAntiBlockBaseOrigins = () => [];
    let capturedUrl = '';

    handler.getXMLHttpRequest = async (url) => {
      capturedUrl = url;
      return {
        statusCode: 200,
        body: `
          <a href="magnet:?xt=urn:btih:ABCDEF1234567890&dn=ONE"></a>
          <span>1.50GB</span>
        `
      };
    };

    const result = await handler.fetchMagnet({
      title: 'ABF-002',
      gid: '654321',
      uc: '1',
      img: 'https://www.javbus.com/images/cover.jpg',
      category: [],
      actress: []
    });

    assert.ok(capturedUrl.includes('img=images%2Fcover.jpg'));
    assert.ok(result);
    assert.strictEqual(result.magnetLinks.length, 1);
  });

  it('prefers regular ajax even when cloudflare fallback is enabled', async () => {
    const handler = createHandler({
      useCloudflareBypass: true
    });
    handler.loadAntiBlockBaseOrigins = () => [];
    let cloudflareCalled = false;

    handler.getXMLHttpRequest = async () => ({
      statusCode: 200,
      body: `
        <a href="magnet:?xt=urn:btih:ABCDEF1234567890&dn=ONE"></a>
        <span>1.50GB</span>
      `
    });
    handler.executeAjaxWithCloudflare = async () => {
      cloudflareCalled = true;
      return null;
    };

    const result = await handler.fetchMagnet({
      title: 'ABF-003',
      gid: '777777',
      uc: '1',
      img: '/images/cover.jpg',
      category: [],
      actress: []
    });

    assert.ok(result);
    assert.strictEqual(cloudflareCalled, false);
    assert.strictEqual(result.magnetLinks.length, 1);
  });

  it('falls back to mirror ajax domains when primary domain is blocked', async () => {
    const handler = createHandler();
    handler.loadAntiBlockBaseOrigins = () => [];
    const calledUrls = [];

    handler.getXMLHttpRequest = async (url) => {
      calledUrls.push(url);

      if (url.includes('javbus.com/ajax/')) {
        throw new Error('Request failed with status code 403');
      }

      return {
        statusCode: 200,
        body: `
          <a href="magnet:?xt=urn:btih:ABCDEF1234567890&dn=ONE"></a>
          <span>1.50GB</span>
        `
      };
    };

    const result = await handler.fetchMagnet({
      title: 'ABF-004',
      gid: '888888',
      uc: '1',
      img: '/images/cover.jpg',
      category: [],
      actress: []
    });

    assert.ok(result);
    assert.ok(calledUrls[0].includes('javbus.com/ajax/'));
    assert.ok(calledUrls.some((url) => /busjav\.cyou\/ajax\/|fanbus\.bond\/ajax\/|cdnbus\.bond\/ajax\//.test(url)));
    assert.strictEqual(result.magnetLinks.length, 1);
  });

  it('prioritizes cached anti-block ajax mirrors before the official javbus domain', async () => {
    const handler = createHandler();
    handler.loadAntiBlockBaseOrigins = () => ['https://www.fanbus.cyou', 'https://www.busjav.cyou'];
    const calledUrls = [];

    handler.getXMLHttpRequest = async (url) => {
      calledUrls.push(url);
      return {
        statusCode: 200,
        body: `
          <a href="magnet:?xt=urn:btih:ABCDEF1234567890&dn=ONE"></a>
          <span>1.50GB</span>
        `
      };
    };

    const result = await handler.fetchMagnet({
      title: 'ABF-004A',
      gid: '888889',
      uc: '1',
      img: '/images/cover.jpg',
      category: [],
      actress: []
    });

    assert.ok(result);
    assert.ok(calledUrls[0].includes('fanbus.cyou/ajax/'));
    assert.strictEqual(handler.preferredAjaxBaseOrigin, 'https://www.fanbus.cyou');
    assert.strictEqual(result.magnetLinks.length, 1);
  });

  it('remembers the last successful ajax mirror domain', async () => {
    const handler = createHandler();
    handler.loadAntiBlockBaseOrigins = () => [];
    const calledUrls = [];

    handler.getXMLHttpRequest = async (url) => {
      calledUrls.push(url);

      if (url.includes('javbus.com/ajax/')) {
        throw new Error('Request failed with status code 403');
      }

      return {
        statusCode: 200,
        body: `
          <a href="magnet:?xt=urn:btih:ABCDEF1234567890&dn=ONE"></a>
          <span>1.50GB</span>
        `
      };
    };

    await handler.fetchMagnet({
      title: 'ABF-005',
      gid: '999999',
      uc: '1',
      img: '/images/cover.jpg',
      category: [],
      actress: []
    });

    calledUrls.length = 0;

    await handler.fetchMagnet({
      title: 'ABF-006',
      gid: '999998',
      uc: '1',
      img: '/images/cover2.jpg',
      category: [],
      actress: []
    });

    assert.ok(/busjav\.cyou\/ajax\/|fanbus\.bond\/ajax\/|cdnbus\.bond\/ajax\//.test(calledUrls[0]));
  });

  it('cools down repeatedly failing ajax origins and prioritizes healthier mirrors', () => {
    const handler = createHandler();
    handler.loadAntiBlockBaseOrigins = () => [];

    handler.recordAjaxBaseOriginFailure('https://www.javbus.com', 'timeout of 12000ms exceeded');
    handler.recordAjaxBaseOriginFailure('https://www.javbus.com', 'timeout of 12000ms exceeded');

    const healthState = handler.ajaxBaseOriginHealth.get('https://www.javbus.com');
    const rankedOrigins = handler.getAjaxBaseOrigins();

    assert.ok(healthState);
    assert.ok(healthState.cooldownUntil > Date.now());
    assert.notStrictEqual(rankedOrigins[0], 'https://www.javbus.com');
    assert.ok(rankedOrigins.length <= 4);
  });

  it('falls back to mirror page domains when the primary page domain is blocked', async () => {
    const handler = createHandler();
    handler.loadAntiBlockBaseOrigins = () => ['https://www.busjav.cyou', 'https://www.fanbus.bond'];
    const calledUrls = [];

    handler.requestPageViaHttp = async (url) => {
      calledUrls.push(url);
      if (url.includes('www.javbus.com')) {
        throw new Error('Request failed with status code 403');
      }

      return {
        statusCode: 200,
        body: '<html><body><div class="movie-box">ok</div></body></html>'
      };
    };

    const result = await handler.getPage('https://www.javbus.com/star/ws2');

    assert.ok(result);
    assert.ok(calledUrls[0].includes('www.busjav.cyou/star/ws2'));
    assert.strictEqual(handler.preferredPageBaseOrigin, 'https://www.busjav.cyou');
    assert.strictEqual(handler.preferredAjaxBaseOrigin, 'https://www.busjav.cyou');
  });

  it('limits page origin candidates to the healthiest top four entries', () => {
    const handler = createHandler();
    handler.loadAntiBlockBaseOrigins = () => [
      'https://www.busjav.cyou',
      'https://www.fanbus.bond',
      'https://www.cdnbus.bond',
      'https://www.dmmbus.cyou',
      'https://mirror.example.com'
    ];

    const origins = handler.getPageBaseOrigins('https://www.javbus.com/star/ws2');

    assert.ok(origins.length <= 4);
    assert.strictEqual(origins[0], 'https://www.busjav.cyou');
    assert.ok(origins.every((origin) => origin.startsWith('https://')));
  });

  it('keeps the official javbus page url as the last fallback when mirror candidates already fill the top slots', async () => {
    const handler = createHandler();
    handler.loadAntiBlockBaseOrigins = () => [
      'https://www.fanbus.cyou',
      'https://www.busdmm.bond',
      'https://www.javsee.cyou',
      'https://www.fanbus.bond',
      'https://www.busjav.cyou'
    ];
    const calledUrls = [];

    handler.requestPageViaHttp = async (url) => {
      calledUrls.push(url);
      return {
        statusCode: 200,
        body: '<html><body><div class="movie-box">ok</div></body></html>'
      };
    };

    const result = await handler.getPage('https://www.javbus.com/star/wc8');

    assert.ok(result);
    assert.ok(calledUrls[0].includes('fanbus.cyou/star/wc8'));
    assert.ok(calledUrls.every((url, index) => index === 0 || !url.includes('javbus.com/star/wc8') || index === calledUrls.length - 1));
  });

  it('filters excluded magnet keywords and falls back to the next largest magnet', async () => {
    const handler = createHandler({
      magnetExcludeKeywords: '-U'
    });
    handler.loadAntiBlockBaseOrigins = () => [];
    handler.getXMLHttpRequest = async () => ({
      statusCode: 200,
      body: `
        <a href="magnet:?xt=urn:btih:ABCDEF1234567890&dn=SONE-943-U"></a>
        <span>4.00GB</span>
        <a href="magnet:?xt=urn:btih:1234567890ABCDEF&dn=SONE-943"></a>
        <span>3.00GB</span>
      `
    });

    const result = await handler.fetchMagnet({
      title: 'SONE-943',
      gid: '123123',
      uc: '1',
      img: '/images/cover.jpg',
      category: [],
      actress: []
    });

    assert.ok(result);
    assert.strictEqual(result.magnetLinks.length, 1);
    assert.strictEqual(result.magnetLinks[0].link, 'magnet:?xt=urn:btih:1234567890ABCDEF&dn=SONE-943');
  });

  it('uses the next validated magnet when magnet content validation rejects the largest candidate', async () => {
    const handler = createHandler({
      magnetContentValidation: true
    });
    handler.loadAntiBlockBaseOrigins = () => [];
    handler.getXMLHttpRequest = async () => ({
      statusCode: 200,
      body: `
        <a href="magnet:?xt=urn:btih:ABCDEF1234567890&dn=SONE-943-AD"></a>
        <span>4.00GB</span>
        <a href="magnet:?xt=urn:btih:1234567890ABCDEF&dn=SONE-943"></a>
        <span>3.00GB</span>
      `
    });
    handler.filterMagnetCandidatesByContent = async (_title, candidates) => [candidates[1]];

    const result = await handler.fetchMagnet({
      title: 'SONE-943',
      gid: '123124',
      uc: '1',
      img: '/images/cover.jpg',
      category: [],
      actress: []
    });

    assert.ok(result);
    assert.strictEqual(result.magnetLinks.length, 1);
    assert.strictEqual(result.magnetLinks[0].link, 'magnet:?xt=urn:btih:1234567890ABCDEF&dn=SONE-943');
  });

  it('keeps the 0.20 fast path untouched when magnet content validation is disabled', async () => {
    const handler = createHandler({
      magnetContentValidation: false
    });
    const candidates = [
      {
        magnetLink: 'magnet:?xt=urn:btih:AAA&dn=FIRST',
        size: 100,
        displayName: 'FIRST'
      },
      {
        magnetLink: 'magnet:?xt=urn:btih:BBB&dn=SECOND',
        size: 200,
        displayName: 'SECOND'
      }
    ];

    handler.magnetValidationRuntime.disabled = true;
    handler.magnetValidationRuntime.cooldownUntil = Date.now() + 60_000;

    const result = await handler.filterMagnetCandidatesByContent('ABF-007', candidates);

    assert.strictEqual(result, candidates);
    assert.strictEqual(handler.magnetValidationRuntime.inspectedCandidates, 0);
    assert.strictEqual(handler.magnetValidationRuntime.disabled, true);
  });

  it('resets the temporary magnet validation cooldown after it expires', () => {
    const handler = createHandler({
      magnetContentValidation: true
    });
    const realNow = Date.now;
    let now = 1_000_000;
    Date.now = () => now;

    try {
      handler.magnetValidationCooldownMs = 5_000;
      handler.recordMagnetValidationStats('ABF-008', {
        totalCandidates: 1,
        validationApplied: true,
        validationSkippedReason: null,
        suspiciousCandidateCount: 1,
        inspectedCount: 8,
        acceptedCount: 0,
        rejectedCount: 0,
        unverifiedCount: 8,
        timeoutCount: 8,
        skippedCount: 0
      });

      assert.strictEqual(handler.magnetValidationRuntime.disabled, true);
      assert.strictEqual(handler.isMagnetValidationEnabled(), false);

      now += 5_001;

      assert.strictEqual(handler.isMagnetValidationEnabled(), true);
      assert.strictEqual(handler.magnetValidationRuntime.disabled, false);
      assert.strictEqual(handler.magnetValidationRuntime.cooldownUntil, 0);
      assert.strictEqual(handler.magnetValidationRuntime.inspectedCandidates, 0);
      assert.strictEqual(handler.magnetValidationRuntime.unverifiedCandidates, 0);
      assert.strictEqual(handler.magnetValidationRuntime.timeoutCandidates, 0);
    } finally {
      Date.now = realNow;
    }
  });

  it('initializes proxy pool from multiple proxy entries', () => {
    const handler = createHandler({
      proxy: 'http://127.0.0.1:9001\ninvalid-proxy\nhttp://127.0.0.1:9002'
    });

    assert.strictEqual(handler.proxyPool.length, 2);
    assert.strictEqual(handler.requestConfig.proxy, 'http://127.0.0.1:9001');
    assert.strictEqual(handler.config.proxy, 'http://127.0.0.1:9001');
  });

  it('drops invalid proxy values instead of repeatedly keeping a broken proxy config', () => {
    const handler = createHandler({
      proxy: '213'
    });

    assert.strictEqual(handler.proxyPool.length, 0);
    assert.strictEqual(handler.requestConfig.proxy, undefined);
    assert.strictEqual(handler.config.proxy, undefined);
  });

  it('normalizes host-port proxy values without requiring users to type the protocol', () => {
    const handler = createHandler({
      proxy: '127.0.0.1:9001'
    });

    assert.strictEqual(handler.proxyPool.length, 1);
    assert.strictEqual(handler.requestConfig.proxy, 'http://127.0.0.1:9001');
    assert.strictEqual(handler.config.proxy, 'http://127.0.0.1:9001');
  });
});

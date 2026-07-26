const assert = require('assert');

const { resolveImageDownloadTarget } = require('../dist/core/queueManagerImageUtils');

describe('queueManagerImageUtils', () => {
  it('rewrites relative image paths to the preferred mirror origin', () => {
    const target = resolveImageDownloadTarget({
      configuredBaseUrl: 'https://www.javbus.com/star/wc8',
      imageSource: '/pics/cover/c71g_b.jpg',
      preferredPageOrigin: 'https://www.fanbus.cyou'
    });

    assert.deepStrictEqual(target, {
      imageUrl: 'https://www.fanbus.cyou/pics/cover/c71g_b.jpg',
      refererUrl: 'https://www.fanbus.cyou/star/wc8',
      runtimeOrigin: 'https://www.fanbus.cyou'
    });
  });

  it('rewrites official javbus absolute image urls to the preferred mirror origin', () => {
    const target = resolveImageDownloadTarget({
      configuredBaseUrl: 'https://www.javbus.com/star/wc8',
      imageSource: 'https://www.javbus.com/pics/cover/c4fm_b.jpg',
      preferredAjaxOrigin: 'https://www.busjav.cyou'
    });

    assert.deepStrictEqual(target, {
      imageUrl: 'https://www.busjav.cyou/pics/cover/c4fm_b.jpg',
      refererUrl: 'https://www.busjav.cyou/star/wc8',
      runtimeOrigin: 'https://www.busjav.cyou'
    });
  });

  it('keeps third-party absolute image urls unchanged', () => {
    const target = resolveImageDownloadTarget({
      configuredBaseUrl: 'https://www.javbus.com/star/wc8',
      imageSource: 'https://cdn.example.com/poster.jpg',
      preferredPageOrigin: 'https://www.fanbus.cyou'
    });

    assert.deepStrictEqual(target, {
      imageUrl: 'https://cdn.example.com/poster.jpg',
      refererUrl: 'https://www.fanbus.cyou/star/wc8',
      runtimeOrigin: 'https://www.fanbus.cyou'
    });
  });
});

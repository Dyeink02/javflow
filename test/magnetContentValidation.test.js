const assert = require('assert');

const {
  classifyMagnetMetadataFiles,
  filterMagnetCandidatesByContent
} = require('../dist/core/magnetContentValidation');

describe('magnetContentValidation', () => {
  it('accepts magnets that mainly contain video, image and subtitle files', () => {
    const result = classifyMagnetMetadataFiles([
      { path: 'HMN-001/HMN-001.mp4', length: 4 * 1024 * 1024 * 1024 },
      { path: 'HMN-001/poster.jpg', length: 512 * 1024 },
      { path: 'HMN-001/subtitle.srt', length: 128 * 1024 }
    ]);

    assert.strictEqual(result.accepted, true);
    assert.ok(result.summary.includes('主视频'));
  });

  it('rejects magnets that contain dangerous ad or installer files', () => {
    const result = classifyMagnetMetadataFiles([
      { path: 'HMN-002/HMN-002.mp4', length: 3 * 1024 * 1024 * 1024 },
      { path: 'HMN-002/最新地址.url', length: 4 * 1024 },
      { path: 'HMN-002/poster.jpg', length: 256 * 1024 }
    ]);

    assert.strictEqual(result.accepted, false);
    assert.ok(result.reason.includes('广告/安装类文件'));
  });

  it('falls back to the first unverified candidate when no candidate passes validation', async () => {
    const candidates = [
      {
        magnetLink: 'magnet:?xt=urn:btih:AAA&dn=FIRST',
        size: 4096,
        displayName: 'FIRST'
      },
      {
        magnetLink: 'magnet:?xt=urn:btih:BBB&dn=SECOND',
        size: 3072,
        displayName: 'SECOND'
      }
    ];

    const kept = await filterMagnetCandidatesByContent({
      title: 'HMN-003',
      candidates,
      enabled: true,
      keepAll: false,
      inspectCandidate: async (candidate) => {
        if (candidate.displayName === 'FIRST') {
          return {
            candidate,
            status: 'rejected',
            reason: '检测到广告包',
            summary: '危险文件 1 个'
          };
        }

        return {
          candidate,
          status: 'unverified',
          reason: 'Timeout',
          summary: '读取超时'
        };
      }
    });

    assert.deepStrictEqual(kept, [candidates[1]]);
  });

  it('keeps the current candidate once validation becomes unverified instead of scanning deeper candidates', async () => {
    const candidates = [
      {
        magnetLink: 'magnet:?xt=urn:btih:AAA&dn=FIRST',
        size: 4096,
        displayName: 'FIRST'
      },
      {
        magnetLink: 'magnet:?xt=urn:btih:BBB&dn=SECOND',
        size: 3072,
        displayName: 'SECOND'
      }
    ];
    const inspected = [];

    const kept = await filterMagnetCandidatesByContent({
      title: 'HMN-004',
      candidates,
      enabled: true,
      keepAll: false,
      inspectCandidate: async (candidate) => {
        inspected.push(candidate.displayName);
        if (candidate.displayName === 'FIRST') {
          return {
            candidate,
            status: 'unverified',
            reason: 'Timeout',
            summary: '读取超时'
          };
        }

        return {
          candidate,
          status: 'accepted',
          reason: '',
          summary: '不应继续检查到这里'
        };
      }
    });

    assert.deepStrictEqual(kept, [candidates[0]]);
    assert.deepStrictEqual(inspected, ['FIRST']);
  });

  it('steps down to the next-largest candidate when the current largest candidate is rejected as ad content', async () => {
    const candidates = [
      {
        magnetLink: 'magnet:?xt=urn:btih:AAA&dn=atom336-fhd-mp4',
        size: 6.75 * 1024,
        displayName: 'atom336-fhd-mp4'
      },
      {
        magnetLink: 'magnet:?xt=urn:btih:BBB&dn=%5BThz%5DATOM-336',
        size: 6.46 * 1024,
        displayName: '[Thz]ATOM-336'
      }
    ];
    const inspected = [];

    const kept = await filterMagnetCandidatesByContent({
      title: 'ATOM-336',
      candidates,
      enabled: true,
      keepAll: false,
      inspectCandidate: async (candidate) => {
        inspected.push(candidate.displayName);
        if (candidate.displayName === 'atom336-fhd-mp4') {
          return {
            candidate,
            status: 'rejected',
            reason: '检测到广告包',
            summary: '危险文件 2 个'
          };
        }

        return {
          candidate,
          status: 'accepted',
          reason: '',
          summary: '主视频文件完整'
        };
      }
    });

    assert.deepStrictEqual(kept, [candidates[1]]);
    assert.deepStrictEqual(inspected, ['atom336-fhd-mp4', '[Thz]ATOM-336']);
  });
});

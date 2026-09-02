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

  it('rejects a torrent whose video files contain multiple film codes', () => {
    const result = classifyMagnetMetadataFiles(
      [
        { path: '140403 SNIS-129.avi', length: 1.4 * 1024 * 1024 * 1024 },
        { path: '130907 SOE-992.avi', length: 1.8 * 1024 * 1024 * 1024 },
        { path: '140807 SNIS-205.mkv', length: 1.6 * 1024 * 1024 * 1024 }
      ],
      'SNIS-205'
    );

    assert.strictEqual(result.accepted, false);
    assert.ok(result.reason.includes('多个不同番号'));
    assert.ok(result.summary.includes('SNIS-129'));
    assert.ok(result.summary.includes('SOE-992'));
  });

  it('accepts split files that belong to the same film code', () => {
    const result = classifyMagnetMetadataFiles(
      [
        { path: 'SNIS-147A.mkv', length: 1.6 * 1024 * 1024 * 1024 },
        { path: 'SNIS-147B.mkv', length: 1.5 * 1024 * 1024 * 1024 },
        { path: 'SNIS-147-COVER.jpg', length: 512 * 1024 }
      ],
      'SNIS-147'
    );

    assert.strictEqual(result.accepted, true);
  });

  it('rejects a smaller second video when it has a different film code', () => {
    const result = classifyMagnetMetadataFiles(
      [
        { path: 'SNIS-147.mkv', length: 2 * 1024 * 1024 * 1024 },
        { path: 'SOE-992.mp4', length: 8 * 1024 * 1024 }
      ],
      'SNIS-147'
    );

    assert.strictEqual(result.accepted, false);
    assert.ok(result.reason.includes('多个不同番号'));
  });

  it('rejects a verified torrent when its code does not match the target', () => {
    const result = classifyMagnetMetadataFiles(
      [{ path: 'SOE-992.avi', length: 1.6 * 1024 * 1024 * 1024 }],
      'SNIS-205'
    );

    assert.strictEqual(result.accepted, false);
    assert.ok(result.reason.includes('目标不符'));
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

  it('does not retain an unverified candidate when strict validation cannot confirm it', async () => {
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

    assert.deepStrictEqual(kept, []);
  });

  it('skips an unverified candidate and uses the next verified candidate', async () => {
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

    assert.deepStrictEqual(kept, [candidates[1]]);
    assert.deepStrictEqual(inspected, ['FIRST', 'SECOND']);
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

  it('keeps only verified candidates in all-magnet mode', async () => {
    const candidates = [
      {
        magnetLink: 'magnet:?xt=urn:btih:AAA&dn=VERIFIED',
        size: 4096,
        displayName: 'VERIFIED'
      },
      {
        magnetLink: 'magnet:?xt=urn:btih:BBB&dn=UNKNOWN',
        size: 3072,
        displayName: 'UNKNOWN'
      },
      {
        magnetLink: 'magnet:?xt=urn:btih:CCC&dn=OUTSIDE-WINDOW',
        size: 2048,
        displayName: 'OUTSIDE-WINDOW'
      }
    ];

    const kept = await filterMagnetCandidatesByContent({
      title: 'HMN-005',
      candidates,
      enabled: true,
      keepAll: true,
      maxInspectCount: 2,
      inspectCandidate: async (candidate) => {
        if (candidate.displayName === 'VERIFIED') {
          return {
            candidate,
            status: 'accepted',
            reason: '',
            summary: '主视频文件完整'
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

    assert.deepStrictEqual(kept, [candidates[0]]);
  });
});

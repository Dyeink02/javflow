const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');

const { createLogBridge } = require('../desktop/mainServices/logBridge');

describe('logBridge', () => {
  it('localizes log prefixes, levels and task states before writing logs', async () => {
    const sentEvents = [];
    const outputDir = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-log-bridge-'));
    const bridge = createLogBridge({
      fs,
      path,
      sendToRenderer(channel, payload) {
        sentEvents.push({ channel, payload });
      },
      mainText: {
        taskLogTitleSuffix: '任务日志',
        versionLabel: '版本',
        startTimeLabel: '开始时间',
        outputLabel: '输出目录',
        baseLabel: '起始地址',
        runtimeSchemeLabel: '运行方案',
        keyLogPrefix: '[重点] ',
        stateLogPrefix: '状态',
        logLevelLabels: {
          info: '信息',
          warn: '警告',
          error: '错误'
        },
        logPrefixLabels: {
          'QueueManager:': '队列管理：'
        }
      },
      statusLabels: {
        running: '运行中'
      },
      logFilterPatterns: {
        noisy: [],
        key: ['抓取任务已完成']
      },
      fileNames: {
        taskLogPrefix: '运行日志',
        latestLogFilename: 'latest-log.txt'
      },
      appTitle: 'JavFlow',
      appVersion: '1.1.18',
      appDemoLabel: 'AED'
    });

    bridge.initializeTaskLogFiles(outputDir, {
      base: 'https://www.javbus.com',
      demoLabel: 'AED'
    });
    bridge.appendTaskLogEntry({
      level: 'warn',
      message: 'QueueManager: 发现分页缺口',
      timestamp: '2026-04-11T15:00:00.000Z'
    });
    bridge.appendTaskStateEntry({
      status: 'running',
      message: '抓取任务已完成'
    });
    await bridge.flushDesktopPipelines();

    const logDir = path.join(outputDir, 'logs');
    const logFile = fs
      .readdirSync(logDir)
      .find((filename) => filename.startsWith('运行日志-') && filename.endsWith('.txt'));
    const content = fs.readFileSync(path.join(logDir, logFile), 'utf8');

    assert.ok(content.includes('警告: 队列管理：发现分页缺口'));
    assert.ok(content.includes('状态(运行中): [重点] 抓取任务已完成'));
    assert.ok(sentEvents.some((event) => event.channel === 'runner:log-context'));
  });
});

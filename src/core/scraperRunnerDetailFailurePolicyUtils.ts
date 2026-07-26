import type { FailedDetailRecord } from './taskStateManager';
import type { DetailFailurePolicy } from './scraperRunnerTypes';

export function classifyDetailFailure(reason: string): DetailFailurePolicy {
  const normalizedReason = String(reason || '').toLowerCase();

  if (
    normalizedReason.includes('request cancelled') ||
    normalizedReason.includes('任务已终止') ||
    normalizedReason.includes('用户主动终止')
  ) {
    return {
      key: 'stopped',
      label: '任务终止',
      maxRetries: 0,
      priority: 99,
      advice: '任务已被终止，本轮不会继续重试。'
    };
  }

  if (
    normalizedReason.includes('cloudflare') ||
    normalizedReason.includes('challenge') ||
    normalizedReason.includes('driver-verify') ||
    normalizedReason.includes('age verification') ||
    normalizedReason.includes('验证页') ||
    normalizedReason.includes('403') ||
    normalizedReason.includes('429')
  ) {
    return {
      key: 'blocked',
      label: '验证拦截',
      maxRetries: 5,
      priority: 0,
      advice: '建议开启 Cloudflare、切换备用网址或检查代理后再重试。'
    };
  }

  if (
    normalizedReason.includes('timed out') ||
    normalizedReason.includes('timeout') ||
    normalizedReason.includes('econnreset') ||
    normalizedReason.includes('enotfound') ||
    normalizedReason.includes('err_connection') ||
    normalizedReason.includes('socket hang up') ||
    normalizedReason.includes('proxy')
  ) {
    return {
      key: 'network',
      label: '网络超时',
      maxRetries: 4,
      priority: 1,
      advice: '建议检查网络、代理或稍后再次补爬。'
    };
  }

  if (
    normalizedReason.includes('响应为空') ||
    normalizedReason.includes('empty') ||
    normalizedReason.includes('页面响应为空') ||
    normalizedReason.includes('返回空')
  ) {
    return {
      key: 'empty',
      label: '页面空响应',
      maxRetries: 3,
      priority: 2,
      advice: '建议重新抓取该详情页，必要时切换域名后补爬。'
    };
  }

  if (
    normalizedReason.includes('parse') ||
    normalizedReason.includes('metadata') ||
    normalizedReason.includes('script') ||
    normalizedReason.includes('cannot read') ||
    normalizedReason.includes('undefined') ||
    normalizedReason.includes('null')
  ) {
    return {
      key: 'parse',
      label: '页面解析失败',
      maxRetries: 3,
      priority: 3,
      advice: '建议稍后重试，若持续失败需检查站点页面结构是否变化。'
    };
  }

  return {
    key: 'unknown',
    label: '未知失败',
    maxRetries: 2,
    priority: 4,
    advice: '建议查看日志后再次补爬。'
  };
}

export function getDetailRecoveryBudget(policy: DetailFailurePolicy, isLargeTaskMode: boolean): number {
  if (policy.key === 'stopped') {
    return 0;
  }

  const baseBudget = isLargeTaskMode ? 2 : 3;
  if (policy.key === 'blocked') {
    return Math.min(policy.maxRetries, baseBudget + 1);
  }

  return Math.min(Math.max(policy.maxRetries, 1), baseBudget);
}

export function buildRecoveryCategorySummary(
  links: string[],
  getReasonForLink: (link: string) => string
): string {
  const summaryMap = new Map<string, number>();

  for (const link of links) {
    const policy = classifyDetailFailure(getReasonForLink(link));
    summaryMap.set(policy.label, (summaryMap.get(policy.label) || 0) + 1);
  }

  return Array.from(summaryMap.entries())
    .map(([label, count]) => `${label} ${count} 条`)
    .join('，');
}

export function getRecoverableMissingDetailLinks(params: {
  links: string[];
  failedDetailMap: Map<string, FailedDetailRecord>;
  detailRecoveryAttemptMap: Map<string, number>;
  getPolicy: (reason: string) => DetailFailurePolicy;
  getRecoveryBudget: (policy: DetailFailurePolicy) => number;
}): string[] {
  const { links, failedDetailMap, detailRecoveryAttemptMap, getPolicy, getRecoveryBudget } = params;

  return [...links]
    .map((link) => {
      const record = failedDetailMap.get(link);
      const policy = getPolicy(record?.reason || '');
      const retryCount = record?.retryCount || 0;
      const retriesRemaining = Math.max(policy.maxRetries - retryCount, 0);
      const recoveryAttempts = detailRecoveryAttemptMap.get(link) || 0;
      const totalRecoveryBudget = getRecoveryBudget(policy);

      return {
        link,
        policy,
        retryCount,
        retriesRemaining,
        recoveryAttempts,
        totalRecoveryBudget,
        hasFailureRecord: failedDetailMap.has(link)
      };
    })
    .filter(
      (item) =>
        (item.retriesRemaining > 0 || !item.hasFailureRecord) &&
        item.recoveryAttempts < item.totalRecoveryBudget
    )
    .sort((left, right) => {
      if (left.policy.priority !== right.policy.priority) {
        return left.policy.priority - right.policy.priority;
      }

      if (left.recoveryAttempts !== right.recoveryAttempts) {
        return left.recoveryAttempts - right.recoveryAttempts;
      }

      if (left.retryCount !== right.retryCount) {
        return left.retryCount - right.retryCount;
      }

      return left.link.localeCompare(right.link, 'zh-CN');
    })
    .map((item) => item.link);
}

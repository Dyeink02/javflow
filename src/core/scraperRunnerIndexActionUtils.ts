export interface IndexProcessingActionPlanInput {
  action: string;
  shouldStopIndexing: boolean;
  currentPage: number;
  targetTotalPages: number;
}

export interface IndexProcessingActionPlan {
  shouldStopIndexing: boolean;
  shouldContinueCurrentLoop: boolean;
  nextPageNumber: number;
  logMessage: string;
  shouldPersistState: boolean;
  stateReason: string;
  stateMessage: string;
}

export function resolveIndexProcessingActionPlan(
  input: IndexProcessingActionPlanInput
): IndexProcessingActionPlan {
  switch (input.action) {
    case 'continue_after_gap':
      return {
        shouldStopIndexing: input.shouldStopIndexing,
        shouldContinueCurrentLoop: true,
        nextPageNumber: input.currentPage + 1,
        logMessage: `第 ${input.currentPage} 页当前未解析到影片链接，但目标共 ${input.targetTotalPages} 页。已记录为分页缺口，继续尝试后续页面。`,
        shouldPersistState: true,
        stateReason: '索引页存在分页缺口',
        stateMessage: `第 ${input.currentPage} 页当前未解析到影片链接，继续尝试后续页面。`
      };
    case 'stop_empty_page':
      return {
        shouldStopIndexing: input.shouldStopIndexing,
        shouldContinueCurrentLoop: true,
        nextPageNumber: input.currentPage,
        logMessage: `第 ${input.currentPage} 页为空，停止继续抓取索引页。`,
        shouldPersistState: true,
        stateReason: '索引页为空，停止继续抓取',
        stateMessage: `第 ${input.currentPage} 页为空，停止继续抓取索引页。`
      };
    case 'continue_resume_completed_page':
      return {
        shouldStopIndexing: input.shouldStopIndexing,
        shouldContinueCurrentLoop: true,
        nextPageNumber: input.currentPage + 1,
        logMessage: `第 ${input.currentPage} 页均为已完成内容，继续检查后续页面是否存在漏抓项目。`,
        shouldPersistState: true,
        stateReason: '恢复模式继续检查下一页',
        stateMessage: '当前页均为已完成内容。'
      };
    case 'stop_no_new_links':
      return {
        shouldStopIndexing: input.shouldStopIndexing,
        shouldContinueCurrentLoop: true,
        nextPageNumber: input.currentPage,
        logMessage: `第 ${input.currentPage} 页没有新链接，停止继续抓取索引页。`,
        shouldPersistState: true,
        stateReason: '索引页无新链接',
        stateMessage: `第 ${input.currentPage} 页没有新链接。`
      };
    case 'stop_limit_reached':
      return {
        shouldStopIndexing: input.shouldStopIndexing,
        shouldContinueCurrentLoop: false,
        nextPageNumber: input.currentPage,
        logMessage: '已达到目标抓取数量上限，当前页面处理完成后将结束索引页扫描。',
        shouldPersistState: false,
        stateReason: '',
        stateMessage: ''
      };
    case 'stop_target_page_reached':
      return {
        shouldStopIndexing: input.shouldStopIndexing,
        shouldContinueCurrentLoop: false,
        nextPageNumber: input.currentPage,
        logMessage: `已到达配置或推算的最后一页：${input.targetTotalPages}。`,
        shouldPersistState: false,
        stateReason: '',
        stateMessage: ''
      };
    default:
      return {
        shouldStopIndexing: input.shouldStopIndexing,
        shouldContinueCurrentLoop: false,
        nextPageNumber: input.currentPage,
        logMessage: '',
        shouldPersistState: false,
        stateReason: '',
        stateMessage: ''
      };
  }
}

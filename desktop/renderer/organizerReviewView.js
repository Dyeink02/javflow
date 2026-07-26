// Organizer review view owns report-path rows and post-run review panel DOM.
// The controller passes in callbacks so business actions stay outside the view.
//
// Ownership summary:
// 1) render organizer report-file rows
// 2) render post-run review entry points from normalized result payloads
// 3) expose passive open-report bindings without owning organizer decisions
//
// File map for maintainers:
// 1) report-file row render helpers
// 2) organizer review row normalization
// 3) review panel/card DOM render helpers
(function initializeOrganizerReviewView(globalScope) {
  const rendererHelpers = globalScope.desktopRendererHelpers || {};
  const clearChildren = rendererHelpers.clearChildren;

  if (!clearChildren) {
    throw new Error('desktopRendererHelpers must be loaded before organizerReviewView');
  }

  function renderReportFiles(container, reportFiles = [], callbacks = {}) {
    if (!container) {
      return;
    }

    clearChildren(container);

    if (!Array.isArray(reportFiles) || reportFiles.length === 0) {
      const empty = document.createElement('p');
      empty.className = 'organizer-report-item';
      empty.textContent = '预览模式下不会生成报告文件。';
      container.appendChild(empty);
      return;
    }

    reportFiles.forEach((filePath) => {
      const row = document.createElement('button');
      row.type = 'button';
      row.className = 'organizer-report-item';
      row.textContent = filePath;
      if (typeof callbacks.onBindOpenReport === 'function') {
        callbacks.onBindOpenReport(row, filePath);
      }
      container.appendChild(row);
    });
  }

  function buildReviewRows(result) {
    const safeResult = result && typeof result === 'object' ? result : null;
    if (!safeResult) {
      return null;
    }

    const reportMap = safeResult.reportMap && typeof safeResult.reportMap === 'object' ? safeResult.reportMap : {};
    const missingDownload =
      safeResult.missingDownload && typeof safeResult.missingDownload === 'object' ? safeResult.missingDownload : {};
    const adRisk = safeResult.adRisk && typeof safeResult.adRisk === 'object' ? safeResult.adRisk : {};

    return [
      {
        title: `遗漏番号：${Number(missingDownload.missingCodeCount || 0)} 条`,
        meta: `补抓磁力：${Number(missingDownload.missingMagnetCount || 0)} 条`,
        reportPath: reportMap.missingMagnets || ''
      },
      {
        title: `含开头广告番号：${Number(adRisk.riskCodeCount || 0)} 条`,
        meta: `补抓磁力：${Number(adRisk.supplementMagnetCount || 0)} 条`,
        reportPath: reportMap.adRiskMagnets || ''
      },
      {
        title: '误判复核入口',
        meta: '打开“含开头广告明细”进行人工复核，并回灌样本。',
        reportPath: reportMap.adRiskDetail || ''
      }
    ];
  }

  // Review view stays report/result driven. Any interpretation of what counts
  // as missing-download or ad-risk business state must happen before data
  // reaches this view.
  function renderReviewPanel(container, result = null, callbacks = {}) {
    if (!container) {
      return;
    }

    clearChildren(container);

    const rows = buildReviewRows(result);
    if (!rows) {
      const empty = document.createElement('p');
      empty.className = 'organizer-review-empty';
      empty.textContent = '整理完成后，这里会显示：遗漏番号、含开头广告补抓、误判复核入口。';
      container.appendChild(empty);
      return;
    }

    rows.forEach((row) => {
      const rowNode = document.createElement('div');
      rowNode.className = 'organizer-review-row';

      const content = document.createElement('div');
      const title = document.createElement('strong');
      title.textContent = row.title;
      content.appendChild(title);

      const meta = document.createElement('p');
      meta.className = 'organizer-review-meta';
      meta.textContent = row.meta;
      content.appendChild(meta);
      rowNode.appendChild(content);

      if (row.reportPath) {
        const openButton = document.createElement('button');
        openButton.type = 'button';
        openButton.className = 'ghost-button';
        openButton.textContent = '打开报告';
        if (typeof callbacks.onBindOpenReport === 'function') {
          callbacks.onBindOpenReport(openButton, row.reportPath);
        }
        rowNode.appendChild(openButton);
      }

      container.appendChild(rowNode);
    });
  }

  globalScope.desktopOrganizerReviewView = {
    renderReportFiles,
    renderReviewPanel
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);

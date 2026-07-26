package crawlquality

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, filePath string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(filePath), err)
	}
	if err := os.WriteFile(filePath, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", filePath, err)
	}
}

func TestSummarizeCompletedRun(t *testing.T) {
	outputDir := t.TempDir()
	writeTestFile(t, filepath.Join(outputDir, defaultMagnetName), "magnet:?xt=urn:btih:AAA&dn=ABC-001\r\nmagnet:?xt=urn:btih:BBB&dn=ABC-002\r\n")
	writeTestFile(t, filepath.Join(outputDir, defaultFilmDataName), `[
  {"title":"ABC-001 title","magnetLinks":[{"link":"magnet:?xt=urn:btih:AAA"}]},
  {"sourceLink":"https://example.test/ABC-002","backupMagnetLinks":[{"link":"magnet:?xt=urn:btih:BBB"}]}
]`)
	writeTestFile(t, filepath.Join(outputDir, defaultLogDirName, defaultLatestLog), `JAV 自动化爬虫工具任务日志
开始时间: 2026-04-30 13:06:06
起始地址: https://www.javbus.com/star/test
运行方案: AED
"limit": 2,
"totalPages": 1,
[2026-04-30 13:06:10] 信息: 成功获取磁力链接 ABC-001
[2026-04-30 13:06:10] 信息: 返回最大磁力链接 magnet:?xt=urn:btih:AAA
[2026-04-30 13:06:11] 信息: 成功获取磁力链接 ABC-002
[2026-04-30 13:06:11] 信息: 返回最大磁力链接 magnet:?xt=urn:btih:BBB
[2026-04-30 13:06:12] 信息: 结果二次校验通过：输出结果内部一致性正常
[2026-04-30 13:06:12] 信息: [重点] 抓取任务完成，总耗时 6 秒。`)

	summary, err := NewService().Summarize(Options{OutputDir: outputDir, WriteReport: true})
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}

	if summary.Status != "ok" {
		t.Fatalf("expected ok status, got %s with issues %#v", summary.Status, summary.Issues)
	}
	if summary.NoticeLevel != "info" {
		t.Fatalf("expected info notice level, got %s", summary.NoticeLevel)
	}
	if summary.MagnetTotal != 2 || summary.MagnetUnique != 2 {
		t.Fatalf("unexpected magnet metrics: total=%d unique=%d", summary.MagnetTotal, summary.MagnetUnique)
	}
	if summary.FilmRecordTotal != 2 || summary.FilmRecordWithMagnet != 2 || summary.FilmCodeUnique != 2 {
		t.Fatalf("unexpected film metrics: %#v", summary)
	}
	if !summary.SecondValidationPassed || !summary.Completed {
		t.Fatalf("expected completed validated run: %#v", summary)
	}
	if summary.DurationSeconds != 6 {
		t.Fatalf("expected duration 6, got %d", summary.DurationSeconds)
	}
	if summary.ReportPath == "" {
		t.Fatalf("expected report path")
	}
	if !strings.Contains(summary.SummaryLine, "输出质量正常") {
		t.Fatalf("unexpected summary line: %s", summary.SummaryLine)
	}
	if _, err := os.Stat(summary.ReportPath); err != nil {
		t.Fatalf("expected report file: %v", err)
	}
}

func TestSummarizeDetectsLatestLogCompletionWithoutExplicitLimitHeader(t *testing.T) {
	outputDir := t.TempDir()
	writeTestFile(t, filepath.Join(outputDir, defaultMagnetName), "magnet:?xt=urn:btih:AAA&dn=ABC-001\r\nmagnet:?xt=urn:btih:BBB&dn=ABC-002\r\n")
	writeTestFile(t, filepath.Join(outputDir, defaultFilmDataName), `[
  {"title":"ABC-001","magnetLinks":[{"link":"magnet:?xt=urn:btih:AAA"}]},
  {"title":"ABC-002","magnetLinks":[{"link":"magnet:?xt=urn:btih:BBB"}]}
]`)
	writeTestFile(t, filepath.Join(outputDir, defaultLogDirName, defaultLatestLog), `JavFlow任务日志
开始时间: 2026-05-01 07:31:06
起始地址: https://www.javbus.com/star/8ec
运行方案: AED
[2026-05-01 07:33:58] 状态: 结果已写入，当前已完成 2 条。
[2026-05-01 07:33:59] 状态: [重点] 抓取任务已完成，已二次校验完成。`)

	summary, err := NewService().Summarize(Options{OutputDir: outputDir, WriteReport: true})
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}

	if !summary.Completed {
		t.Fatalf("expected completed summary")
	}
	if !summary.SecondValidationPassed {
		t.Fatalf("expected second validation to pass")
	}
	if summary.TargetLimit != 2 {
		t.Fatalf("expected inferred target limit 2, got %d", summary.TargetLimit)
	}
	if summary.DurationSeconds <= 0 {
		t.Fatalf("expected computed duration from timestamps, got %d", summary.DurationSeconds)
	}
	if summary.Status != "ok" {
		t.Fatalf("expected ok status, got %s with issues %#v", summary.Status, summary.Issues)
	}
}

func TestSummarizeWarnsWhenOutputBelowTarget(t *testing.T) {
	outputDir := t.TempDir()
	writeTestFile(t, filepath.Join(outputDir, defaultMagnetName), "magnet:?xt=urn:btih:AAA&dn=ABC-001\r\n")
	writeTestFile(t, filepath.Join(outputDir, defaultFilmDataName), `[{"title":"ABC-001","magnetLinks":[{"link":"magnet:?xt=urn:btih:AAA"}]}]`)
	writeTestFile(t, filepath.Join(outputDir, defaultLogDirName, defaultLatestLog), `"limit": 3,
[2026-04-30 13:06:20] 警告: 磁力内容校验未完成，保留当前候选；原因：Timeout
[2026-04-30 13:06:21] 错误: 响应状态码: 429
[2026-04-30 13:06:22] 信息: [重点] 抓取任务完成，总耗时 3 秒。`)

	summary, err := NewService().Summarize(Options{OutputDir: outputDir})
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}

	if summary.Status != "warning" {
		t.Fatalf("expected warning status, got %s with issues %#v", summary.Status, summary.Issues)
	}
	if summary.HTTP429Count != 1 {
		t.Fatalf("expected one 429 line, got %d", summary.HTTP429Count)
	}
	if summary.ValidationTimeoutCount != 1 {
		t.Fatalf("expected one validation timeout, got %d", summary.ValidationTimeoutCount)
	}
	if len(summary.Issues) == 0 {
		t.Fatalf("expected issues")
	}
}

func TestSummarizePrefersFinalSummaryDuration(t *testing.T) {
	outputDir := t.TempDir()
	writeTestFile(t, filepath.Join(outputDir, defaultMagnetName), "magnet:?xt=urn:btih:AAA&dn=ABC-001\r\n")
	writeTestFile(t, filepath.Join(outputDir, defaultFilmDataName), `[{"title":"ABC-001","magnetLinks":[{"link":"magnet:?xt=urn:btih:AAA"}]}]`)
	writeTestFile(t, filepath.Join(outputDir, defaultLogDirName, defaultLatestLog), `开始时间: 2026-05-04 12:21:34
[2026-05-04 12:21:40] 信息: 处理中间步骤，总耗时 6 秒。
[2026-05-04 12:24:39] 信息: [重点] 抓取任务完成，总耗时 185 秒。
`)

	summary, err := NewService().Summarize(Options{OutputDir: outputDir})
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}

	if summary.DurationSeconds != 185 {
		t.Fatalf("expected final summary duration 185, got %d", summary.DurationSeconds)
	}
}

func TestSummarizeCanInferOutputDirFromLogDir(t *testing.T) {
	outputDir := t.TempDir()
	logDir := filepath.Join(outputDir, defaultLogDirName)
	writeTestFile(t, filepath.Join(outputDir, defaultMagnetName), "magnet:?xt=urn:btih:AAA\r\n")
	writeTestFile(t, filepath.Join(outputDir, defaultFilmDataName), `[{"title":"ABC-001","magnetLinks":["magnet:?xt=urn:btih:AAA"]}]`)
	writeTestFile(t, filepath.Join(logDir, defaultLatestLog), `[2026-04-30 13:06:22] 信息: [重点] 抓取任务完成，总耗时 3 秒。`)

	summary, err := NewService().Summarize(Options{LogDir: logDir})
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}

	if summary.OutputDir != outputDir {
		t.Fatalf("expected output dir %s, got %s", outputDir, summary.OutputDir)
	}
	if summary.MagnetTotal != 1 {
		t.Fatalf("expected one magnet, got %d", summary.MagnetTotal)
	}
}

func TestSummarizeSeparatesStoppedRunWithoutOutput(t *testing.T) {
	outputDir := t.TempDir()
	logDir := filepath.Join(outputDir, defaultLogDirName)
	writeTestFile(t, filepath.Join(logDir, defaultLatestLog), `开始时间: 2026-04-30 14:24:35
[2026-04-30 14:25:00] 状态: 正在终止任务并中断当前进度...
[2026-04-30 14:25:00] 警告: [重点] 已发送终止指令，正在中断队列与请求...
[2026-04-30 14:25:04] 错误: [重点] 绕过 Cloudflare 失败: Navigation timeout of 30000 ms exceeded
`)

	summary, err := NewService().Summarize(Options{OutputDir: outputDir, WriteReport: true})
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}

	if summary.Status != "stopped-empty" {
		t.Fatalf("expected stopped-empty status, got %s with issues %#v", summary.Status, summary.Issues)
	}
	if summary.NoticeLevel != "warn" {
		t.Fatalf("expected warn notice level, got %s", summary.NoticeLevel)
	}
	if summary.StatusText != "任务已终止且未产生有效输出" {
		t.Fatalf("unexpected status text: %s", summary.StatusText)
	}
	if !summary.StopRequested || !summary.StoppedWithoutOutput {
		t.Fatalf("expected stopRequested and stoppedWithoutOutput, got %#v", summary)
	}
	if summary.Completed {
		t.Fatalf("expected stopped run to be incomplete")
	}
	reportContent, err := os.ReadFile(summary.ReportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !strings.Contains(string(reportContent), "状态：任务已终止且未产生有效输出") {
		t.Fatalf("expected stopped status in report, got %s", string(reportContent))
	}
}

func TestSummarizeSeparatesStoppedRunWithPartialOutput(t *testing.T) {
	outputDir := t.TempDir()
	writeTestFile(t, filepath.Join(outputDir, defaultMagnetName), "magnet:?xt=urn:btih:AAA&dn=ABC-001\r\n")
	writeTestFile(t, filepath.Join(outputDir, defaultFilmDataName), `[{"title":"ABC-001","magnetLinks":[{"link":"magnet:?xt=urn:btih:AAA"}]}]`)
	writeTestFile(t, filepath.Join(outputDir, defaultLogDirName, defaultLatestLog), `"limit": 3,
[2026-04-30 14:25:00] 状态: 正在终止任务并中断当前进度...
[2026-04-30 14:25:00] 警告: [重点] 已发送终止指令，正在中断队列与请求...
[2026-04-30 14:25:01] 信息: 成功获取磁力链接 ABC-001
`)

	summary, err := NewService().Summarize(Options{OutputDir: outputDir, WriteReport: true})
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}

	if summary.Status != "stopped-partial" {
		t.Fatalf("expected stopped-partial status, got %s with issues %#v", summary.Status, summary.Issues)
	}
	if summary.NoticeLevel != "warn" {
		t.Fatalf("expected warn notice level, got %s", summary.NoticeLevel)
	}
	if summary.StatusText != "任务已终止，但已保留部分输出" {
		t.Fatalf("unexpected status text: %s", summary.StatusText)
	}
	if !summary.StopRequested || !summary.StoppedWithPartialOutput {
		t.Fatalf("expected stopRequested and stoppedWithPartialOutput, got %#v", summary)
	}
	if len(summary.TopSuggestionLines) == 0 {
		t.Fatalf("expected top suggestion lines")
	}
	if summary.Completed {
		t.Fatalf("expected partial stopped run to be incomplete")
	}
}

func TestSummarizeTreatsActressFilteringAsExpectedTxtReduction(t *testing.T) {
	outputDir := t.TempDir()
	writeTestFile(t, filepath.Join(outputDir, defaultMagnetName), "magnet:?xt=urn:btih:BBB&dn=BBB-002\r\n")
	writeTestFile(t, filepath.Join(outputDir, defaultFilmDataName), `[
  {
    "title":"AAA-001",
    "sourceLink":"https://example.test/AAA-001",
    "filteredByActressCount":true,
    "actressCount":6,
    "magnetLinks":[{"link":"magnet:?xt=urn:btih:AAA"}]
  },
  {
    "title":"BBB-002",
    "sourceLink":"https://example.test/BBB-002",
    "filteredByActressCount":false,
    "actressCount":2,
    "magnetLinks":[{"link":"magnet:?xt=urn:btih:BBB"}]
  }
]`)
	writeTestFile(t, filepath.Join(outputDir, defaultUnfinishedName), `# 任务状态：已完成
# 已完成：2
# 目标条数：2
# 站点原始条目：2
# 站点唯一番号：2
# 站点重复条目：0
`)
	writeTestFile(t, filepath.Join(outputDir, defaultLogDirName, defaultLatestLog), `"limit": 2,
[2026-05-03 10:00:00] 信息: second validation passed
[2026-05-03 10:00:01] 信息: crawl completed
`)

	summary, err := NewService().Summarize(Options{OutputDir: outputDir})
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}

	if summary.FilmRecordWithMagnet != 2 {
		t.Fatalf("expected 2 film records with magnet, got %d", summary.FilmRecordWithMagnet)
	}
	if summary.FilmRecordFilteredByActressCount != 1 {
		t.Fatalf("expected 1 actress-filtered record, got %d", summary.FilmRecordFilteredByActressCount)
	}
	if summary.ExpectedMagnetTxtTotal != 1 {
		t.Fatalf("expected TXT output total 1 after filtering, got %d", summary.ExpectedMagnetTxtTotal)
	}
	if summary.MagnetTotal != 1 {
		t.Fatalf("expected actual TXT magnet count 1, got %d", summary.MagnetTotal)
	}
	if len(summary.FilteredItemIDs) != 1 || summary.FilteredItemIDs[0] != "AAA-001" {
		t.Fatalf("unexpected filtered IDs: %#v", summary.FilteredItemIDs)
	}
	if !strings.Contains(summary.SummaryLine, "TXT 导出 1 = 2 - 1") {
		t.Fatalf("expected summary line to explain TXT export equation, got %s", summary.SummaryLine)
	}
	for _, issue := range summary.Issues {
		if strings.Contains(issue.Message, "TXT 实际输出 1 条，但按 filmData 统计应输出") {
			t.Fatalf("did not expect mismatch warning when TXT count matches expected filtered result: %#v", summary.Issues)
		}
	}
	foundEquation := false
	for _, issue := range summary.Issues {
		if strings.Contains(issue.Message, "TXT 导出 1 = 2 - 1") {
			foundEquation = true
			break
		}
	}
	if !foundEquation {
		t.Fatalf("expected issue list to explain TXT export equation, got %#v", summary.Issues)
	}
}

func TestSummarizeRealCaseJiecheng26(t *testing.T) {
	if os.Getenv("CRAWLQUALITY_REAL_CASE") == "" {
		t.Skip("set CRAWLQUALITY_REAL_CASE=1 to run against local real output")
	}

	outputDir := `C:\Users\<username>\Desktop\磁力链接\結城りの26`
	summary, err := NewService().Summarize(Options{OutputDir: outputDir, WriteReport: true})
	if err != nil {
		t.Fatalf("summarize real case: %v", err)
	}

	t.Logf("status=%s statusText=%s", summary.Status, summary.StatusText)
	t.Logf("target=%d crawled=%d siteRaw=%d siteDup=%d gap=%d", summary.TargetLimit, summary.CrawledUniqueCount, summary.SiteRawEntryCount, summary.SiteDuplicateEntryCount, summary.UniqueTargetShortfallCount)
	t.Logf("filmWithMagnet=%d filtered=%d expectedTxt=%d magnetTxt=%d", summary.FilmRecordWithMagnet, summary.FilmRecordFilteredByActressCount, summary.ExpectedMagnetTxtTotal, summary.MagnetTotal)
	t.Logf("filteredPreview=%v", func() []string {
		if len(summary.FilteredItemIDs) > 12 {
			return summary.FilteredItemIDs[:12]
		}
		return summary.FilteredItemIDs
	}())
	t.Logf("summaryLine=%s", summary.SummaryLine)
}

// Ownership summary:
//   This file implements domain logic for the javflow backend.
//
// File map for maintainers:
//   1) Domain-specific types and helpers.
//   2) Internal service implementation.
//

package subcrawl

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"javflow/internal/avsubscription"
	"javflow/internal/contracts/crawlartifact"
	"javflow/internal/crawlidentity"
	"javflow/internal/crawlindex"
	"javflow/internal/crawloutput"
	"javflow/internal/crawlparse"
	"javflow/internal/crawlrequest"
	"javflow/internal/events"
)

func (t *CrawlTask) Run(bus *events.Bus, subscriptions *avsubscription.Service) {
	t.mu.Lock()
	t.running = true
	t.mu.Unlock()

	defer func() {
		t.mu.Lock()
		t.running = false
		t.mu.Unlock()
	}()

	for i, req := range t.requests {
		if t.ctx.Err() != nil {
			t.setStatus(CrawlStatus{Phase: "stopped", Status: "stopped", Message: "用户已停止订阅抓取"})
			t.emitState(bus)
			return
		}

		t.mu.Lock()
		t.status.BatchCompleted = i
		t.status.ActressName = req.ActressName
		t.mu.Unlock()

		t.runSingle(bus, subscriptions, req)
	}

	t.mu.Lock()
	t.status.BatchCompleted = len(t.requests)
	t.status.Phase = "completed"
	t.status.Status = "completed"
	t.status.Message = fmt.Sprintf("全部完成，共 %d 个订阅", len(t.requests))
	t.mu.Unlock()
	t.emitState(bus)
}

func (t *CrawlTask) runSingle(bus *events.Bus, subscriptions *avsubscription.Service, req CrawlRequest) {
	t.setStatus(CrawlStatus{
		Phase:       "index",
		Status:      "running",
		Message:     fmt.Sprintf("正在获取 %s 的索引页", req.ActressName),
		ActressName: req.ActressName,
		BatchTotal:  t.status.BatchTotal,
	})
	t.emitState(bus)
	t.emitLog(bus, "info", fmt.Sprintf("开始抓取: %s (目标 %d 部)", req.ActressName, req.TargetCount))

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	client, err := crawlrequest.NewClient(crawlrequest.PageRequestOptions{
		Proxy:        req.Proxy,
		ConfigCookie: req.ConfigCookie,
		Timeout:      timeout,
	})
	if err != nil {
		t.emitLog(bus, "error", fmt.Sprintf("创建请求客户端失败: %v", err))
		t.mu.Lock()
		t.status.Failed++
		t.mu.Unlock()
		return
	}

	baseURL := req.PreferredBase
	if baseURL == "" {
		if parsed, parseErr := url.Parse(req.CrawlURL); parseErr == nil && parsed.Host != "" {
			baseURL = parsed.Scheme + "://" + parsed.Host
		} else {
			baseURL = "https://www.javbus.com"
		}
	}

	detailLinks, err := t.fetchIndexLinks(bus, client, req, baseURL)
	if err != nil {
		t.emitLog(bus, "error", fmt.Sprintf("获取索引页失败: %v", err))
		t.mu.Lock()
		t.status.Failed++
		t.mu.Unlock()
		return
	}

	if len(detailLinks) == 0 {
		t.emitLog(bus, "warn", "索引页未找到任何影片链接")
		return
	}

	selectedLinks := detailLinks
	selectedCodes := []string{}
	if isSubscriptionIncrementalMode(req) {
		baselineCount := len(normalizeSubscriptionBaselineCodes(req.BaselineCodes))
		selectedLinks, selectedCodes = selectSubscriptionIncrementalLinks(detailLinks, req.BaselineCodes, req.TargetCount)
		t.emitLog(bus, "info", fmt.Sprintf("第一页共 %d 部，基线 %d 部，新增 %d 部", len(detailLinks), baselineCount, len(selectedLinks)))
		if len(selectedCodes) > 0 {
			t.emitLog(bus, "info", fmt.Sprintf("本次仅抓取新增番号：%s", strings.Join(selectedCodes, ", ")))
		}
	}

	targetCount := len(selectedLinks)
	if targetCount == 0 {
		t.mu.Lock()
		t.status.Total = 0
		t.status.Completed = 0
		t.status.Failed = 0
		t.status.Phase = "completed"
		t.status.Status = "completed"
		t.status.Message = fmt.Sprintf("%s 本次无新增番号需要抓取", req.ActressName)
		t.mu.Unlock()
		t.emitState(bus)
		t.emitLog(bus, "info", fmt.Sprintf("%s 本次无新增番号需要抓取", req.ActressName))
		return
	}

	t.mu.Lock()
	t.status.Total = targetCount
	t.status.Phase = "detail"
	t.status.Message = fmt.Sprintf("正在抓取详情 (0/%d)", targetCount)
	t.mu.Unlock()
	t.emitState(bus)

	actressDir := resolveSubcrawlOutputDir(req.OutputDir, req.ActressName, t.status.BatchTotal)
	artifactPaths := crawlartifact.ResolveCrawlOutputPaths(actressDir)
	if strings.TrimSpace(req.UserDataDir) != "" {
		artifactPaths = crawlartifact.ResolveInternalArtifactPaths(req.UserDataDir, actressDir)
	}

	writer, err := crawloutput.NewWriterWithArtifactPaths(actressDir, artifactPaths)
	if err != nil {
		t.emitLog(bus, "error", fmt.Sprintf("创建输出目录失败: %v", err))
		return
	}
	writer.SetArtifactMetadata(crawloutput.ArtifactMetadata{
		ActressName:  req.ActressName,
		CrawlURL:     req.CrawlURL,
		SiteBase:     baseURL,
		TargetCount:  targetCount,
		ItemsPerPage: 30,
		TotalPages:   calcSubcrawlTotalPages(targetCount, 30),
	})

	completed := 0
	failed := 0

	for idx, link := range selectedLinks {
		if t.ctx.Err() != nil {
			break
		}

		t.mu.Lock()
		t.status.Current = link
		t.status.Message = fmt.Sprintf("正在抓取详情 (%d/%d)", idx+1, targetCount)
		t.mu.Unlock()
		t.emitState(bus)

		filmData, magnetErr := t.fetchFilmWithMagnet(bus, client, link, baseURL)
		if magnetErr != nil {
			t.emitLog(bus, "warn", fmt.Sprintf("抓取失败 %s: %v", link, magnetErr))
			failed++
			continue
		}

		if _, writeErr := writer.WriteFilmData(filmData); writeErr != nil {
			t.emitLog(bus, "error", fmt.Sprintf("写入失败: %v", writeErr))
			failed++
			continue
		}

		completed++
		t.mu.Lock()
		t.status.Completed = completed
		t.status.Failed = failed
		t.mu.Unlock()
		t.emitLog(bus, "info", fmt.Sprintf("完成: %s", filmData.Title))

		if idx < len(selectedLinks)-1 {
			time.Sleep(2 * time.Second)
		}
	}

	if flushErr := writer.Flush(); flushErr != nil {
		t.emitLog(bus, "error", fmt.Sprintf("输出文件写入失败: %v", flushErr))
	}

	if completed > 0 && req.SubscriptionID != "" && subscriptions != nil {
		if updated, updateErr := subscriptions.MarkSubscriptionCrawlCompleted(req.SubscriptionID, actressDir); updateErr == nil {
			t.emitLog(bus, "info", fmt.Sprintf("已更新订阅同步状态 (+%d, baseline=%d)", completed, len(updated.BaselineCodes)))
		} else {
			_, _ = subscriptions.MarkSynced(req.SubscriptionID)
			t.emitLog(bus, "warn", fmt.Sprintf("订阅基线回写失败，已仅标记同步: %v", updateErr))
		}
	}

	t.emitLog(bus, "info", fmt.Sprintf("%s 抓取完成: 成功 %d, 失败 %d", req.ActressName, completed, failed))
}

func (t *CrawlTask) fetchIndexLinks(bus *events.Bus, client *crawlrequest.Client, req CrawlRequest, baseURL string) ([]string, error) {
	crawlURL := strings.TrimSpace(req.CrawlURL)
	if crawlURL == "" {
		return nil, fmt.Errorf("抓取地址为空")
	}

	pageURL := crawlindex.BuildIndexPageURL(crawlURL, "", "", 1)
	if strings.TrimSpace(pageURL) == "" {
		pageURL = crawlURL
	}

	resp, err := client.GetPage(t.ctx, pageURL, "")
	if err != nil {
		return nil, err
	}

	links := crawlparse.ParsePageLinks(resp.Body)
	normalized := make([]string, 0, len(links))
	for _, link := range links {
		full := crawlindex.NormalizeDetailLink(link)
		if full == "" {
			continue
		}
		if !strings.HasPrefix(full, "http") {
			full = strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(full, "/")
		}
		normalized = append(normalized, full)
	}

	t.emitLog(bus, "info", fmt.Sprintf("索引页找到 %d 个链接", len(normalized)))

	// 订阅增量模式只看第一页，先做 baseline 差集，再决定后续抓哪些详情页。
	if isSubscriptionIncrementalMode(req) {
		return normalized, nil
	}

	if len(normalized) >= req.TargetCount || req.TargetCount <= 30 {
		return normalized, nil
	}

	pagesNeeded := (req.TargetCount + 29) / 30
	if pagesNeeded > 5 {
		pagesNeeded = 5
	}
	for page := 2; page <= pagesNeeded; page++ {
		if t.ctx.Err() != nil {
			break
		}
		nextURL := crawlindex.BuildIndexPageURL(crawlURL, "", "", page)
		nextResp, nextErr := client.GetPage(t.ctx, nextURL, "")
		if nextErr != nil {
			break
		}
		nextLinks := crawlparse.ParsePageLinks(nextResp.Body)
		for _, link := range nextLinks {
			full := crawlindex.NormalizeDetailLink(link)
			if full == "" {
				continue
			}
			if !strings.HasPrefix(full, "http") {
				full = strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(full, "/")
			}
			normalized = append(normalized, full)
		}
		time.Sleep(1500 * time.Millisecond)
	}

	return normalized, nil
}

func (t *CrawlTask) fetchFilmWithMagnet(bus *events.Bus, client *crawlrequest.Client, detailURL string, baseURL string) (crawloutput.FilmData, error) {
	resp, err := client.GetPage(t.ctx, detailURL, "")
	if err != nil {
		return crawloutput.FilmData{}, fmt.Errorf("详情页请求失败: %w", err)
	}

	metadata, parseErr := crawlparse.ParseMetadata(resp.Body)
	if parseErr != nil {
		return crawloutput.FilmData{}, fmt.Errorf("详情页解析失败: %w", parseErr)
	}

	categories := crawlparse.ParseCategories(resp.Body)
	actresses := crawlparse.ParseActress(resp.Body)

	ajaxURL := buildMagnetAjaxURL(metadata, baseURL)
	magnetResult, magnetErr := FetchMagnetWithFallback(t.ctx, client, ajaxURL, detailURL)

	filmData := crawloutput.FilmData{
		Title:      metadata.Title,
		SourceLink: detailURL,
		Category:   categories,
		Actress:    actresses,
		CoverImage: metadata.Img,
	}

	if magnetErr == nil && magnetResult != nil && magnetResult.Magnet != "" {
		filmData.Magnet = magnetResult.Magnet
		for _, ml := range magnetResult.MagnetLinks {
			filmData.MagnetLinks = append(filmData.MagnetLinks, struct {
				Link string `json:"link"`
				Size string `json:"size"`
			}{Link: ml.Link, Size: ml.Size})
		}
	}
	if magnetErr != nil {
		return crawloutput.FilmData{}, fmt.Errorf("磁力获取失败: %w", magnetErr)
	}
	if strings.TrimSpace(filmData.Magnet) == "" {
		return crawloutput.FilmData{}, fmt.Errorf("磁力获取失败: 浏览器磁力响应为空")
	}

	filmData.ActressCount = len(actresses)
	return filmData, nil
}

func buildMagnetAjaxURL(metadata crawlparse.Metadata, baseURL string) string {
	origin := strings.TrimRight(baseURL, "/")
	if parsed, err := url.Parse(baseURL); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		origin = parsed.Scheme + "://" + parsed.Host
	}
	normalizedImageParam := crawlrequest.NormalizeAjaxImageParam(metadata.Img)
	return fmt.Sprintf(
		"%s/ajax/uncledatoolsbyajax.php?gid=%s&lang=zh&img=%s&uc=%s&floor=%d",
		origin,
		metadata.GID,
		normalizedImageParam,
		metadata.UC,
		time.Now().UnixNano()%1000+1,
	)
}

func (t *CrawlTask) setStatus(s CrawlStatus) {
	t.mu.Lock()
	s.BatchTotal = t.status.BatchTotal
	s.BatchCompleted = t.status.BatchCompleted
	if s.ActressName == "" {
		s.ActressName = t.status.ActressName
	}
	t.status = s
	t.mu.Unlock()
}

func (t *CrawlTask) emitState(bus *events.Bus) {
	t.mu.Lock()
	status := t.status
	t.mu.Unlock()
	raw, _ := json.Marshal(status)
	bus.Emit("subcrawl.state", string(raw))
}

func (t *CrawlTask) emitLog(bus *events.Bus, level string, message string) {
	payload := map[string]string{
		"level":     level,
		"message":   message,
		"timestamp": time.Now().Format(time.RFC3339),
	}
	raw, _ := json.Marshal(payload)
	bus.Emit("subcrawl.log", string(raw))
}

func sanitizeDirName(name string) string {
	name = strings.TrimSpace(name)
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	result := replacer.Replace(name)
	if result == "" {
		return "unknown"
	}
	return result
}

func calcSubcrawlTotalPages(targetCount int, itemsPerPage int) int {
	if targetCount <= 0 {
		return 0
	}
	if itemsPerPage <= 0 {
		itemsPerPage = 30
	}
	return (targetCount + itemsPerPage - 1) / itemsPerPage
}

func isSubscriptionIncrementalMode(req CrawlRequest) bool {
	return strings.TrimSpace(req.SubscriptionID) != "" && len(normalizeSubscriptionBaselineCodes(req.BaselineCodes)) > 0
}

func normalizeSubscriptionBaselineCodes(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}

	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized := crawlidentity.NormalizeFilmID(value)
		if strings.TrimSpace(normalized) == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func selectSubscriptionIncrementalLinks(detailLinks []string, baselineCodes []string, targetCount int) ([]string, []string) {
	if len(detailLinks) == 0 {
		return []string{}, []string{}
	}

	baselineSet := make(map[string]struct{}, len(baselineCodes))
	for _, code := range normalizeSubscriptionBaselineCodes(baselineCodes) {
		baselineSet[code] = struct{}{}
	}

	selectedLinks := make([]string, 0, len(detailLinks))
	selectedCodes := make([]string, 0, len(detailLinks))
	seenCodes := map[string]struct{}{}

	for _, link := range detailLinks {
		code := crawlidentity.ExtractFilmID(link)
		if strings.TrimSpace(code) == "" {
			continue
		}
		if _, exists := baselineSet[code]; exists {
			continue
		}
		if _, exists := seenCodes[code]; exists {
			continue
		}
		seenCodes[code] = struct{}{}
		selectedLinks = append(selectedLinks, link)
		selectedCodes = append(selectedCodes, code)
		if targetCount > 0 && len(selectedLinks) >= targetCount {
			break
		}
	}

	return selectedLinks, selectedCodes
}

func resolveSubcrawlOutputDir(baseOutputDir string, actressName string, batchTotal int) string {
	trimmedOutputDir := strings.TrimSpace(baseOutputDir)
	if trimmedOutputDir == "" {
		return sanitizeDirName(actressName)
	}

	// 单条订阅抓取时，用户选择的目录就是最终目录，不再额外套一层演员名。
	if batchTotal <= 1 {
		return trimmedOutputDir
	}

	leafName := sanitizeDirName(actressName)
	currentLeaf := filepath.Base(strings.TrimRight(trimmedOutputDir, `\/`))
	if strings.EqualFold(currentLeaf, leafName) {
		return trimmedOutputDir
	}
	return filepath.Join(trimmedOutputDir, leafName)
}

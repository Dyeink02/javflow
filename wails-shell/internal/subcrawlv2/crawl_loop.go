// Ownership summary:
//   This file implements domain logic for the javflow backend.
//
// File map for maintainers:
//   1) Domain-specific types and helpers.
//   2) Internal service implementation.
//

package subcrawlv2

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"javflow/internal/avsubscriptionv2"
	"javflow/internal/common"
	"javflow/internal/contracts/crawlartifact"
	"javflow/internal/crawlfetch"
	"javflow/internal/crawlidentity"
	"javflow/internal/crawlindex"
	"javflow/internal/crawloutput"
	"javflow/internal/crawlparse"
	"javflow/internal/crawlrequest"
	"javflow/internal/events"
	"javflow/internal/modulelog"
)

func (t *CrawlTask) Run(bus *events.Bus, subscriptions *avsubscriptionv2.Service, fetch *crawlfetch.Service) {
	t.mu.Lock()
	t.running = true
	t.mu.Unlock()

	defer func() {
		t.mu.Lock()
		t.running = false
		t.mu.Unlock()
	}()

	jobs := make(chan CrawlRequest)
	var workers sync.WaitGroup
	workerCount := t.concurrency
	if workerCount > len(t.requests) {
		workerCount = len(t.requests)
	}
	for index := 0; index < workerCount; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for req := range jobs {
				if t.ctx.Err() != nil {
					return
				}
				t.mu.Lock()
				t.status.Active++
				t.status.ActressName = req.ActressName
				t.mu.Unlock()
				child := &CrawlTask{
					ctx:         t.ctx,
					requests:    []CrawlRequest{req},
					concurrency: 1,
					appPath:     t.appPath,
					status:      CrawlStatus{BatchTotal: len(t.requests), Concurrency: t.concurrency},
				}
				child.runSingle(bus, subscriptions, fetch, req)
				result := child.Status()
				t.mu.Lock()
				t.status.Active--
				t.status.BatchCompleted++
				if result.Total == 0 && result.Failed == 0 {
					t.status.BatchNoUpdate++
				} else if result.Completed > 0 {
					t.status.BatchSucceeded++
				} else {
					t.status.Failed++
				}
				t.mu.Unlock()
				t.emitState(bus)
			}
		}()
	}

	for _, req := range t.requests {
		select {
		case <-t.ctx.Done():
			break
		case jobs <- req:
		}
		if t.ctx.Err() != nil {
			break
		}
	}
	close(jobs)
	workers.Wait()

	t.mu.Lock()
	if t.ctx.Err() != nil {
		t.status.Phase = "stopped"
		t.status.Status = "stopped"
		t.status.Message = "\u7528\u6237\u5df2\u505c\u6b62\u5168\u90e8\u8ba2\u9605\u66f4\u65b0"
	} else {
		t.status.Phase = "completed"
		t.status.Status = "completed"
		t.status.Message = fmt.Sprintf("\u5168\u90e8\u66f4\u65b0\u5b8c\u6210\uff1a\u6210\u529f %d\uff0c\u65e0\u66f4\u65b0 %d\uff0c\u5931\u8d25 %d", t.status.BatchSucceeded, t.status.BatchNoUpdate, t.status.Failed)
	}
	t.mu.Unlock()
	t.emitState(bus)
}

func (t *CrawlTask) runSingle(bus *events.Bus, subscriptions *avsubscriptionv2.Service, fetch *crawlfetch.Service, req CrawlRequest) {
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

	var client *crawlrequest.Client
	var err error
	if fetch != nil && fetch.Client() != nil {
		opts := fetch.Client().CloneOptions()
		opts.Proxy = req.Proxy
		opts.ConfigCookie = req.ConfigCookie
		opts.Timeout = timeout
		client, err = crawlrequest.NewClient(opts)
	} else {
		client, err = crawlrequest.NewClient(crawlrequest.PageRequestOptions{
			Proxy:        req.Proxy,
			ConfigCookie: req.ConfigCookie,
			Timeout:      timeout,
		})
	}
	if err != nil {
		t.emitLog(bus, "error", fmt.Sprintf("创建请求客户端失败: %v", err))
		t.mu.Lock()
		t.status.Failed++
		t.mu.Unlock()
		return
	}
	defer client.Close()

	baseURL := req.PreferredBase
	if baseURL == "" {
		if parsed, parseErr := url.Parse(req.CrawlURL); parseErr == nil && parsed.Host != "" {
			baseURL = parsed.Scheme + "://" + parsed.Host
		} else {
			baseURL = "https://www.javbus.com"
		}
	}

	selectedLinks, selectedCodes, err := t.resolveTargetLinks(bus, client, req, baseURL)
	if err != nil {
		t.emitLog(bus, "error", fmt.Sprintf("获取索引页失败: %v", err))
		t.mu.Lock()
		t.status.Failed++
		t.mu.Unlock()
		return
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

	if len(selectedCodes) > 0 {
		t.emitLog(bus, "info", fmt.Sprintf("本次精准抓取番号：%s", strings.Join(selectedCodes, ", ")))
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
		ActressName:    req.ActressName,
		CrawlURL:       req.CrawlURL,
		SiteBase:       baseURL,
		TargetCount:    targetCount,
		CompletedCount: 0,
		ItemsPerPage:   30,
		TotalPages:     calcSubcrawlTotalPages(targetCount, 30),
	})

	completed := 0
	failed := 0
	magnetLines := make([]string, 0, targetCount)
	magnetSeen := map[string]struct{}{}
	for idx, link := range selectedLinks {
		if t.ctx.Err() != nil {
			break
		}

		t.mu.Lock()
		t.status.Current = link
		t.status.Message = fmt.Sprintf("正在抓取详情 (%d/%d)", idx+1, targetCount)
		t.mu.Unlock()
		t.emitState(bus)

		filmData, magnetErr := t.fetchFilmWithMagnet(client, link, baseURL)
		if magnetErr != nil {
			t.emitLog(bus, "warn", fmt.Sprintf("抓取失败 %s: %v", link, magnetErr))
			failed++
			continue
		}

		filmData = applyActressCountFilter(filmData, req.ActressCountFilterThreshold)

		if _, writeErr := writer.WriteFilmData(filmData); writeErr != nil {
			t.emitLog(bus, "error", fmt.Sprintf("写入失败: %v", writeErr))
			failed++
			continue
		}

		completed++
		if filmData.FilteredByActressCount || filmData.FilteredByFilmCode {
			t.emitLog(bus, "info", fmt.Sprintf("filtered collection film: %s", filmData.Title))
		} else {
			for _, magnetLine := range strings.Split(filmData.Magnet, "\n") {
				magnetLine = strings.TrimSpace(magnetLine)
				key := strings.ToLower(magnetLine)
				if magnetLine == "" {
					continue
				}
				if _, exists := magnetSeen[key]; exists {
					continue
				}
				magnetSeen[key] = struct{}{}
				magnetLines = append(magnetLines, magnetLine)
			}
		}
		t.mu.Lock()
		t.status.Completed = completed
		t.status.Failed = failed
		t.mu.Unlock()
		t.emitLog(bus, "info", fmt.Sprintf("完成: %s", filmData.Title))

		if idx < len(selectedLinks)-1 {
			time.Sleep(2 * time.Second)
		}
	}

	writer.SetArtifactMetadata(crawloutput.ArtifactMetadata{
		CompletedAt:    time.Now().Format(time.RFC3339),
		ActressName:    req.ActressName,
		CrawlURL:       req.CrawlURL,
		SiteBase:       baseURL,
		TargetCount:    targetCount,
		CompletedCount: completed,
		ItemsPerPage:   30,
		TotalPages:     calcSubcrawlTotalPages(targetCount, 30),
	})
	if flushErr := writer.Flush(); flushErr != nil {
		t.emitLog(bus, "error", fmt.Sprintf("输出文件写入失败: %v", flushErr))
	}
	if completed > 0 {
		visiblePath, outputErr := writeSubscriptionResult(actressDir, magnetLines, completed, time.Now())
		if outputErr != nil {
			t.emitLog(bus, "error", fmt.Sprintf("\u8ba2\u9605\u7ed3\u679c\u6587\u4ef6\u5199\u5165\u5931\u8d25: %v", outputErr))
			t.mu.Lock()
			t.status.Failed++
			t.mu.Unlock()
			return
		}
		t.emitLog(bus, "info", fmt.Sprintf("\u5df2\u4fdd\u7559\u8ba2\u9605\u7ed3\u679c: %s", visiblePath))
	}

	if completed > 0 && req.SubscriptionID != "" && subscriptions != nil {
		if updated, updateErr := subscriptions.MarkCrawlCompleted(req.SubscriptionID, actressDir); updateErr == nil {
			t.emitLog(bus, "info", fmt.Sprintf("已更新订阅同步状态 (+%d, baseline=%d)", completed, updated.BaselineCount))
			if bus != nil {
				bus.Emit("avsubscriptionv2.list-updated", map[string]any{
					"trigger":        "batch-crawl-completed",
					"subscriptionId": req.SubscriptionID,
				})
			}
		} else {
			t.emitLog(bus, "warn", fmt.Sprintf("订阅基线回写失败: %v", updateErr))
		}
	}

	t.emitLog(bus, "info", fmt.Sprintf("%s 抓取完成: 成功 %d, 失败 %d", req.ActressName, completed, failed))
}

func (t *CrawlTask) resolveTargetLinks(bus *events.Bus, client *crawlrequest.Client, req CrawlRequest, baseURL string) ([]string, []string, error) {
	targetCodes := normalizeTargetCodes(req.TargetCodes)
	if len(targetCodes) == 0 {
		return t.fetchTopLinks(bus, client, req, baseURL)
	}

	targetSet := map[string]struct{}{}
	for _, code := range targetCodes {
		targetSet[code] = struct{}{}
	}

	foundLinks := make([]string, 0, len(targetCodes))
	foundCodes := make([]string, 0, len(targetCodes))
	seenCodes := map[string]struct{}{}

	maxPages := calcSubcrawlScanPages(len(targetCodes), 30)
	for page := 1; page <= maxPages; page++ {
		if t.ctx.Err() != nil {
			break
		}
		pageURL := crawlindex.BuildIndexPageURL(req.CrawlURL, "", "", page)
		if strings.TrimSpace(pageURL) == "" {
			pageURL = req.CrawlURL
		}

		resp, err := client.GetPage(t.ctx, pageURL, "")
		if err != nil {
			return nil, nil, err
		}

		rawLinks := crawlparse.ParsePageLinks(resp.Body)
		pageMatched := 0
		for _, link := range rawLinks {
			full := crawlindex.NormalizeDetailLink(link)
			if full == "" {
				continue
			}
			if !strings.HasPrefix(full, "http") {
				full = strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(full, "/")
			}
			code := crawlidentity.ExtractFilmID(full)
			if _, exists := targetSet[code]; !exists {
				continue
			}
			if _, exists := seenCodes[code]; exists {
				continue
			}
			seenCodes[code] = struct{}{}
			foundCodes = append(foundCodes, code)
			foundLinks = append(foundLinks, full)
			pageMatched++
		}

		t.emitLog(bus, "info", fmt.Sprintf("第 %d 页匹配到 %d 个待抓番号", page, pageMatched))
		if len(foundCodes) >= len(targetCodes) {
			break
		}
	}

	return foundLinks, foundCodes, nil
}

func (t *CrawlTask) fetchTopLinks(bus *events.Bus, client *crawlrequest.Client, req CrawlRequest, baseURL string) ([]string, []string, error) {
	crawlURL := strings.TrimSpace(req.CrawlURL)
	if crawlURL == "" {
		return nil, nil, fmt.Errorf("抓取地址为空")
	}

	pageURL := crawlindex.BuildIndexPageURL(crawlURL, "", "", 1)
	if strings.TrimSpace(pageURL) == "" {
		pageURL = crawlURL
	}
	resp, err := client.GetPage(t.ctx, pageURL, "")
	if err != nil {
		return nil, nil, err
	}

	links := crawlparse.ParsePageLinks(resp.Body)
	normalized := make([]string, 0, len(links))
	codes := make([]string, 0, len(links))
	for _, link := range links {
		full := crawlindex.NormalizeDetailLink(link)
		if full == "" {
			continue
		}
		if !strings.HasPrefix(full, "http") {
			full = strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(full, "/")
		}
		normalized = append(normalized, full)
		code := crawlidentity.ExtractFilmID(full)
		if strings.TrimSpace(code) != "" {
			codes = append(codes, code)
		}
		if req.TargetCount > 0 && len(normalized) >= req.TargetCount {
			break
		}
	}

	t.emitLog(bus, "info", fmt.Sprintf("索引页找到 %d 个链接", len(normalized)))
	return normalized, normalizeTargetCodes(codes), nil
}

func (t *CrawlTask) fetchFilmWithMagnet(client *crawlrequest.Client, detailURL string, baseURL string) (crawloutput.FilmData, error) {
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
	candidates, magnetErr := crawlrequest.FetchMagnetCandidatesWithFallback(t.ctx, client, metadata, baseURL, detailURL)
	var magnetResult *crawlrequest.MagnetResult
	if magnetErr == nil {
		magnetResult = crawlrequest.BuildMagnetResult(candidates, false, 3)
	}

	filmData := crawloutput.FilmData{
		Title:       metadata.Title,
		SourceLink:  detailURL,
		Category:    categories,
		Actress:     actresses,
		CoverImage:  metadata.Img,
		ReleaseDate: metadata.ReleaseDate,
		Maker:       metadata.Maker,
		Label:       metadata.Label,
		Series:      metadata.Series,
	}

	if magnetErr == nil && magnetResult != nil && magnetResult.Magnet != "" {
		filmData.Magnet = magnetResult.Magnet
		for _, ml := range magnetResult.MagnetLinks {
			filmData.MagnetLinks = append(filmData.MagnetLinks, struct {
				Link        string `json:"link"`
				Size        string `json:"size"`
				DisplayName string `json:"displayName,omitempty"`
			}{Link: ml.Link, Size: ml.Size, DisplayName: ml.DisplayName})
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

func applyActressCountFilter(filmData crawloutput.FilmData, threshold int) crawloutput.FilmData {
	if threshold <= 0 || filmData.ActressCount < threshold {
		return filmData
	}
	filmData.FilteredByActressCount = true
	remark := fmt.Sprintf("actress count %d >= threshold %d, skip magnet output only", filmData.ActressCount, threshold)
	if filmData.FilterRemark != "" {
		filmData.FilterRemark += "; "
	}
	filmData.FilterRemark += remark
	return filmData
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
	bus.Emit("subcrawlv2.state", string(raw))
}

func (t *CrawlTask) emitLog(bus *events.Bus, level string, message string) {
	_ = modulelog.Append(t.appPath, modulelog.Subscription, level, message, time.Now())
	payload := map[string]string{
		"level":     level,
		"message":   message,
		"timestamp": time.Now().Format(time.RFC3339),
	}
	raw, _ := json.Marshal(payload)
	bus.Emit("subcrawlv2.log", string(raw))
}

func writeSubscriptionResult(outputDir string, magnetLines []string, completed int, now time.Time) (string, error) {
	if completed <= 0 || len(magnetLines) == 0 {
		return "", nil
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", err
	}
	fileName := fmt.Sprintf("%d\u5e74%d\u6708%d\u53f7\u66f4\u65b0%d\u90e8.txt", now.Year(), int(now.Month()), now.Day(), completed)
	filePath := filepath.Join(outputDir, fileName)
	if err := common.WriteUTF8TextFile(filePath, strings.Join(magnetLines, "\r\n")+"\r\n"); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if strings.EqualFold(entry.Name(), fileName) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(outputDir, entry.Name())); err != nil {
			return "", err
		}
	}
	return filePath, nil
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

func calcSubcrawlScanPages(targetCodeCount int, itemsPerPage int) int {
	if itemsPerPage <= 0 {
		itemsPerPage = 30
	}
	pages := calcSubcrawlTotalPages(targetCodeCount, itemsPerPage) + 2
	if pages < 1 {
		pages = 1
	}
	if pages > 50 {
		pages = 50
	}
	return pages
}

func normalizeTargetCodes(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := map[string]struct{}{}
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

func resolveSubcrawlOutputDir(baseOutputDir string, actressName string, batchTotal int) string {
	trimmedOutputDir := strings.TrimSpace(baseOutputDir)
	if trimmedOutputDir == "" {
		return sanitizeDirName(actressName)
	}
	if batchTotal <= 1 {
		return trimmedOutputDir
	}
	return filepath.Join(trimmedOutputDir, sanitizeDirName(actressName))
}

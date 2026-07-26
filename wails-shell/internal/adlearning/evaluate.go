package adlearning

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// evaluate.go owns local ad-risk scoring heuristics and ffmpeg/onnx-assisted
// evaluation entrypoints.
//
// Ownership summary:
// 1) run ad-risk evaluation heuristics against video/image inputs
// 2) coordinate ffmpeg/onnx-assisted scoring entrypoints
// 3) keep scoring policy separate from sample import and UI-facing orchestration
//
// File map for maintainers:
// 1) model preset / score-tier definitions
// 2) hash-cache and ffmpeg command helpers
// 3) frame/image evaluation entrypoints
// 4) score aggregation and evidence/reason shaping

const (
	hashCacheVersion  = 1
	hashCacheMaxItems = 12000
)

var (
	defaultVideoFrameSeconds = []int{3, 8, 15}
	urlPattern               = regexp.MustCompile(`[a-z0-9-]+\.(com|net|org|cn|cc|tv|xyz|me|vip|top)`)
)

type scoreTier struct {
	High   int
	Medium int
	Low    int
}

type adModelPreset struct {
	ID                  string
	Label               string
	FrameSeconds        []int
	KeywordScorePerHit  int
	KeywordScoreMax     int
	DomainPatternScore  int
	TemplateScore       scoreTier
	AdSampleScore       scoreTier
	NormalSamplePenalty scoreTier
}

type EvaluateOptions struct {
	VideoPath   string
	StreamURL   string
	FilmCode    string
	AdThreshold int
	ModelType   string
}

type EvaluateResult struct {
	VideoPath          string         `json:"videoPath"`
	ModelType          string         `json:"modelType"`
	ModelLabel         string         `json:"modelLabel"`
	FFmpegAvailable    bool           `json:"ffmpegAvailable"`
	HashesFromCache    bool           `json:"hashesFromCache"`
	Score              float64        `json:"score"`
	Threshold          float64        `json:"threshold"`
	IsAd               bool           `json:"isAd"`
	Reasons            []string       `json:"reasons"`
	BestAdDistance     *int           `json:"bestAdDistance,omitempty"`
	BestNormalDistance *int           `json:"bestNormalDistance,omitempty"`
	SampleCounts       map[string]int `json:"sampleCounts"`
	Evidence           map[string]any `json:"evidence"`
}

type sampleMatch struct {
	Distance       int
	VideoHashIndex int
	SampleID       string
	SourcePath     string
	FilmCode       string
	FrameSecond    *int
	HashBits       string
	Label          string
}

type videoHashCacheRecord struct {
	UpdatedAtMS int64    `json:"updatedAtMs"`
	Hashes      []string `json:"hashes"`
}

type videoHashCacheFile struct {
	Version   int                             `json:"version"`
	UpdatedAt string                          `json:"updatedAt"`
	Items     map[string]videoHashCacheRecord `json:"items"`
}

func getAdModelPreset(rawValue string) adModelPreset {
	// These names are compatibility presets exposed to the existing UI. The
	// current detector is a lightweight strategy engine based on frame hashes,
	// learned samples, filename/domain signals, and keywords; it does not load
	// ONNX YOLO/MobileNet/SqueezeNet networks yet.
	switch normalizeAdModelType(rawValue) {
	case "squeezenet-fast":
		return adModelPreset{
			ID:                 "squeezenet-fast",
			Label:              "SqueezeNet Fast",
			FrameSeconds:       []int{2, 6, 11},
			KeywordScorePerHit: 13,
			KeywordScoreMax:    36,
			DomainPatternScore: 24,
			TemplateScore: scoreTier{
				High:   48,
				Medium: 30,
				Low:    12,
			},
			AdSampleScore: scoreTier{
				High:   38,
				Medium: 24,
				Low:    10,
			},
			NormalSamplePenalty: scoreTier{
				High:   38,
				Medium: 20,
			},
		}
	case "yolov8n-balanced":
		return adModelPreset{
			ID:                 "yolov8n-balanced",
			Label:              "YOLOv8n Balanced",
			FrameSeconds:       []int{2, 5, 8, 12, 15},
			KeywordScorePerHit: 17,
			KeywordScoreMax:    45,
			DomainPatternScore: 32,
			TemplateScore: scoreTier{
				High:   62,
				Medium: 40,
				Low:    20,
			},
			AdSampleScore: scoreTier{
				High:   52,
				Medium: 35,
				Low:    15,
			},
			NormalSamplePenalty: scoreTier{
				High:   48,
				Medium: 28,
			},
		}
	default:
		return adModelPreset{
			ID:                 defaultAdModelType,
			Label:              "MobileNetV3 Lite",
			FrameSeconds:       []int{3, 8, 15},
			KeywordScorePerHit: 15,
			KeywordScoreMax:    40,
			DomainPatternScore: 30,
			TemplateScore: scoreTier{
				High:   55,
				Medium: 35,
				Low:    15,
			},
			AdSampleScore: scoreTier{
				High:   45,
				Medium: 30,
				Low:    12,
			},
			NormalSamplePenalty: scoreTier{
				High:   45,
				Medium: 25,
			},
		}
	}
}

func normalizeFrameSeconds(values []int) []int {
	if len(values) == 0 {
		return append([]int(nil), defaultVideoFrameSeconds...)
	}
	seen := map[int]struct{}{}
	result := make([]int, 0, len(values))
	for _, value := range values {
		second := clamp(value, 0, 30)
		if _, exists := seen[second]; exists {
			continue
		}
		seen[second] = struct{}{}
		result = append(result, second)
	}
	if len(result) == 0 {
		return append([]int(nil), defaultVideoFrameSeconds...)
	}
	return result
}

func getModelFrameSeconds(rawValue string) []int {
	return normalizeFrameSeconds(getAdModelPreset(rawValue).FrameSeconds)
}

func (s *Service) EvaluateVideoRisk(options EvaluateOptions) (EvaluateResult, error) {
	videoPath := strings.TrimSpace(options.VideoPath)
	if videoPath == "" {
		return EvaluateResult{}, fmt.Errorf("videoPath 不能为空")
	}

	streamURL := strings.TrimSpace(options.StreamURL)
	if streamURL != "" {
		if err := validateStreamURL(streamURL); err != nil {
			return EvaluateResult{}, fmt.Errorf("streamUrl 无效：%w", err)
		}
	}

	model := s.loadModel()
	modelType := normalizeAdModelType(firstNonEmpty(options.ModelType, model.Meta.ActiveModel))
	threshold := normalizeStoredThreshold(options.AdThreshold, model.Thresholds.AdScore)
	ffmpegAvailable, hashes, fromCache := s.buildVideoHashes(videoPath, streamURL, getModelFrameSeconds(modelType))
	return s.evaluateRiskWithHashes(videoPath, model, modelType, threshold, ffmpegAvailable, hashes, fromCache), nil
}

func validateStreamURL(streamURL string) error {
	if strings.ContainsAny(streamURL, "\r\n\t") {
		return fmt.Errorf("包含非法空白字符")
	}
	if strings.HasPrefix(streamURL, "-") {
		return fmt.Errorf("不能以 '-' 开头")
	}

	parsed, err := url.Parse(streamURL)
	if err != nil {
		return fmt.Errorf("URL 格式无效：%w", err)
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return fmt.Errorf("仅允许 http/https 协议")
	}

	host := strings.TrimSpace(strings.ToLower(parsed.Hostname()))
	if host == "" {
		return fmt.Errorf("缺少主机名")
	}
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return fmt.Errorf("不能指向本机")
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()) {
		return fmt.Errorf("不能指向私有网络")
	}
	return nil
}

func (s *Service) evaluateRiskWithHashes(videoPath string, model LearningModel, modelType string, threshold int, ffmpegAvailable bool, hashes []string, fromCache bool) EvaluateResult {
	modelType = normalizeAdModelType(modelType)
	preset := getAdModelPreset(modelType)
	frameSeconds := getModelFrameSeconds(modelType)
	filename := strings.ToLower(filepath.Base(videoPath))
	reasons := make([]string, 0, 8)
	score := 0

	keywordHits := make([]string, 0)
	for _, keyword := range model.Keywords {
		if keyword != "" && strings.Contains(filename, keyword) {
			keywordHits = append(keywordHits, keyword)
		}
	}

	domainPatternHit := urlPattern.MatchString(filename)
	if len(keywordHits) > 0 {
		score += minInt(preset.KeywordScoreMax, len(keywordHits)*preset.KeywordScorePerHit)
		reasons = append(reasons, "命中广告关键词："+strings.Join(keywordHits, ", "))
	}
	if domainPatternHit {
		score += preset.DomainPatternScore
		reasons = append(reasons, "文件名疑似包含广告站点域名特征")
	}

	adMatch := findBestSampleMatch(hashes, model.AdSamples)
	normalMatch := findBestSampleMatch(hashes, model.NormalSamples)
	templateMatch := findBestSampleMatch(hashes, addTemplateLabels(model.IntroTemplates))

	coarseByTemplate := templateMatch != nil && templateMatch.Distance <= model.Thresholds.LowSimilarityDistance
	coarseByAdSample := adMatch != nil && adMatch.Distance <= model.Thresholds.MediumSimilarityDistance
	coarseByKeyword := len(keywordHits) > 0 || domainPatternHit
	coarsePassed := coarseByTemplate || coarseByAdSample || coarseByKeyword

	evidence := map[string]any{
		"frameHashes":           buildFrameHashEvidence(hashes, frameSeconds),
		"keywordHits":           keywordHits,
		"model":                 map[string]any{"modelType": modelType, "modelLabel": preset.Label, "frameSeconds": frameSeconds},
		"bestTemplateMatch":     buildTemplateEvidence(templateMatch),
		"bestAdSampleMatch":     buildSampleEvidence(adMatch, true),
		"bestNormalSampleMatch": buildSampleEvidence(normalMatch, true),
		"cacheInfo":             map[string]any{"fromCache": fromCache},
	}

	if !coarsePassed {
		reasons = append([]string{"模型策略：" + preset.Label}, reasons...)
		reasons = append(reasons, "FFmpeg 粗筛未命中广告特征，跳过后续精筛。")
		evidence["coarseStage"] = map[string]any{
			"passed":     false,
			"byTemplate": nil,
			"byAdSample": nil,
			"byKeyword":  coarseByKeyword,
		}
		return EvaluateResult{
			VideoPath:          videoPath,
			ModelType:          modelType,
			ModelLabel:         preset.Label,
			FFmpegAvailable:    ffmpegAvailable,
			HashesFromCache:    fromCache,
			Score:              0,
			Threshold:          float64(threshold),
			IsAd:               false,
			Reasons:            reasons,
			BestAdDistance:     toOptionalInt(adMatch),
			BestNormalDistance: toOptionalInt(normalMatch),
			SampleCounts: map[string]int{
				"ad":             len(model.AdSamples),
				"normal":         len(model.NormalSamples),
				"introTemplates": len(model.IntroTemplates),
			},
			Evidence: evidence,
		}
	}

	if coarseByTemplate && templateMatch != nil {
		if templateMatch.Distance <= model.Thresholds.HighSimilarityDistance {
			score += 20
			reasons = append(reasons, fmt.Sprintf("FFmpeg 粗筛命中片头模板（高相似，距离 %d）", templateMatch.Distance))
		} else {
			score += 12
			reasons = append(reasons, fmt.Sprintf("FFmpeg 粗筛命中片头模板（中低相似，距离 %d）", templateMatch.Distance))
		}
	}

	if coarseByAdSample && adMatch != nil {
		if adMatch.Distance <= model.Thresholds.HighSimilarityDistance {
			score += 20
			reasons = append(reasons, fmt.Sprintf("FFmpeg 粗筛命中广告样本（高相似，距离 %d）", adMatch.Distance))
		} else {
			score += 10
			reasons = append(reasons, fmt.Sprintf("FFmpeg 粗筛命中广告样本（中相似，距离 %d）", adMatch.Distance))
		}
	}

	if templateMatch != nil {
		switch {
		case templateMatch.Distance <= model.Thresholds.HighSimilarityDistance:
			score += preset.TemplateScore.High
			reasons = append(reasons, fmt.Sprintf("命中片头模板（高相似，距离 %d）", templateMatch.Distance))
		case templateMatch.Distance <= model.Thresholds.MediumSimilarityDistance:
			score += preset.TemplateScore.Medium
			reasons = append(reasons, fmt.Sprintf("命中片头模板（中相似，距离 %d）", templateMatch.Distance))
		case templateMatch.Distance <= model.Thresholds.LowSimilarityDistance:
			score += preset.TemplateScore.Low
			reasons = append(reasons, fmt.Sprintf("命中片头模板（低相似，距离 %d）", templateMatch.Distance))
		}
	}

	if adMatch != nil {
		switch {
		case adMatch.Distance <= model.Thresholds.HighSimilarityDistance:
			score += preset.AdSampleScore.High
			reasons = append(reasons, fmt.Sprintf("与广告样本高相似（距离 %d）", adMatch.Distance))
		case adMatch.Distance <= model.Thresholds.MediumSimilarityDistance:
			score += preset.AdSampleScore.Medium
			reasons = append(reasons, fmt.Sprintf("与广告样本中相似（距离 %d）", adMatch.Distance))
		case adMatch.Distance <= model.Thresholds.LowSimilarityDistance:
			score += preset.AdSampleScore.Low
			reasons = append(reasons, fmt.Sprintf("与广告样本低相似（距离 %d）", adMatch.Distance))
		}
	}

	if normalMatch != nil {
		switch {
		case normalMatch.Distance <= model.Thresholds.HighSimilarityDistance:
			score -= preset.NormalSamplePenalty.High
			reasons = append(reasons, fmt.Sprintf("与正常样本高相似（距离 %d）", normalMatch.Distance))
		case normalMatch.Distance <= model.Thresholds.MediumSimilarityDistance:
			score -= preset.NormalSamplePenalty.Medium
			reasons = append(reasons, fmt.Sprintf("与正常样本中相似（距离 %d）", normalMatch.Distance))
		}
	}

	score = clamp(score, 0, 100)
	isAd := float64(score) >= float64(threshold)
	evidence["coarseStage"] = map[string]any{
		"passed":     true,
		"byTemplate": buildCoarseTemplateEvidence(templateMatch, coarseByTemplate),
		"byAdSample": buildCoarseSampleEvidence(adMatch, coarseByAdSample),
		"byKeyword":  coarseByKeyword,
	}
	reasons = append([]string{"模型策略：" + preset.Label}, reasons...)

	return EvaluateResult{
		VideoPath:          videoPath,
		ModelType:          modelType,
		ModelLabel:         preset.Label,
		FFmpegAvailable:    ffmpegAvailable,
		HashesFromCache:    fromCache,
		Score:              float64(score),
		Threshold:          float64(threshold),
		IsAd:               isAd,
		Reasons:            reasons,
		BestAdDistance:     toOptionalInt(adMatch),
		BestNormalDistance: toOptionalInt(normalMatch),
		SampleCounts: map[string]int{
			"ad":             len(model.AdSamples),
			"normal":         len(model.NormalSamples),
			"introTemplates": len(model.IntroTemplates),
		},
		Evidence: evidence,
	}
}

func addTemplateLabels(samples []SampleRecord) []SampleRecord {
	if len(samples) == 0 {
		return nil
	}
	result := make([]SampleRecord, 0, len(samples))
	for _, sample := range samples {
		result = append(result, sample)
	}
	return result
}

func buildFrameHashEvidence(hashes []string, frameSeconds []int) []map[string]any {
	result := make([]map[string]any, 0, len(hashes))
	for index, hashBits := range hashes {
		var frameSecond any
		if index < len(frameSeconds) {
			frameSecond = frameSeconds[index]
		} else {
			frameSecond = nil
		}
		result = append(result, map[string]any{
			"index":       index,
			"frameSecond": frameSecond,
			"hashHex":     hashBitsToHex(hashBits),
		})
	}
	return result
}

func buildTemplateEvidence(match *sampleMatch) any {
	if match == nil {
		return nil
	}
	return map[string]any{
		"templateId":  match.SampleID,
		"distance":    match.Distance,
		"sourcePath":  match.SourcePath,
		"frameSecond": match.FrameSecond,
	}
}

func buildSampleEvidence(match *sampleMatch, includeFilmCode bool) any {
	if match == nil {
		return nil
	}
	result := map[string]any{
		"sampleId":    match.SampleID,
		"distance":    match.Distance,
		"sourcePath":  match.SourcePath,
		"frameSecond": match.FrameSecond,
	}
	if includeFilmCode {
		result["filmCode"] = match.FilmCode
	}
	return result
}

func buildCoarseTemplateEvidence(match *sampleMatch, passed bool) any {
	if !passed || match == nil {
		return nil
	}
	return map[string]any{
		"distance":    match.Distance,
		"sourcePath":  match.SourcePath,
		"frameSecond": match.FrameSecond,
	}
}

func buildCoarseSampleEvidence(match *sampleMatch, passed bool) any {
	if !passed || match == nil {
		return nil
	}
	return map[string]any{
		"distance":    match.Distance,
		"sourcePath":  match.SourcePath,
		"filmCode":    match.FilmCode,
		"frameSecond": match.FrameSecond,
	}
}

func toOptionalInt(match *sampleMatch) *int {
	if match == nil {
		return nil
	}
	value := match.Distance
	return &value
}

func findBestSampleMatch(videoHashes []string, samples []SampleRecord) *sampleMatch {
	if len(videoHashes) == 0 || len(samples) == 0 {
		return nil
	}
	var best *sampleMatch
	for _, sample := range samples {
		sampleHash := strings.TrimSpace(sample.HashBits)
		if sampleHash == "" {
			continue
		}
		for index, videoHash := range videoHashes {
			distance := hammingDistance(videoHash, sampleHash)
			if distance < 0 {
				continue
			}
			if best == nil || distance < best.Distance {
				best = &sampleMatch{
					Distance:       distance,
					VideoHashIndex: index,
					SampleID:       strings.TrimSpace(sample.ID),
					SourcePath:     strings.TrimSpace(sample.SourcePath),
					FilmCode:       strings.TrimSpace(sample.FilmCode),
					FrameSecond:    sample.FrameSecond,
					HashBits:       sampleHash,
				}
			}
		}
	}
	return best
}

func hammingDistance(leftBits string, rightBits string) int {
	left := strings.TrimSpace(leftBits)
	right := strings.TrimSpace(rightBits)
	maxLength := maxInt(len(left), len(right))
	if maxLength == 0 {
		return -1
	}
	diff := 0
	for index := 0; index < maxLength; index++ {
		leftValue := byte('0')
		rightValue := byte('0')
		if index < len(left) {
			leftValue = left[index]
		}
		if index < len(right) {
			rightValue = right[index]
		}
		if leftValue != rightValue {
			diff++
		}
	}
	return diff
}

func toPerceptualBits(rawBuffer []byte) string {
	if len(rawBuffer) < 64 {
		return ""
	}
	bytes64 := rawBuffer[:64]
	total := 0
	for _, value := range bytes64 {
		total += int(value)
	}
	average := float64(total) / 64

	var builder strings.Builder
	builder.Grow(64)
	for _, value := range bytes64 {
		if float64(value) >= average {
			builder.WriteByte('1')
		} else {
			builder.WriteByte('0')
		}
	}
	return builder.String()
}

func (s *Service) hashCachePath() string {
	return filepath.Join(s.paths.UserData, "ad-learning-hash-cache.json")
}

func (s *Service) buildVideoHashes(videoPath string, streamURL string, frameSeconds []int) (bool, []string, bool) {
	command, ok := s.resolveFFmpegCommand()
	if !ok {
		return false, nil, false
	}

	useStream := streamURL != ""

	if !useStream {
		info, err := os.Stat(videoPath)
		if err != nil || info.IsDir() {
			return true, nil, false
		}
	}

	normalizedSeconds := normalizeFrameSeconds(frameSeconds)

	if !useStream {
		info, _ := os.Stat(videoPath)
		cacheKey := buildVideoHashCacheKey(videoPath, info, normalizedSeconds)
		if cached := s.getCachedVideoHashes(cacheKey); len(cached) > 0 {
			return true, cached, true
		}
	}

	hashes := make([]string, 0, len(normalizedSeconds))
	if useStream {
		for _, second := range normalizedSeconds {
			bits, err := computeStreamFrameHash(command, streamURL, second)
			if err != nil || bits == "" {
				continue
			}
			hashes = append(hashes, bits)
		}
	} else {
		for _, second := range normalizedSeconds {
			bits, err := computeVideoFrameHash(command, videoPath, second)
			if err != nil || bits == "" {
				continue
			}
			hashes = append(hashes, bits)
		}
	}

	if !useStream && len(hashes) > 0 {
		info, _ := os.Stat(videoPath)
		cacheKey := buildVideoHashCacheKey(videoPath, info, normalizedSeconds)
		s.setCachedVideoHashes(cacheKey, hashes)
	}
	return true, hashes, false
}

func (s *Service) resolveFFmpegCommand() (string, bool) {
	s.ffmpegMu.Lock()
	defer s.ffmpegMu.Unlock()

	if s.ffmpegChecked {
		return s.ffmpegCommand, s.ffmpegAvailable
	}

	for _, candidate := range s.ffmpegCandidates() {
		if !fileExists(candidate) {
			continue
		}
		if probeFFmpegCommand(candidate) {
			s.ffmpegChecked = true
			s.ffmpegAvailable = true
			s.ffmpegCommand = candidate
			return s.ffmpegCommand, true
		}
	}

	if probeFFmpegCommand("ffmpeg") {
		s.ffmpegChecked = true
		s.ffmpegAvailable = true
		s.ffmpegCommand = "ffmpeg"
		return s.ffmpegCommand, true
	}

	s.ffmpegChecked = true
	s.ffmpegAvailable = false
	s.ffmpegCommand = ""
	return "", false
}

func (s *Service) ffmpegCandidates() []string {
	candidates := []string{
		filepath.Join(s.paths.UserData, "tools", "ffmpeg", "ffmpeg.exe"),
		filepath.Join(s.paths.ResourcesPath, "tools", "ffmpeg", "ffmpeg.exe"),
		filepath.Join(s.paths.AppPath, "tools", "ffmpeg", "ffmpeg.exe"),
		filepath.Join(s.paths.AppPath, "resources", "ffmpeg", "win-x64", "ffmpeg.exe"),
		filepath.Join(s.paths.ResourcesPath, "resources", "ffmpeg", "win-x64", "ffmpeg.exe"),
		filepath.Join(s.paths.AppPath, "desktop", "resources", "ffmpeg", "win-x64", "ffmpeg.exe"),
	}

	unique := map[string]struct{}{}
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		resolved := strings.TrimSpace(candidate)
		if resolved == "" {
			continue
		}
		absolute, err := filepath.Abs(resolved)
		if err != nil {
			continue
		}
		key := strings.ToLower(absolute)
		if _, exists := unique[key]; exists {
			continue
		}
		unique[key] = struct{}{}
		result = append(result, absolute)
	}
	return result
}

func probeFFmpegCommand(command string) bool {
	if !strings.EqualFold(command, "ffmpeg") && !fileExists(command) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, command, "-version").CombinedOutput()
	if err != nil {
		return false
	}
	return len(output) > 0
}

func computeVideoFrameHash(command string, filePath string, second int) (string, error) {
	raw, err := execFFmpegBuffer(command, []string{
		"-v", "error",
		"-ss", fmt.Sprintf("%d", second),
		"-i", filePath,
		"-frames:v", "1",
		"-vf", "scale=8:8,format=gray",
		"-f", "rawvideo",
		"-",
	}, 12*time.Second)
	if err != nil {
		return "", err
	}
	return toPerceptualBits(raw), nil
}

func execFFmpegBuffer(command string, args []string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		if stderrText == "" {
			stderrText = err.Error()
		}
		return nil, fmt.Errorf("ffmpeg 执行失败: %s", stderrText)
	}
	return stdout.Bytes(), nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func buildVideoHashCacheKey(videoPath string, info os.FileInfo, frameSeconds []int) string {
	absolute, err := filepath.Abs(videoPath)
	if err != nil {
		absolute = videoPath
	}
	frameValues := normalizeFrameSeconds(frameSeconds)
	parts := make([]string, 0, len(frameValues))
	for _, second := range frameValues {
		parts = append(parts, fmt.Sprintf("%d", second))
	}
	return strings.ToLower(filepath.Clean(absolute)) +
		"|" + fmt.Sprintf("%d", info.Size()) +
		"|" + fmt.Sprintf("%d", info.ModTime().UnixMilli()) +
		"|" + strings.Join(parts, ",")
}

func (s *Service) loadHashCache() videoHashCacheFile {
	s.hashCacheMu.Lock()
	defer s.hashCacheMu.Unlock()

	if s.hashCacheLoaded {
		return s.hashCache
	}

	cache := videoHashCacheFile{
		Version: hashCacheVersion,
		Items:   map[string]videoHashCacheRecord{},
	}
	contents, err := os.ReadFile(s.hashCachePath())
	if err == nil {
		parsed := videoHashCacheFile{}
		if json.Unmarshal(contents, &parsed) == nil {
			cache.UpdatedAt = strings.TrimSpace(parsed.UpdatedAt)
			if parsed.Items != nil {
				cache.Items = parsed.Items
			}
		}
	}

	s.hashCacheLoaded = true
	s.hashCache = cache
	return s.hashCache
}

func (s *Service) saveHashCache(cache videoHashCacheFile) {
	cache.Version = hashCacheVersion
	cache.UpdatedAt = s.now().Format(time.RFC3339)
	if cache.Items == nil {
		cache.Items = map[string]videoHashCacheRecord{}
	}

	trimHashCache(cache.Items)
	payload, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.hashCachePath()), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(s.hashCachePath(), payload, 0o644); err != nil {
		return
	}

	s.hashCacheMu.Lock()
	s.hashCacheLoaded = true
	s.hashCache = cache
	s.hashCacheMu.Unlock()
}

func trimHashCache(items map[string]videoHashCacheRecord) {
	if len(items) <= hashCacheMaxItems {
		return
	}

	type cacheEntry struct {
		Key   string
		Value videoHashCacheRecord
	}
	entries := make([]cacheEntry, 0, len(items))
	for key, value := range items {
		entries = append(entries, cacheEntry{Key: key, Value: value})
	}
	sort.Slice(entries, func(left int, right int) bool {
		return entries[left].Value.UpdatedAtMS < entries[right].Value.UpdatedAtMS
	})

	removeCount := len(entries) - hashCacheMaxItems
	for index := 0; index < removeCount; index++ {
		delete(items, entries[index].Key)
	}
}

func (s *Service) getCachedVideoHashes(cacheKey string) []string {
	if strings.TrimSpace(cacheKey) == "" {
		return nil
	}
	cache := s.loadHashCache()
	record, exists := cache.Items[cacheKey]
	if !exists || len(record.Hashes) == 0 {
		return nil
	}

	result := make([]string, 0, len(record.Hashes))
	for _, hash := range record.Hashes {
		hash = strings.TrimSpace(hash)
		if hash != "" {
			result = append(result, hash)
		}
	}
	return result
}

func (s *Service) setCachedVideoHashes(cacheKey string, hashes []string) {
	if strings.TrimSpace(cacheKey) == "" || len(hashes) == 0 {
		return
	}

	cache := s.loadHashCache()
	normalized := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		hash = strings.TrimSpace(hash)
		if hash != "" {
			normalized = append(normalized, hash)
		}
		if len(normalized) >= 20 {
			break
		}
	}
	if len(normalized) == 0 {
		return
	}

	if cache.Items == nil {
		cache.Items = map[string]videoHashCacheRecord{}
	}
	cache.Items[cacheKey] = videoHashCacheRecord{
		UpdatedAtMS: s.now().UnixMilli(),
		Hashes:      normalized,
	}
	s.saveHashCache(cache)
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

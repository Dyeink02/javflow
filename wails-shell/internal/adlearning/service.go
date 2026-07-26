// Package adlearning owns Go-native ad-learning state, samples, and evaluation
// helpers.
//
// Maintenance boundary:
// - persist the learning model and sample inventory
// - expose summary/update helpers
// - coordinate FFmpeg/hash-cache dependencies for evaluation flows
// - keep organizer orchestration and UI message policy outside this package
//
// Ownership summary:
// 1) expose the Go-native ad-learning facade and persisted model/sample state
// 2) keep evaluate/update/import logic behind one local service layer
// 3) coordinate FFmpeg/hash-cache dependencies without absorbing organizer orchestration
//
// File map for maintainers:
// 1) persisted model/sample contracts
// 2) model normalization/load/save helpers
// 3) service facade methods that dispatch into learning/evaluate helpers
// 4) shared FFmpeg/hash-cache state owned across those helper calls
package adlearning

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	runtimepaths "javflow/internal/runtime"
)

const (
	modelVersion       = 1
	defaultAdModelType = "mobile-net-v3-lite"
)

type Thresholds struct {
	AdScore                  int `json:"adScore"`
	HighSimilarityDistance   int `json:"highSimilarityDistance"`
	MediumSimilarityDistance int `json:"mediumSimilarityDistance"`
	LowSimilarityDistance    int `json:"lowSimilarityDistance"`
}

type Meta struct {
	ActiveModel string `json:"activeModel"`
}

type Metrics struct {
	LastLearning      any `json:"lastLearning"`
	TotalLearningRuns int `json:"totalLearningRuns"`
}

type SampleRecord struct {
	ID          string `json:"id"`
	Label       string `json:"label,omitempty"`
	SourceType  string `json:"sourceType,omitempty"`
	HashBits    string `json:"hashBits"`
	HashHex     string `json:"hashHex"`
	SourcePath  string `json:"sourcePath"`
	FrameSecond *int   `json:"frameSecond"`
	FilmCode    string `json:"filmCode"`
	AddedAt     string `json:"addedAt"`
}

type LearningModel struct {
	Version        int            `json:"version"`
	UpdatedAt      string         `json:"updatedAt"`
	Keywords       []string       `json:"keywords"`
	Thresholds     Thresholds     `json:"thresholds"`
	AdSamples      []SampleRecord `json:"adSamples"`
	NormalSamples  []SampleRecord `json:"normalSamples"`
	IntroTemplates []SampleRecord `json:"introTemplates"`
	Meta           Meta           `json:"meta"`
	Metrics        Metrics        `json:"metrics"`
}

type Summary struct {
	ModelPath          string     `json:"modelPath"`
	Version            int        `json:"version"`
	UpdatedAt          string     `json:"updatedAt"`
	KeywordCount       int        `json:"keywordCount"`
	AdSampleCount      int        `json:"adSampleCount"`
	NormalSampleCount  int        `json:"normalSampleCount"`
	IntroTemplateCount int        `json:"introTemplateCount"`
	ActiveModel        string     `json:"activeModel"`
	ActiveModelLabel   string     `json:"activeModelLabel"`
	Thresholds         Thresholds `json:"thresholds"`
	Metrics            Metrics    `json:"metrics"`
}

type UpdateModelOptions struct {
	Keywords                 []string
	AdScore                  *int
	HighSimilarityDistance   *int
	MediumSimilarityDistance *int
	LowSimilarityDistance    *int
	ModelType                string
}

type Service struct {
	paths           runtimepaths.Paths
	now             func() time.Time
	ffmpegMu        sync.Mutex
	ffmpegChecked   bool
	ffmpegAvailable bool
	ffmpegCommand   string
	hashCacheMu     sync.Mutex
	hashCacheLoaded bool
	hashCache       videoHashCacheFile
}

func NewService(paths runtimepaths.Paths) *Service {
	return &Service{
		paths: paths,
		now:   time.Now,
	}
}

func (s *Service) modelPath() string {
	return filepath.Join(s.paths.UserData, "ad-learning-model.json")
}

func defaultModel() LearningModel {
	return LearningModel{
		Version:   modelVersion,
		UpdatedAt: "",
		Keywords:  []string{},
		Thresholds: Thresholds{
			AdScore:                  60,
			HighSimilarityDistance:   10,
			MediumSimilarityDistance: 16,
			LowSimilarityDistance:    22,
		},
		AdSamples:      []SampleRecord{},
		NormalSamples:  []SampleRecord{},
		IntroTemplates: []SampleRecord{},
		Meta: Meta{
			ActiveModel: defaultAdModelType,
		},
		Metrics: Metrics{
			LastLearning:      nil,
			TotalLearningRuns: 0,
		},
	}
}

// Normalization stays centralized here so older model files and newer UI calls
// converge to one stable persisted shape before summaries/evaluation use them.
func normalizeAdModelType(rawValue string) string {
	value := strings.ToLower(strings.TrimSpace(rawValue))
	switch value {
	case "mobile-net-v3-lite", "squeezenet-fast", "yolov8n-balanced":
		return value
	default:
		return defaultAdModelType
	}
}

func getModelLabel(rawValue string) string {
	switch normalizeAdModelType(rawValue) {
	case "squeezenet-fast":
		return "SqueezeNet Fast（轻量策略）"
	case "yolov8n-balanced":
		return "YOLOv8n Balanced（轻量策略）"
	default:
		return "MobileNetV3 Lite（轻量策略）"
	}
}

func normalizeThresholdOption(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return clamp(*value, 1, 100)
}

func normalizeStoredThreshold(value int, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return clamp(value, 1, 100)
}

func clamp(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func uniqueText(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	sort.Strings(result)
	return result
}

func hashBitsToHex(bits string) string {
	normalized := strings.TrimSpace(bits)
	if normalized == "" {
		return ""
	}

	padding := len(normalized) % 4
	if padding != 0 {
		normalized += strings.Repeat("0", 4-padding)
	}

	var builder strings.Builder
	for index := 0; index < len(normalized); index += 4 {
		value, err := strconv.ParseInt(normalized[index:index+4], 2, 64)
		if err != nil {
			return ""
		}
		builder.WriteString(strings.ToLower(strconv.FormatInt(value, 16)))
	}
	return builder.String()
}

func buildIntroTemplatesFromAdSamples(adSamples []SampleRecord, now func() time.Time) []SampleRecord {
	templates := make([]SampleRecord, 0)
	seenHashes := map[string]struct{}{}

	for index, sample := range adSamples {
		hashBits := strings.TrimSpace(sample.HashBits)
		if hashBits == "" {
			continue
		}
		if _, exists := seenHashes[hashBits]; exists {
			continue
		}
		seenHashes[hashBits] = struct{}{}

		templateID := strings.TrimSpace(sample.ID)
		if templateID == "" {
			templateID = "legacy-template-" + strconv.Itoa(index+1)
		}

		addedAt := strings.TrimSpace(sample.AddedAt)
		if addedAt == "" {
			addedAt = now().Format(time.RFC3339)
		}

		templates = append(templates, SampleRecord{
			ID:          templateID,
			HashBits:    hashBits,
			HashHex:     hashBitsToHex(hashBits),
			SourcePath:  strings.TrimSpace(sample.SourcePath),
			FrameSecond: sample.FrameSecond,
			FilmCode:    strings.TrimSpace(sample.FilmCode),
			AddedAt:     addedAt,
		})
	}

	return templates
}

func ensureLearningModelShape(model LearningModel, now func() time.Time) LearningModel {
	// Shape repair is the main compatibility gate for persisted model files.
	// When ad-learning counters/samples look wrong after upgrades, start here.
	nextModel := defaultModel()

	nextModel.Version = model.Version
	if nextModel.Version == 0 {
		nextModel.Version = modelVersion
	}
	nextModel.UpdatedAt = strings.TrimSpace(model.UpdatedAt)
	nextModel.Keywords = uniqueText(model.Keywords)
	nextModel.Thresholds = Thresholds{
		AdScore:                  normalizeStoredThreshold(model.Thresholds.AdScore, nextModel.Thresholds.AdScore),
		HighSimilarityDistance:   normalizeStoredThreshold(model.Thresholds.HighSimilarityDistance, nextModel.Thresholds.HighSimilarityDistance),
		MediumSimilarityDistance: normalizeStoredThreshold(model.Thresholds.MediumSimilarityDistance, nextModel.Thresholds.MediumSimilarityDistance),
		LowSimilarityDistance:    normalizeStoredThreshold(model.Thresholds.LowSimilarityDistance, nextModel.Thresholds.LowSimilarityDistance),
	}
	nextModel.AdSamples = normalizeSamples(model.AdSamples)
	nextModel.NormalSamples = normalizeSamples(model.NormalSamples)
	nextModel.IntroTemplates = normalizeSamples(model.IntroTemplates)
	if len(nextModel.IntroTemplates) == 0 {
		nextModel.IntroTemplates = buildIntroTemplatesFromAdSamples(nextModel.AdSamples, now)
	}
	nextModel.Meta = Meta{
		ActiveModel: normalizeAdModelType(model.Meta.ActiveModel),
	}
	nextModel.Metrics = model.Metrics

	return nextModel
}

func normalizeSamples(samples []SampleRecord) []SampleRecord {
	if len(samples) == 0 {
		return []SampleRecord{}
	}
	result := make([]SampleRecord, 0, len(samples))
	for _, sample := range samples {
		hashBits := strings.TrimSpace(sample.HashBits)
		hashHex := strings.TrimSpace(sample.HashHex)
		if hashHex == "" && hashBits != "" {
			hashHex = hashBitsToHex(hashBits)
		}
		result = append(result, SampleRecord{
			ID:          strings.TrimSpace(sample.ID),
			Label:       strings.TrimSpace(sample.Label),
			SourceType:  strings.TrimSpace(sample.SourceType),
			HashBits:    hashBits,
			HashHex:     hashHex,
			SourcePath:  strings.TrimSpace(sample.SourcePath),
			FrameSecond: sample.FrameSecond,
			FilmCode:    strings.TrimSpace(sample.FilmCode),
			AddedAt:     strings.TrimSpace(sample.AddedAt),
		})
	}
	return result
}

func (s *Service) loadModel() LearningModel {
	modelPath := s.modelPath()
	contents, err := os.ReadFile(modelPath)
	if err != nil {
		return defaultModel()
	}

	loaded := LearningModel{}
	if err := json.Unmarshal(contents, &loaded); err != nil {
		return defaultModel()
	}

	return ensureLearningModelShape(loaded, s.now)
}

func (s *Service) saveModel(model LearningModel) (LearningModel, error) {
	// Save path owns the canonical persisted model rewrite so callers never patch
	// the JSON file shape directly.
	modelPath := s.modelPath()
	if err := os.MkdirAll(filepath.Dir(modelPath), 0o755); err != nil {
		return LearningModel{}, err
	}

	nextModel := ensureLearningModelShape(model, s.now)
	nextModel.UpdatedAt = s.now().Format(time.RFC3339)

	payload, err := json.MarshalIndent(nextModel, "", "  ")
	if err != nil {
		return LearningModel{}, err
	}

	if err := os.WriteFile(modelPath, payload, 0o644); err != nil {
		return LearningModel{}, err
	}

	return nextModel, nil
}

func (s *Service) summarizeModel(model LearningModel) Summary {
	// Summaries are the public read model for bridge/UI callers; keep any new
	// derived counters here instead of scattering them across consumers.
	nextModel := ensureLearningModelShape(model, s.now)
	activeModel := normalizeAdModelType(nextModel.Meta.ActiveModel)
	return Summary{
		ModelPath:          s.modelPath(),
		Version:            nextModel.Version,
		UpdatedAt:          nextModel.UpdatedAt,
		KeywordCount:       len(nextModel.Keywords),
		AdSampleCount:      len(nextModel.AdSamples),
		NormalSampleCount:  len(nextModel.NormalSamples),
		IntroTemplateCount: len(nextModel.IntroTemplates),
		ActiveModel:        activeModel,
		ActiveModelLabel:   getModelLabel(activeModel),
		Thresholds:         nextModel.Thresholds,
		Metrics:            nextModel.Metrics,
	}
}

func (s *Service) GetSummary() Summary {
	return s.summarizeModel(s.loadModel())
}

func (s *Service) UpdateModel(options UpdateModelOptions) (Summary, error) {
	model := s.loadModel()
	model.Keywords = uniqueText(append(model.Keywords, options.Keywords...))
	model.Meta = Meta{
		ActiveModel: normalizeAdModelType(firstNonEmpty(options.ModelType, model.Meta.ActiveModel)),
	}
	model.Thresholds = Thresholds{
		AdScore:                  normalizeThresholdOption(options.AdScore, model.Thresholds.AdScore),
		HighSimilarityDistance:   normalizeThresholdOption(options.HighSimilarityDistance, model.Thresholds.HighSimilarityDistance),
		MediumSimilarityDistance: normalizeThresholdOption(options.MediumSimilarityDistance, model.Thresholds.MediumSimilarityDistance),
		LowSimilarityDistance:    normalizeThresholdOption(options.LowSimilarityDistance, model.Thresholds.LowSimilarityDistance),
	}

	savedModel, err := s.saveModel(model)
	if err != nil {
		return Summary{}, err
	}

	return s.summarizeModel(savedModel), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

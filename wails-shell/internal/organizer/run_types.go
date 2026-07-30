package organizer

// run_types.go defines the Go organizer runtime data shapes shared across scan,
// transfer, review, and report phases.
//
// Ownership summary:
// 1) define pipeline data contracts for organizer execution
// 2) keep shared sink/result structs adjacent to the organizer run contract
// 3) give phase files one common vocabulary instead of ad hoc struct drift
//
// File map for maintainers:
// 1) shared log/progress sink contracts
// 2) crawl-derived preload DTOs
// 3) organizer run option/result/record structs

type LogEntry struct {
	Level     string `json:"level"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

type ProgressEntry map[string]any

type LogSink func(LogEntry)
type ProgressSink func(ProgressEntry)
type AdRiskEvaluator func(AdRiskRequest) (AdRiskResult, error)

// PreloadedExpectedCodes keeps organizer's crawl-derived inputs in one
// explicit read-only bundle. This avoids making callers maintain several
// duplicated fields that all represent the same crawl snapshot state.
type PreloadedExpectedCodes struct {
	SourceType         string      `json:"sourceType"`
	SourcePath         string      `json:"sourcePath,omitempty"`
	OutputDir          string      `json:"outputDir,omitempty"`
	FilmDataPath       string      `json:"filmDataPath,omitempty"`
	OrganizerCodesPath string      `json:"organizerCodesPath,omitempty"`
	ActressName        string      `json:"actressName,omitempty"`
	TotalRecords       int         `json:"totalRecords"`
	CodeCount          int         `json:"codeCount"`
	Codes              []string    `json:"codes"`
	CodeEntries        []CodeEntry `json:"codeEntries"`
}

// RunOptions is organizer's one execution contract.
//
// Design rules:
//   - bridge/UI callers should collapse crawl-derived expectations into
//     PreloadedExpected whenever possible
//   - filesystem/runtime callbacks stay here as function fields so the organizer
//     domain remains testable without UI dependencies
//   - compatibility payload fields are retained, but should not grow further
type RunOptions struct {
	RootPath              string `json:"rootPath"`
	MinSizeMB             int    `json:"minSizeMB"`
	Suffix                string `json:"suffix"`
	VideoExtensions       string `json:"videoExtensions"`
	AdFileAction          string `json:"adFileAction"`
	DryRun                bool   `json:"dryRun"`
	IncludeSubdirectories bool   `json:"includeSubdirectories"`
	StrictExpectedCodes   bool   `json:"strictExpectedCodes"`
	// ExpectedCodes / ExpectedCodeEntries are compatibility fields kept for
	// older callers and direct service tests. The current Wails bridge collapses
	// them into PreloadedExpected first, so one crawl snapshot travels as one
	// explicit read-only payload on the normal desktop path.
	ExpectedCodes       []string    `json:"expectedCodes"`
	ExpectedCodeEntries []CodeEntry `json:"expectedCodeEntries"`
	// CrawlOutputDir is the stable fallback input when the caller did not
	// preload expected codes yet. Organizer may read artifacts from this path,
	// but it should stay a read-only dependency on persisted crawl outputs.
	CrawlOutputDir     string                 `json:"crawlOutputDir"`
	PreloadedExpected  PreloadedExpectedCodes `json:"preloadedExpected"`
	AdDetectionEnabled bool                   `json:"adDetectionEnabled"`
	AdModelType        string                 `json:"adModelType"`
	AdThreshold        int                    `json:"adThreshold"`
	AdKeywords         string                 `json:"adKeywords"`
	AlistBaseURL       string                 `json:"alistBaseURL"`
	BatchDelete         bool                   `json:"batchDelete"`         // 是否启用批量删除
	DeleteIntervalMs    int                    `json:"deleteIntervalMs"`    // 删除间隔（毫秒）
	OrganizeIntervalMs  int                    `json:"organizeIntervalMs"`  // 整理间隔（毫秒）
	RetryMissingMagnets bool                   `json:"retryMissingMagnets"` // 是否对遗漏番号补抓磁力
	OnLog               LogSink                `json:"-"`
	OnProgress         ProgressSink           `json:"-"`
	EvaluateAdRisk     AdRiskEvaluator        `json:"-"`
}

// Summary is the operator-facing aggregate for one organizer run. Counts here
// are intentionally coarse and stable so UI/log/report consumers do not each
// invent their own totals.
type Summary struct {
	ScannedTotal          int `json:"scannedTotal"`
	VideoTotal            int `json:"videoTotal"`
	NonAdVideo            int `json:"nonAdVideo"`
	QualifiedVideo        int `json:"qualifiedVideo"`
	MatchedToCrawlCode    int `json:"matchedToCrawlCode"`
	MovedToWaiting        int `json:"movedToWaiting"`
	MovedToUnmatched      int `json:"movedToUnmatched"`
	MovedToDelete         int `json:"movedToDelete"`
	MovedToIntroAd        int `json:"movedToIntroAd"`
	DeletedDirectly       int `json:"deletedDirectly"`
	AdFileCount           int `json:"adFileCount"`
	SkippedNoCode         int `json:"skippedNoCode"`
	SkippedSmall          int `json:"skippedSmall"`
	UnmatchedVideo        int `json:"unmatchedVideo"`
	FailedOperations      int `json:"failedOperations"`
	AdRiskRejected        int `json:"adRiskRejected"`
	AdDetectionErrors     int `json:"adDetectionErrors"`
	SupplementMagnetCount int `json:"supplementMagnetCount"`
	ExpectedCodeTotal     int `json:"expectedCodeTotal"`
	DetectedCodeCount     int `json:"detectedCodeCount"`
	MissingCodeCount      int `json:"missingCodeCount"`
	MissingMagnetCount    int `json:"missingMagnetCount"`
	RemovedEmptyDirs      int `json:"removedEmptyDirs"`
}

type RunConfig struct {
	IncludeSubdirectories bool   `json:"includeSubdirectories"`
	MinSizeMB             int    `json:"minSizeMB"`
	SuffixInput           string `json:"suffixInput"`
	AdFileAction          string `json:"adFileAction"`
	StrictExpectedCodes   bool   `json:"strictExpectedCodes"`
	AdDetectionEnabled    bool   `json:"adDetectionEnabled"`
	AdModelType           string `json:"adModelType"`
	AdThreshold           int    `json:"adThreshold"`
	VideoExtensions       string `json:"videoExtensions"`
	AlistBaseURL          string `json:"alistBaseURL,omitempty"`
	BatchDelete           bool   `json:"batchDelete"`        // 是否启用批量删除
	DeleteIntervalMs      int    `json:"deleteIntervalMs"`   // 删除间隔（毫秒）
	OrganizeIntervalMs    int    `json:"organizeIntervalMs"` // 整理间隔（毫秒）
}

// RunResult is the stable API projection returned to bridge/UI callers. Keep
// preview shaping here and avoid exposing internal phase structs directly.
type RunResult struct {
	RootPath          string              `json:"rootPath"`
	DryRun            bool                `json:"dryRun"`
	Config            RunConfig           `json:"config"`
	ExpectedCodeCount int                 `json:"expectedCodeCount"`
	Summary           Summary             `json:"summary"`
	Paths             map[string]string   `json:"paths"`
	ReportMap         map[string]string   `json:"reportMap"`
	ReportFiles       []string            `json:"reportFiles"`
	Preview           PreviewResult       `json:"preview"`
	AdRisk            AdRiskSummary       `json:"adRisk"`
	MissingDownload   MissingDownloadInfo `json:"missingDownload"`
}

type NameRescueRecord struct {
	OriginalPath string `json:"originalPath"`
	TargetPath   string `json:"targetPath,omitempty"`
	FilmCode     string `json:"filmCode,omitempty"`
	Status       string `json:"status"`
	Reason       string `json:"reason,omitempty"`
}

type NameRescueResult struct {
	ScannedTotal   int                `json:"scannedTotal"`
	MatchedTotal   int                `json:"matchedTotal"`
	UnmatchedTotal int                `json:"unmatchedTotal"`
	FailedTotal    int                `json:"failedTotal"`
	WaitingDir     string             `json:"waitingDir"`
	UnmatchedDir   string             `json:"unmatchedDir"`
	ReportPath     string             `json:"reportPath"`
	Records        []NameRescueRecord `json:"records"`
}

type PreviewResult struct {
	RenameRecords    []RenameRecord    `json:"renameRecords"`
	UnmatchedRecords []UnmatchedRecord `json:"unmatchedRecords"`
	AdRiskRecords    []AdRiskRecord    `json:"adRiskRecords"`
}

type AdRiskSummary struct {
	RiskCodeCount         int      `json:"riskCodeCount"`
	SupplementMagnetCount int      `json:"supplementMagnetCount"`
	RiskCodes             []string `json:"riskCodes"`
}

type MissingDownloadInfo struct {
	MissingCodeCount   int      `json:"missingCodeCount"`
	MissingMagnetCount int      `json:"missingMagnetCount"`
	MissingCodes       []string `json:"missingCodes"`
}

type FileEntry struct {
	Path         string `json:"path"`
	RelativePath string `json:"relativePath"`
	TopDirName   string `json:"topDirName"`
	IsRootLevel  bool   `json:"isRootLevel"`
	IsVideo      bool   `json:"isVideo"`
}

type Candidate struct {
	Src                 string `json:"src"`
	Size                int64  `json:"size"`
	IsVideo             bool   `json:"isVideo"`
	IsRootLevel         bool   `json:"isRootLevel"`
	FilmCode            string `json:"filmCode"`
	RenameByFilmCode    bool   `json:"renameByFilmCode"`
	KeepOriginalReason  string `json:"keepOriginalReason"`
	ExpectedCodeMatched bool   `json:"expectedCodeMatched"`
}

type RenameRecord struct {
	OriginalName        string `json:"originalName"`
	OriginalPath        string `json:"originalPath"`
	WaitingPath         string `json:"waitingPath"`
	NewName             string `json:"newName"`
	FilmCode            string `json:"filmCode"`
	RenameApplied       bool   `json:"renameApplied"`
	Note                string `json:"note"`
	ExpectedCodeMatched bool   `json:"expectedCodeMatched"`
	Size                int64  `json:"size"`
}

type UnmatchedRecord struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	IsVideo bool   `json:"isVideo"`
	Reason  string `json:"reason"`
}

type AdRiskRequest struct {
	VideoPath   string `json:"videoPath"`
	StreamURL   string `json:"streamURL,omitempty"`
	FilmCode    string `json:"filmCode"`
	AdThreshold int    `json:"adThreshold"`
	ModelType   string `json:"modelType,omitempty"`
}

type AdRiskResult struct {
	IsAd      bool           `json:"isAd"`
	Score     float64        `json:"score"`
	Threshold float64        `json:"threshold"`
	Reasons   []string       `json:"reasons"`
	Evidence  map[string]any `json:"evidence"`
}

type AdRiskRecord struct {
	FilmCode   string         `json:"filmCode"`
	SourcePath string         `json:"sourcePath"`
	Size       int64          `json:"size"`
	Score      float64        `json:"score"`
	Threshold  float64        `json:"threshold"`
	Reasons    []string       `json:"reasons"`
	Evidence   map[string]any `json:"evidence"`
}

package organizer

import (
	"path/filepath"
	"strings"

	"javflow/internal/contracts/crawlartifact"
)

// paths.go owns organizer-managed directory names and report/artifact path
// helpers under one organizer root.
//
// Ownership summary:
// 1) define organizer-managed directory/report path conventions
// 2) keep organizer path layout centralized under one root contract
// 3) separate path naming from organizer execution phases
//
// File map for maintainers:
// 1) organizer-managed path/file constants
// 2) path DTOs and root-layout contracts
// 3) path derivation helpers for reports, state, and managed directories

const (
	waitingDirName           = "\u5f85\u6574\u7406"
	unmatchedDirName         = "\u672a\u547d\u4e2d"
	toDeleteDirName          = "\u5f85\u5220\u9664"
	introAdDirName           = "\u542b\u5f00\u5934\u5e7f\u544a"
	logsDirName              = "logs"
	stateDirName             = ".video-organizer-state"
	batchDeleteStagingPrefix = ".video-organizer-batch-delete-"
	renameMapName            = "\u66f4\u6539\u524d\u540e\u5bf9\u7167.txt"
	unmatchedName            = "\u672a\u8bc6\u522b\u756a\u53f7\u89c6\u9891.txt"
	adRiskCodesName          = "\u542b\u5f00\u5934\u5e7f\u544a\u756a\u53f7.txt"
	adRiskDetailName         = "\u542b\u5f00\u5934\u5e7f\u544a\u660e\u7ec6.txt"
	adRiskMagnetsName        = "\u542b\u5f00\u5934\u5e7f\u544a\u8865\u6293\u78c1\u529b.txt"
	missingMagnetsName       = "\u9057\u6f0f\u756a\u53f7\u78c1\u529b\u8865\u6293.txt"
	rescueReportName         = "番号名称抢救报告.txt"
	renameHistoryName        = "rename-history.jsonl"
)

// Paths defines the organizer-owned directory and report layout under one root
// path. Path rules live here so future output changes do not require scanning
// crawl parsing or organizer execution code.
type Paths struct {
	RootPath           string `json:"rootPath"`
	WaitingDir         string `json:"waitingDir"`
	UnmatchedDir       string `json:"unmatchedDir"`
	ToDeleteDir        string `json:"toDeleteDir"`
	IntroAdDir         string `json:"introAdDir"`
	LogsDir            string `json:"logsDir"`
	StateDir           string `json:"stateDir"`
	RenameMapPath      string `json:"renameMapPath"`
	UnmatchedPath      string `json:"unmatchedPath"`
	AdRiskCodesPath    string `json:"adRiskCodesPath"`
	AdRiskDetailPath   string `json:"adRiskDetailPath"`
	AdRiskMagnetsPath  string `json:"adRiskMagnetsPath"`
	MissingMagnetsPath string `json:"missingMagnetsPath"`
	RescueReportPath   string `json:"rescueReportPath"`
	RenameHistoryPath  string `json:"renameHistoryPath"`
}

func (s *Service) ResolvePaths(rootPath string) Paths {
	normalizedRootPath := crawlartifact.NormalizeRootPath(rootPath)

	return Paths{
		RootPath:           normalizedRootPath,
		WaitingDir:         filepath.Join(normalizedRootPath, waitingDirName),
		UnmatchedDir:       filepath.Join(normalizedRootPath, unmatchedDirName),
		ToDeleteDir:        filepath.Join(normalizedRootPath, toDeleteDirName),
		IntroAdDir:         filepath.Join(normalizedRootPath, introAdDirName),
		LogsDir:            filepath.Join(normalizedRootPath, logsDirName),
		StateDir:           filepath.Join(normalizedRootPath, stateDirName),
		RenameMapPath:      filepath.Join(normalizedRootPath, renameMapName),
		UnmatchedPath:      filepath.Join(normalizedRootPath, unmatchedName),
		AdRiskCodesPath:    filepath.Join(normalizedRootPath, adRiskCodesName),
		AdRiskDetailPath:   filepath.Join(normalizedRootPath, adRiskDetailName),
		AdRiskMagnetsPath:  filepath.Join(normalizedRootPath, adRiskMagnetsName),
		MissingMagnetsPath: filepath.Join(normalizedRootPath, missingMagnetsName),
		RescueReportPath:   filepath.Join(normalizedRootPath, rescueReportName),
		RenameHistoryPath:  filepath.Join(normalizedRootPath, stateDirName, renameHistoryName),
	}
}

func (s *Service) ResolveTargetPath(rootPath string, kind string) string {
	paths := s.ResolvePaths(rootPath)
	switch strings.TrimSpace(kind) {
	case "waiting":
		return paths.WaitingDir
	case "unmatched":
		return paths.UnmatchedDir
	case "delete":
		return paths.ToDeleteDir
	case "intro-ad":
		return paths.IntroAdDir
	case "logs":
		return paths.LogsDir
	case "reports", "root", "":
		return paths.RootPath
	default:
		return paths.RootPath
	}
}

func (s *Service) ResolveCrawlOutputPaths(outputDir string) crawlartifact.CrawlOutputPaths {
	return crawlartifact.ResolveCrawlOutputPaths(outputDir)
}

// BatchDeleteWhitelist 返回批量删除白名单（需要保留的文件/文件夹名）
func (p Paths) BatchDeleteWhitelist() map[string]struct{} {
	return map[string]struct{}{
		waitingDirName:     {}, // 待整理
		unmatchedDirName:   {}, // 未命中
		toDeleteDirName:    {}, // 待删除
		introAdDirName:     {}, // 含开头广告
		logsDirName:        {}, // logs
		stateDirName:       {}, // .video-organizer-state
		renameMapName:      {}, // 更改前后对照.txt
		unmatchedName:      {}, // 未识别番号视频.txt
		adRiskCodesName:    {}, // 含开头广告番号.txt
		adRiskDetailName:   {}, // 含开头广告明细.txt
		adRiskMagnetsName:  {}, // 含开头广告补抓磁力.txt
		missingMagnetsName: {}, // 遗漏番号磁力补抓.txt
		rescueReportName:   {}, // 番号名称抢救报告.txt

		// Crawl artifacts may be placed beside the media tree when the user
		// chooses a broad download directory as the organizer root. They are
		// application output, never organizer deletion candidates.
		crawlartifact.CrawlFilmDataFile:        {},
		crawlartifact.CrawlProfileFile:         {},
		crawlartifact.OrganizerCodesFile:       {},
		crawlartifact.FilteredFilmCodesFile:    {},
		crawlartifact.DefaultMagnetTxt:         {},
		crawlartifact.DefaultFilteredCodesTxt:  {},
		crawlartifact.DefaultLatestLogTxt:      {},
		crawlartifact.DefaultUnfinishedTxt:     {},
		crawlartifact.DefaultQualitySummaryTxt: {},
		"log":                                  {},
		"AV订阅":                                 {},
		"JAV爬虫":                                {},
		"媒体库刮削":                                {},
	}
}

// IsBatchDeleteWhitelisted 检查文件/文件夹是否在批量删除白名单中
func (p Paths) IsBatchDeleteWhitelisted(name string) bool {
	whitelist := p.BatchDeleteWhitelist()
	for candidate := range whitelist {
		if strings.EqualFold(strings.TrimSpace(candidate), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

// Actress Atlas crawl-output handoff owns the safe output path used when a
// performer is sent from Actor Atlas to the JAV crawler.
//
// Ownership summary:
// 1) select a reusable user output root without trusting source/app paths
// 2) append one sanitized performer directory for collision isolation
// 3) keep output-path policy out of the renderer and crawl request pipeline
//
// File map for maintainers:
// 1) Actor Atlas output result contract
// 2) reusable-root and hidden-fallback selection
// 3) path containment and performer-folder sanitization helpers
package bridge

import (
	"fmt"
	"path/filepath"
	"strings"
)

const actorAtlasHiddenCrawlRootDirName = "actor-atlas-crawl"

type actressCrawlOutputResult struct {
	Output string `json:"output"`
	Root   string `json:"root"`
	Source string `json:"source"`
}

// resolveActressCrawlerOutput returns a future crawl directory only. It does
// not create a folder when a user merely views an actress, so canceled handoffs
// do not leave empty directories behind.
func (a *API) resolveActressCrawlerOutput(payload map[string]any) (actressCrawlOutputResult, error) {
	actressName := sanitizeActorAtlasOutputName(nonEmptyString(payload["actressName"]))
	if actressName == "" {
		return actressCrawlOutputResult{}, fmt.Errorf("actor name is required")
	}

	root, source := a.resolveActressCrawlerOutputRoot()
	if root == "" {
		return actressCrawlOutputResult{}, fmt.Errorf("cannot resolve actress crawl output root")
	}
	return actressCrawlOutputResult{
		Output: filepath.Join(root, actressName),
		Root:   root,
		Source: source,
	}, nil
}

func (a *API) resolveActressCrawlerOutputRoot() (string, string) {
	settings := a.loadBridgeSettingsSnapshot()
	candidate := strings.TrimSpace(nonEmptyString(settings["output"]))
	if isReusableActorAtlasOutputRoot(candidate, a.runtime.paths.AppPath, a.runtime.paths.Documents) {
		return filepath.Clean(candidate), "last-user-output"
	}

	userData := strings.TrimSpace(a.runtime.paths.UserData)
	if userData == "" {
		return "", ""
	}
	// UserData is the application-private roaming path. Keeping first-run
	// actor crawls here prevents collision with the user's visible exports.
	return filepath.Join(filepath.Clean(userData), actorAtlasHiddenCrawlRootDirName), "internal-hidden"
}

func isReusableActorAtlasOutputRoot(candidate string, appPath string, documentsPath string) bool {
	cleanedCandidate := strings.TrimSpace(candidate)
	if cleanedCandidate == "" {
		return false
	}
	if pathIsSameOrChild(cleanedCandidate, appPath) {
		return false
	}

	// A newly installed app receives this settings default even before the user
	// chooses an export location. Treat it as first-run rather than claiming it
	// is the user's previous output folder.
	firstRunDefault := filepath.Join(strings.TrimSpace(documentsPath), "JAV自动化爬虫工具输出")
	return !sameCleanPath(cleanedCandidate, firstRunDefault)
}

func pathIsSameOrChild(candidate string, parent string) bool {
	if strings.TrimSpace(candidate) == "" || strings.TrimSpace(parent) == "" {
		return false
	}
	relative, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(candidate))
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func sameCleanPath(left string, right string) bool {
	if strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

func sanitizeActorAtlasOutputName(value string) string {
	cleaned := strings.TrimSpace(value)
	cleaned = strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	).Replace(cleaned)
	cleaned = strings.TrimRight(cleaned, ". ")
	if cleaned == "" {
		return ""
	}
	return cleaned
}

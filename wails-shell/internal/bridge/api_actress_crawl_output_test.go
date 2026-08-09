package bridge

import (
	"path/filepath"
	"testing"

	runtimepaths "javflow/internal/runtime"
	"javflow/internal/settings"
)

func newActressCrawlOutputTestAPI(t *testing.T) (*API, runtimepaths.Paths) {
	t.Helper()
	root := t.TempDir()
	paths := runtimepaths.Paths{
		AppPath:   filepath.Join(root, "app"),
		UserData:  filepath.Join(root, "user-data"),
		Documents: filepath.Join(root, "documents"),
	}
	return &API{runtime: runtimeFacade{paths: paths, store: settings.NewStore(paths)}}, paths
}

func TestResolveActressCrawlerOutputUsesHiddenPathForFirstRun(t *testing.T) {
	api, paths := newActressCrawlOutputTestAPI(t)

	result, err := api.resolveActressCrawlerOutput(map[string]any{"actressName": "三上悠亜"})
	if err != nil {
		t.Fatalf("resolve output: %v", err)
	}
	if result.Source != "internal-hidden" {
		t.Fatalf("source = %q, want internal-hidden", result.Source)
	}
	want := filepath.Join(paths.UserData, actorAtlasHiddenCrawlRootDirName, "三上悠亜")
	if result.Output != want {
		t.Fatalf("output = %q, want %q", result.Output, want)
	}
}

func TestResolveActressCrawlerOutputReusesExternalUserRoot(t *testing.T) {
	api, paths := newActressCrawlOutputTestAPI(t)
	externalRoot := filepath.Join(filepath.Dir(paths.AppPath), "exports")
	if err := api.runtime.store.Save(map[string]any{"output": externalRoot}); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	result, err := api.resolveActressCrawlerOutput(map[string]any{"actressName": "瀬戸環奈"})
	if err != nil {
		t.Fatalf("resolve output: %v", err)
	}
	if result.Source != "last-user-output" {
		t.Fatalf("source = %q, want last-user-output", result.Source)
	}
	want := filepath.Join(externalRoot, "瀬戸環奈")
	if result.Output != want {
		t.Fatalf("output = %q, want %q", result.Output, want)
	}
}

func TestResolveActressCrawlerOutputRejectsAppPath(t *testing.T) {
	api, paths := newActressCrawlOutputTestAPI(t)
	if err := api.runtime.store.Save(map[string]any{"output": filepath.Join(paths.AppPath, "JAV爬虫")}); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	result, err := api.resolveActressCrawlerOutput(map[string]any{"actressName": "小宵こなん"})
	if err != nil {
		t.Fatalf("resolve output: %v", err)
	}
	if result.Source != "internal-hidden" {
		t.Fatalf("source = %q, want internal-hidden", result.Source)
	}
	if pathIsSameOrChild(result.Output, paths.AppPath) {
		t.Fatalf("output %q must not stay below app path %q", result.Output, paths.AppPath)
	}
}

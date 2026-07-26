package bridge

import (
	"testing"

	runtimepaths "javflow/internal/runtime"
	"javflow/internal/settings"
)

func TestMutateBridgeSettingsPersistsChanges(t *testing.T) {
	tempDir := t.TempDir()
	api := &API{
		runtime: runtimeFacade{
			store: settings.NewStore(runtimepaths.Paths{
				UserData:  tempDir,
				Documents: tempDir,
			}),
		},
	}

	updated, err := api.mutateBridgeSettings(func(currentSettings map[string]any) {
		currentSettings["proxy"] = "http://127.0.0.1:7890"
		currentSettings["backgroundImage"] = "C:\\images\\bg.png"
	})
	if err != nil {
		t.Fatalf("mutateBridgeSettings returned error: %v", err)
	}

	if got := nonEmptyString(updated["proxy"]); got != "http://127.0.0.1:7890" {
		t.Fatalf("updated proxy = %q, want proxy to be persisted in returned map", got)
	}

	loaded, err := api.runtime.store.Load()
	if err != nil {
		t.Fatalf("load settings after mutate returned error: %v", err)
	}

	if got := nonEmptyString(loaded["proxy"]); got != "http://127.0.0.1:7890" {
		t.Fatalf("saved proxy = %q, want persisted proxy", got)
	}
	if got := nonEmptyString(loaded["backgroundImage"]); got != "C:\\images\\bg.png" {
		t.Fatalf("saved backgroundImage = %q, want persisted background image", got)
	}
}

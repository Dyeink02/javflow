package bridge

import (
	"strconv"
	"sync"
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

func TestMutateBridgeSettingsKeepsConcurrentWorkspaceFields(t *testing.T) {
	tempDir := t.TempDir()
	api := &API{
		runtime: runtimeFacade{
			store: settings.NewStore(runtimepaths.Paths{UserData: tempDir, Documents: tempDir}),
		},
	}

	const workers = 16
	var waitGroup sync.WaitGroup
	waitGroup.Add(workers)
	for index := 0; index < workers; index++ {
		index := index
		go func() {
			defer waitGroup.Done()
			key := "workspace-test-" + strconv.Itoa(index)
			if _, err := api.mutateBridgeSettings(func(currentSettings map[string]any) {
				currentSettings[key] = true
			}); err != nil {
				t.Errorf("concurrent mutation %s failed: %v", key, err)
			}
		}()
	}
	waitGroup.Wait()

	loaded, err := api.runtime.store.Load()
	if err != nil {
		t.Fatalf("load settings after concurrent mutations: %v", err)
	}
	for index := 0; index < workers; index++ {
		key := "workspace-test-" + strconv.Itoa(index)
		if loaded[key] != true {
			t.Fatalf("concurrent mutation lost field %s: %#v", key, loaded[key])
		}
	}
}

func TestApplyCrawlerSettingsPayloadDefaultsMagnetContentValidationToDisabled(t *testing.T) {
	settings := map[string]any{}
	applyCrawlerSettingsPayload(settings, map[string]any{})

	if got := settings["magnetContentValidation"]; got != false {
		t.Fatalf("missing magnetContentValidation = %v, want false", got)
	}
}

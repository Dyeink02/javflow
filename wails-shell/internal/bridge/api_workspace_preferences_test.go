package bridge

import (
	"encoding/json"
	"testing"

	runtimepaths "javflow/internal/runtime"
	"javflow/internal/settings"
)

func TestSaveWorkspacePreferencesPersistsPartialUpdates(t *testing.T) {
	tempDir := t.TempDir()
	api := &API{
		runtime: runtimeFacade{
			store: settings.NewStore(runtimepaths.Paths{
				UserData:  tempDir,
				Documents: tempDir,
			}),
		},
	}

	result, handled, err := api.handleRuntimeBootstrapCommand("app:save-workspace-preferences", map[string]any{
		"libraryCompactLayout":             true,
		"libraryShowHiddenFiles":           false,
		"libraryScrapeConcurrency":         99,
		"libraryAutoSubscribeFromOutput":   true,
		"organizerAutoSubscribeFromOutput": true,
	})
	if err != nil {
		t.Fatalf("save workspace preferences: %v", err)
	}
	if !handled {
		t.Fatal("workspace preferences command was not handled")
	}

	response := map[string]any{}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("decode workspace preferences response: %v", err)
	}
	if got := intValue(response["libraryScrapeConcurrency"], 0); got != 5 {
		t.Fatalf("response concurrency = %d, want clamped value 5", got)
	}

	_, handled, err = api.handleRuntimeBootstrapCommand("app:save-workspace-preferences", map[string]any{
		"libraryAutoSubscribeFromOutput": false,
	})
	if err != nil || !handled {
		t.Fatalf("save partial workspace preferences: handled=%v err=%v", handled, err)
	}

	loaded, err := api.runtime.store.Load()
	if err != nil {
		t.Fatalf("load saved workspace preferences: %v", err)
	}
	if got := intValue(loaded["libraryScrapeConcurrency"], 0); got != 5 {
		t.Fatalf("saved concurrency = %d, want retained clamped value 5", got)
	}
	if got := boolValue(loaded["libraryCompactLayout"], false); !got {
		t.Fatal("saved compact layout = false, want true")
	}
	if got := boolValue(loaded["libraryShowHiddenFiles"], true); got {
		t.Fatal("saved hidden-files preference = true, want false")
	}
	if got := boolValue(loaded["libraryAutoSubscribeFromOutput"], true); got {
		t.Fatal("saved library auto-subscribe = true, want false after partial update")
	}
	if got := boolValue(loaded["organizerAutoSubscribeFromOutput"], false); !got {
		t.Fatal("saved organizer auto-subscribe = false, want true")
	}
	if got := intValue(loaded["libraryWorkspacePreferencesVersion"], 0); got != 1 {
		t.Fatalf("library preference version = %d, want 1", got)
	}
	if got := intValue(loaded["organizerWorkspacePreferencesVersion"], 0); got != 1 {
		t.Fatalf("organizer preference version = %d, want 1", got)
	}
}

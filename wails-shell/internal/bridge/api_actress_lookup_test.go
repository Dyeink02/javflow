package bridge

import (
	"encoding/json"
	"strings"
	"testing"

	"javflow/internal/actresslookup"
)

func TestResolveActressAliasPrefersUniqueLocalAlias(t *testing.T) {
	api := &API{lookup: lookupFacade{actressLookup: actresslookup.NewService()}}
	raw, handled, err := api.handleLookupTargetCommand("app:resolve-actress-alias", map[string]any{"actorName": "相泽南"})
	if err != nil {
		t.Fatalf("resolve local alias: %v", err)
	}
	if !handled {
		t.Fatal("expected actress alias command to be handled")
	}

	var result struct {
		Name     string   `json:"name"`
		Aliases  []string `json:"aliases"`
		Provider string   `json:"provider"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decode local alias result: %v", err)
	}
	if result.Name != "相沢みなみ" || result.Provider != "local-alias-index" {
		t.Fatalf("unexpected local alias result: %+v", result)
	}
	if len(result.Aliases) < 2 || result.Aliases[0] != "相泽南" {
		t.Fatalf("unexpected local alias evidence: %+v", result.Aliases)
	}
}

func TestResolveActressAliasRequiresMetadataServiceAfterLocalMiss(t *testing.T) {
	api := &API{lookup: lookupFacade{actressLookup: actresslookup.NewService()}}
	_, handled, err := api.handleLookupTargetCommand("app:resolve-actress-alias", map[string]any{"actorName": "不存在的演员"})
	if !handled {
		t.Fatal("expected actress alias command to be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "actor metadata service is not initialized") {
		t.Fatalf("expected metadata-service boundary error after local miss, got %v", err)
	}
}

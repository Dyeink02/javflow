package bridge

import (
	"path/filepath"
	"testing"
)

func TestSameLibraryMetadataActressNormalizesSpacing(t *testing.T) {
	if !sameLibraryMetadataActress("松本 いちか", "松本いちか") {
		t.Fatal("expected normalized actress names to match")
	}
	if sameLibraryMetadataActress("松本いちか", "深田えいみ") {
		t.Fatal("different actress names must not match")
	}
}

func TestAutoSubscriptionOutputDirUsesSiblingForDifferentActress(t *testing.T) {
	current := filepath.Join(t.TempDir(), "新松本いちか")
	if got := autoSubscriptionOutputDir(current, "松本いちか"); got != current {
		t.Fatalf("matching output should be reused, got %s", got)
	}
	want := filepath.Join(filepath.Dir(current), "深田えいみ")
	if got := autoSubscriptionOutputDir(current, "深田えいみ"); got != want {
		t.Fatalf("expected sibling output %s, got %s", want, got)
	}
}

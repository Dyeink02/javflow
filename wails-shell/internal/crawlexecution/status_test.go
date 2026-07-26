package crawlexecution

import "testing"

func TestNormalizeStatusFallsBackToIdle(t *testing.T) {
	if got := NormalizeStatus("", ""); got != "idle" {
		t.Fatalf("expected idle, got %q", got)
	}
}

func TestStatusLabel(t *testing.T) {
	if got := StatusLabel("running"); got != "运行中" {
		t.Fatalf("unexpected label: %q", got)
	}
}

func TestIsActiveStatus(t *testing.T) {
	if !IsActiveStatus("starting") {
		t.Fatal("expected starting to be active")
	}
	if IsActiveStatus("completed") {
		t.Fatal("expected completed to be inactive")
	}
}

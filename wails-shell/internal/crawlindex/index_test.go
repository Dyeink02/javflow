package crawlindex

import "testing"

func TestBuildIndexPageURL(t *testing.T) {
	if got := BuildIndexPageURL("https://www.javbus.com/star/okq", "", "", 3); got != "https://www.javbus.com/star/okq/3" {
		t.Fatalf("unexpected star page url: %q", got)
	}

	if got := BuildIndexPageURL("https://www.javbus.com", "", "", 2); got != "https://www.javbus.com/page/2" {
		t.Fatalf("unexpected root page url: %q", got)
	}
}

func TestNormalizeDetailLink(t *testing.T) {
	got := NormalizeDetailLink("https://www.javbus.com/ABP-889/?foo=1")
	if got != "https://www.javbus.com/abp-889?foo=1" {
		t.Fatalf("unexpected normalized detail link: %q", got)
	}
}

func TestAnalyzePageLinkDuplicates(t *testing.T) {
	analysis := AnalyzePageLinkDuplicates([]string{
		"https://www.javbus.com/ABP-889",
		"https://www.javbus.com/abp889",
		"https://www.javbus.com/SSIS-123",
	})

	if analysis.UniqueCount != 2 {
		t.Fatalf("expected unique count 2, got %d", analysis.UniqueCount)
	}
	if analysis.DuplicateCount != 1 {
		t.Fatalf("expected duplicate count 1, got %d", analysis.DuplicateCount)
	}
	if len(analysis.DuplicateIDs) != 1 || analysis.DuplicateIDs[0] != "ABP-889" {
		t.Fatalf("unexpected duplicate ids: %#v", analysis.DuplicateIDs)
	}
}

func TestGetTrackedPageLinks(t *testing.T) {
	links := []string{"a", "b", "c", "d"}
	tracked := GetTrackedPageLinks(links, 5, 3)
	if len(tracked) != 2 || tracked[0] != "a" || tracked[1] != "b" {
		t.Fatalf("unexpected tracked links: %#v", tracked)
	}
}

func TestResolveIndexTargetPageState(t *testing.T) {
	expectedItemsPerPage := 30
	state := ResolveIndexTargetPageState(3, 0, 65, &expectedItemsPerPage)

	if state.InferredTotalPages != 3 {
		t.Fatalf("expected inferred total pages 3, got %d", state.InferredTotalPages)
	}
	if state.TargetTotalPages != 3 {
		t.Fatalf("expected target total pages 3, got %d", state.TargetTotalPages)
	}
	if !state.IsLastTargetPage {
		t.Fatal("expected current page to be treated as last target page")
	}
}

func TestResolveIndexQueueLimitDecision(t *testing.T) {
	decision := ResolveIndexQueueLimitDecision(10, 8, 5)

	if decision.QueueCount != 2 {
		t.Fatalf("expected queue count 2, got %d", decision.QueueCount)
	}
	if decision.RemainingSlots != 2 {
		t.Fatalf("expected remaining slots 2, got %d", decision.RemainingSlots)
	}
	if decision.ShouldStopBeforeQueue {
		t.Fatal("did not expect pre-queue stop")
	}
	if !decision.ShouldStopAfterQueue {
		t.Fatal("expected stop after queue when limit is reached")
	}
}

func TestResolveIndexProcessingDecisionContinueAfterGap(t *testing.T) {
	expectedCount := 30
	decision := ResolveIndexProcessingDecision(IndexProcessingDecisionInput{
		CurrentPage:      2,
		TargetTotalPages: 4,
		ExpectedCount:    &expectedCount,
		LinksCount:       0,
		NewLinksCount:    0,
		ResumeExisting:   false,
		FilmLimit:        0,
		FilmsQueued:      0,
	})

	if decision.Action != "continue_after_gap" {
		t.Fatalf("expected continue_after_gap, got %q", decision.Action)
	}
	if !decision.ShouldAdvancePage || decision.ShouldStopIndexing {
		t.Fatalf("unexpected decision flags: %#v", decision)
	}
}

func TestResolveIndexProcessingDecisionResumeCompletedPage(t *testing.T) {
	decision := ResolveIndexProcessingDecision(IndexProcessingDecisionInput{
		CurrentPage:      5,
		TargetTotalPages: 0,
		ExpectedCount:    nil,
		LinksCount:       20,
		NewLinksCount:    0,
		ResumeExisting:   true,
		FilmLimit:        0,
		FilmsQueued:      0,
	})

	if decision.Action != "continue_resume_completed_page" {
		t.Fatalf("expected continue_resume_completed_page, got %q", decision.Action)
	}
	if !decision.ShouldAdvancePage || decision.ShouldStopIndexing {
		t.Fatalf("unexpected decision flags: %#v", decision)
	}
}

func TestResolveIndexProcessingDecisionStopTargetPageReached(t *testing.T) {
	decision := ResolveIndexProcessingDecision(IndexProcessingDecisionInput{
		CurrentPage:      3,
		TargetTotalPages: 3,
		ExpectedCount:    nil,
		LinksCount:       20,
		NewLinksCount:    5,
		ResumeExisting:   false,
		FilmLimit:        100,
		FilmsQueued:      25,
	})

	if decision.Action != "stop_target_page_reached" {
		t.Fatalf("expected stop_target_page_reached, got %q", decision.Action)
	}
	if !decision.ShouldAdvancePage || !decision.ShouldStopIndexing {
		t.Fatalf("unexpected decision flags: %#v", decision)
	}
}

func TestShouldWarnSparseIndexPage(t *testing.T) {
	expectedItemsPerPage := 30
	if !ShouldWarnSparseIndexPage(false, &expectedItemsPerPage, false, 12) {
		t.Fatal("expected sparse page warning to be enabled")
	}
	if ShouldWarnSparseIndexPage(true, &expectedItemsPerPage, false, 12) {
		t.Fatal("did not expect sparse warning when indexing already stopping")
	}
}

package crawltaskstate

import (
	"time"

	"javflow/internal/crawlexecution"
)

// types.go owns the persisted state contract shared across resume, validation,
// and reconciliation/reporting flows.
//
// Keep these shapes stable so resume data, validation reports, and diagnostic
// summaries can move together without each caller inventing its own schema.
//
// Practical split:
// - this file is the schema surface for the task-state snapshot
// - `manager.go` owns where the snapshot lives
// - `persisted_output.go` owns the persisted-output read model used by review
//
// Ownership summary:
// 1) define the persisted task-state schema shared across resume/validation/reporting
// 2) keep snapshot contracts stable across runner and review consumers
// 3) separate schema definition from path resolution and persisted-output inspection
//
// File map for maintainers:
// 1) persisted snapshot/report schema DTOs
// 2) page audit, failed-detail, and validation record shapes
// 3) builder/restore shared state contract types

type PageAuditRecord struct {
	PageNumber       int     `json:"pageNumber"`
	URL              string  `json:"url"`
	ExpectedCount    *int    `json:"expectedCount"`
	ActualCount      int     `json:"actualCount"`
	RetryCount       int     `json:"retryCount"`
	ValidationPassed bool    `json:"validationPassed"`
	ConfidenceScore  float64 `json:"confidenceScore"`
	Confidence       string  `json:"confidence"`
	Reason           string  `json:"reason"`
	UpdatedAt        string  `json:"updatedAt"`
}

// ResultValidationReport is the post-run consistency snapshot. It explains how
// far the persisted crawl output diverged from the expected detail set after
// queueing, processing, and second-pass validation have all finished.
type ResultValidationReport struct {
	GeneratedAt                   string   `json:"generatedAt"`
	TotalRecords                  int      `json:"totalRecords"`
	UniqueRecords                 int      `json:"uniqueRecords"`
	DuplicateCount                int      `json:"duplicateCount"`
	InvalidRecordCount            int      `json:"invalidRecordCount"`
	ExpectedItemCount             int      `json:"expectedItemCount"`
	PersistedItemCount            int      `json:"persistedItemCount"`
	MissingFromQueueCount         int      `json:"missingFromQueueCount"`
	ExpectedButNotQueuedCount     int      `json:"expectedButNotQueuedCount"`
	ProcessedButNotPersistedCount int      `json:"processedButNotPersistedCount"`
	UniqueMagnetCount             int      `json:"uniqueMagnetCount"`
	LowConfidencePageCount        int      `json:"lowConfidencePageCount"`
	MissingItems                  []string `json:"missingItems"`
	ExpectedButNotQueuedItems     []string `json:"expectedButNotQueuedItems"`
	ProcessedButNotPersistedItems []string `json:"processedButNotPersistedItems"`
	LowConfidencePages            []int    `json:"lowConfidencePages"`
	Passed                        bool     `json:"passed"`
	Summary                       string   `json:"summary"`
}

type FailedDetailRecord struct {
	Item         string `json:"item,omitempty"`
	SourceLink   string `json:"sourceLink,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Category     string `json:"category,omitempty"`
	RetryCount   int    `json:"retryCount,omitempty"`
	RetryAdvice  string `json:"retryAdvice,omitempty"`
	Recoverable  *bool  `json:"recoverable,omitempty"`
	LastFailedAt string `json:"lastFailedAt,omitempty"`
}

type DuplicateExpectedEntryGroup struct {
	ItemID string   `json:"itemId"`
	Links  []string `json:"links"`
}

// ReconciliationSnapshot captures the ID-level set math behind "missing",
// "not queued", "not persisted", and duplicate expectations. It is optional in
// light snapshots, but when present it gives restore/review code enough detail
// to explain why a run ended incomplete.
type ReconciliationSnapshot struct {
	ExpectedIDs                 []string                      `json:"expectedIds"`
	QueuedIDs                   []string                      `json:"queuedIds"`
	ProcessedIDs                []string                      `json:"processedIds"`
	PersistedIDs                []string                      `json:"persistedIds"`
	ExpectedButNotQueuedIDs     []string                      `json:"expectedButNotQueuedIds"`
	ExpectedButNotPersistedIDs  []string                      `json:"expectedButNotPersistedIds"`
	ProcessedButNotPersistedIDs []string                      `json:"processedButNotPersistedIds"`
	DuplicateExpectedIDs        []string                      `json:"duplicateExpectedIds"`
	ExpectedEntryCount          int                           `json:"expectedEntryCount,omitempty"`
	RawDuplicateEntryCount      int                           `json:"rawDuplicateEntryCount,omitempty"`
	RawDuplicateGroups          []DuplicateExpectedEntryGroup `json:"rawDuplicateGroups,omitempty"`
}

type SnapshotConfig struct {
	Base             *string `json:"base"`
	Output           string  `json:"output"`
	Limit            int     `json:"limit"`
	TotalPages       int     `json:"totalPages"`
	ItemsPerPage     int     `json:"itemsPerPage"`
	Parallel         int     `json:"parallel"`
	Delay            int     `json:"delay"`
	Timeout          int     `json:"timeout"`
	SecondValidation bool    `json:"secondValidation"`
	TaskTemplate     string  `json:"taskTemplate"`
}

type SnapshotProgress struct {
	NextPageIndex        int  `json:"nextPageIndex"`
	ExpectedItemsPerPage *int `json:"expectedItemsPerPage"`
	Queued               int  `json:"queued"`
	Attempted            int  `json:"attempted"`
	Completed            int  `json:"completed"`
	Skipped              int  `json:"skipped"`
}

type SnapshotLinks struct {
	Expected         []string `json:"expected"`
	ExpectedIDs      []string `json:"expectedIds,omitempty"`
	Queued           []string `json:"queued"`
	QueuedIDs        []string `json:"queuedIds,omitempty"`
	Processed        []string `json:"processed"`
	ProcessedIDs     []string `json:"processedIds,omitempty"`
	Persisted        []string `json:"persisted"`
	PersistedIDs     []string `json:"persistedIds,omitempty"`
	PersistedFilmIDs []string `json:"persistedFilmIds"`
	SkippedIDs       []string `json:"skippedIds"`
}

// Snapshot is the canonical persisted runtime state for one crawl output
// directory. The runner writes it for resume, while review/inspection layers
// read the same file to explain status without re-running crawl logic.
type Snapshot struct {
	// Snapshot is the persisted runtime state that resume/review code reads back.
	// Keep this shape stable so new fields do not silently break restore logic.
	SchemaVersion    int                     `json:"schemaVersion"`
	AppVersion       string                  `json:"appVersion"`
	Status           string                  `json:"status"`
	Message          string                  `json:"message"`
	UpdatedAt        string                  `json:"updatedAt"`
	StartedAt        string                  `json:"startedAt"`
	Config           SnapshotConfig          `json:"config"`
	Progress         SnapshotProgress        `json:"progress"`
	Links            SnapshotLinks           `json:"links"`
	Reconciliation   *ReconciliationSnapshot `json:"reconciliation,omitempty"`
	MissingItems     []string                `json:"missingItems,omitempty"`
	FailedDetails    []FailedDetailRecord    `json:"failedDetails"`
	PageAudits       []PageAuditRecord       `json:"pageAudits"`
	ValidationReport *ResultValidationReport `json:"validationReport,omitempty"`
}

type BuilderConfigInput struct {
	Base             string
	Output           string
	Limit            int
	TotalPages       int
	ItemsPerPage     int
	Parallel         int
	Delay            int
	Timeout          int
	SecondValidation bool
	TaskTemplate     string
}

// BuilderParams is the write-side contract between the live runner state and
// the persisted Snapshot shape. Keeping this input explicit makes it easier to
// see which runtime counters participate in resume semantics versus review-only
// diagnostics.
type BuilderParams struct {
	// BuilderParams is intentionally explicit so the write path can show exactly
	// which live counters participate in persisted resume state.
	AppVersion           string
	Status               string
	Message              string
	StartedAt            string
	Config               BuilderConfigInput
	PageIndex            int
	ExpectedItemsPerPage *int
	FilmsQueued          int
	FilmsAttempted       int
	FilmCount            int
	ExpectedDetailLinks  []string
	QueuedDetailLinks    []string
	ProcessedDetailLinks []string
	PersistedDetailLinks []string
	PersistedFilmIDs     []string
	SkippedItemIDs       []string
	Reconciliation       ReconciliationSnapshot
	MissingItems         []string
	FailedDetails        []FailedDetailRecord
	PageAudits           []PageAuditRecord
	ValidationReport     *ResultValidationReport
	Mode                 crawlexecution.SnapshotMode
	UpdatedAt            time.Time
}

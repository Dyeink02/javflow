package crawlrunner

import (
	"time"

	"javflow/internal/crawltaskstate"
)

// types.go owns the runner's core status, event, and callback contracts.
//
// Ownership summary:
// 1) define the runner's status, phase, event, and callback contracts
// 2) keep orchestration vocabulary stable across runner/bridge/UI code
// 3) avoid scattering runner contract definitions across execution helpers
//
// File map for maintainers:
// 1) runner status enums and label tables
// 2) phase/event DTO contracts
// 3) callback/event handler type aliases

type RunnerStatus string

const (
	StatusIdle       RunnerStatus = "idle"
	StatusStarting   RunnerStatus = "starting"
	StatusRunning    RunnerStatus = "running"
	StatusStopping   RunnerStatus = "stopping"
	StatusCompleted  RunnerStatus = "completed"
	StatusStopped    RunnerStatus = "stopped"
	StatusError      RunnerStatus = "error"
	StatusIncomplete RunnerStatus = "incomplete"
)

// StatusLabels is the UI-facing label table for the runner state machine.
var StatusLabels = map[RunnerStatus]string{
	StatusIdle:       "待机",
	StatusStarting:   "启动中",
	StatusRunning:    "运行中",
	StatusStopping:   "终止中",
	StatusCompleted:  "已完成",
	StatusStopped:    "已终止",
	StatusError:      "异常",
	StatusIncomplete: "未完成",
}

func (s RunnerStatus) Label() string {
	if label, ok := StatusLabels[s]; ok {
		return label
	}
	return string(s)
}

type PhaseKey string

// PhaseDefinition describes one runner phase in the orchestration state
// machine. Execution logic lives in the runner implementation, not in this
// contract file.
type PhaseDefinition struct {
	Key        PhaseKey
	Label      string
	Execute    func(r any) error
	ShouldSkip func(r any) bool
	NextPhase  PhaseKey
}

type PhaseEventType string

const (
	EventState PhaseEventType = "state"
	EventLog   PhaseEventType = "log"
)

// PhaseEvent is the normalized event contract emitted from the runner toward
// logs, live state, and UI projection layers.
type PhaseEvent struct {
	Type    PhaseEventType `json:"type"`
	Status  RunnerStatus   `json:"status"`
	Message string         `json:"message"`
	Stats   *RunnerStats   `json:"stats,omitempty"`
	Phase   PhaseKey       `json:"phase,omitempty"`
	Data    any            `json:"data,omitempty"`
}

// RunnerStats is the lightweight live counter block that can be safely pushed
// to UI/log layers without exposing the whole runner state.
type RunnerStats struct {
	Queued                 int      `json:"queued"`
	Attempted              int      `json:"attempted"`
	Completed              int      `json:"completed"`
	PageIndex              int      `json:"pageIndex"`
	FilteredByActressCount int      `json:"filteredByActressCount"`
	FilteredItemIDs        []string `json:"filteredItemIds,omitempty"`
	CompletedItems         int      `json:"completedItems"`
	CompletedItemIDs       []string `json:"completedItemIds,omitempty"`
	CompletedMagnetCount   int      `json:"completedMagnetCount"`
}

// Config is the runner-side execution contract after UI payloads and restored
// runtime state have already been normalized by upper layers.
type Config struct {
	BaseURL                     string                        `json:"baseUrl"`
	Base                        string                        `json:"base"`
	Parallel                    int                           `json:"parallel"`
	Timeout                     time.Duration                 `json:"timeout"`
	Limit                       int                           `json:"limit"`
	TotalPages                  int                           `json:"totalPages"`
	ItemsPerPage                int                           `json:"itemsPerPage"`
	Delay                       int                           `json:"delay"`
	RetryCount                  int                           `json:"retryCount"`
	RetryDelay                  time.Duration                 `json:"retryDelay"`
	Nomag                       bool                          `json:"nomag"`
	Allmag                      bool                          `json:"allmag"`
	Nopic                       bool                          `json:"nopic"`
	Proxy                       string                        `json:"proxy"`
	Output                      string                        `json:"output"`
	UserDataDir                 string                        `json:"userDataDir"`
	Search                      string                        `json:"search"`
	SecondValidation            bool                          `json:"secondValidation"`
	MagnetExcludeKeywords       string                        `json:"magnetExcludeKeywords"`
	ActressCountFilterThreshold int                           `json:"actressCountFilterThreshold"`
	FilmCodeFilterThreshold     string                        `json:"filmCodeFilterThreshold"`
	MinReleaseDate              string                        `json:"minReleaseDate"`
	SupplementMagnetTopN        int                           `json:"supplementMagnetTopN"`
	RestoredState               *crawltaskstate.RestoredState `json:"-"`
}

// PageAudit records page-level validation/retry evidence for review and resume
// diagnostics.
type PageAudit struct {
	PageNumber       int    `json:"pageNumber"`
	URL              string `json:"url"`
	ExpectedCount    *int   `json:"expectedCount"`
	ActualCount      int    `json:"actualCount"`
	RetryCount       int    `json:"retryCount"`
	ValidationPassed bool   `json:"validationPassed"`
	ConfidenceScore  int    `json:"confidenceScore"`
	Confidence       string `json:"confidence"`
	Reason           string `json:"reason"`
	UpdatedAt        string `json:"updatedAt"`
}

// FailedDetail is the persistent per-item failure contract surfaced in review
// panels and quality output.
type FailedDetail struct {
	Item         string `json:"item"`
	SourceLink   string `json:"sourceLink"`
	Reason       string `json:"reason"`
	Category     string `json:"category"`
	RetryCount   int    `json:"retryCount"`
	RetryAdvice  string `json:"retryAdvice"`
	Recoverable  bool   `json:"recoverable"`
	LastFailedAt string `json:"lastFailedAt"`
}

// TaskSnapshot is the compact persisted runner snapshot used by resume and
// review readers. It is not the full internal runner state graph.
type TaskSnapshot struct {
	Status               RunnerStatus   `json:"status"`
	Message              string         `json:"message"`
	PageIndex            int            `json:"pageIndex"`
	ExpectedItemsPerPage *int           `json:"expectedItemsPerPage"`
	FilmsQueued          int            `json:"filmsQueued"`
	FilmsAttempted       int            `json:"filmsAttempted"`
	FilmCount            int            `json:"filmCount"`
	PageAudits           []PageAudit    `json:"pageAudits"`
	FailedDetails        []FailedDetail `json:"failedDetails"`
	MissingItems         []string       `json:"missingItems"`
}

// EventHandler is the callback shape shared by the runner and downstream event
// projection layers.
type EventHandler func(event PhaseEvent)

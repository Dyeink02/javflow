// Ownership summary:
//   This file implements domain logic for the javflow backend.
//
// File map for maintainers:
//   1) Domain-specific types and helpers.
//   2) Internal service implementation.
//

package subcrawlv2

import (
	"context"
	"sync"
)

type CrawlStatus struct {
	Phase          string `json:"phase"`
	Status         string `json:"status"`
	Message        string `json:"message"`
	Total          int    `json:"total"`
	Completed      int    `json:"completed"`
	Failed         int    `json:"failed"`
	Current        string `json:"current"`
	ActressName    string `json:"actressName"`
	BatchTotal     int    `json:"batchTotal"`
	BatchCompleted int    `json:"batchCompleted"`
	BatchSucceeded int    `json:"batchSucceeded"`
	BatchNoUpdate  int    `json:"batchNoUpdate"`
	Concurrency    int    `json:"concurrency"`
	Active         int    `json:"active"`
}

type CrawlTask struct {
	ctx         context.Context
	cancel      context.CancelFunc
	requests    []CrawlRequest
	mu          sync.Mutex
	status      CrawlStatus
	running     bool
	concurrency int
	appPath     string
}

func newCrawlTask(ctx context.Context, cancel context.CancelFunc, requests []CrawlRequest, concurrency int, appPath string) *CrawlTask {
	return &CrawlTask{
		ctx:         ctx,
		cancel:      cancel,
		requests:    requests,
		concurrency: concurrency,
		appPath:     appPath,
		status: CrawlStatus{
			Phase:       "idle",
			Status:      "idle",
			BatchTotal:  len(requests),
			Concurrency: concurrency,
		},
	}
}

func (t *CrawlTask) IsRunning() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.running
}

func (t *CrawlTask) Status() CrawlStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status
}

func (t *CrawlTask) Stop() {
	t.cancel()
}

// Ownership summary:
//   This file implements domain logic for the javflow backend.
//
// File map for maintainers:
//   1) Domain-specific types and helpers.
//   2) Internal service implementation.
//

package subcrawl

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
}

type CrawlTask struct {
	ctx      context.Context
	cancel   context.CancelFunc
	requests []CrawlRequest
	mu       sync.Mutex
	status   CrawlStatus
	running  bool
}

func newCrawlTask(ctx context.Context, cancel context.CancelFunc, requests []CrawlRequest) *CrawlTask {
	return &CrawlTask{
		ctx:      ctx,
		cancel:   cancel,
		requests: requests,
		status: CrawlStatus{
			Phase:      "idle",
			Status:     "idle",
			BatchTotal: len(requests),
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


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
	"fmt"
	"sync"
	"time"

	"javflow/internal/avsubscriptionv2"
	"javflow/internal/crawlfetch"
	"javflow/internal/events"
	runtimepaths "javflow/internal/runtime"
)

type CrawlRequest struct {
	SubscriptionID string        `json:"subscriptionId"`
	ActressName    string        `json:"actressName"`
	CrawlURL       string        `json:"crawlUrl"`
	PreferredBase  string        `json:"preferredBase"`
	OutputDir      string        `json:"outputDir"`
	TargetCount    int           `json:"targetCount"`
	TargetCodes    []string      `json:"targetCodes,omitempty"`
	UserDataDir    string        `json:"userDataDir,omitempty"`
	Proxy          string        `json:"proxy"`
	ConfigCookie   string        `json:"configCookie"`
	Timeout        time.Duration `json:"timeout"`
}

type Service struct {
	bus           *events.Bus
	paths         runtimepaths.Paths
	subscriptions *avsubscriptionv2.Service
	fetch         *crawlfetch.Service
	mu            sync.Mutex
	activeTask    *CrawlTask
}

func NewService(bus *events.Bus, paths runtimepaths.Paths, subscriptions *avsubscriptionv2.Service, fetch *crawlfetch.Service) *Service {
	return &Service{
		bus:           bus,
		paths:         paths,
		subscriptions: subscriptions,
		fetch:         fetch,
	}
}

func (s *Service) StartSingle(ctx context.Context, req CrawlRequest) error {
	return s.StartMany(ctx, []CrawlRequest{req}, 1)
}

func (s *Service) StartMany(ctx context.Context, requests []CrawlRequest, concurrency int) error {
	if len(requests) == 0 {
		return fmt.Errorf("没有可执行的订阅抓取请求")
	}

	s.mu.Lock()
	if s.activeTask != nil && s.activeTask.IsRunning() {
		s.mu.Unlock()
		return fmt.Errorf("订阅抓取任务正在运行中")
	}

	taskCtx, cancel := context.WithCancel(ctx)
	task := newCrawlTask(taskCtx, cancel, requests, clampConcurrency(concurrency), s.paths.AppPath)
	s.activeTask = task
	s.mu.Unlock()

	s.runTaskAsync(task, func() {
		task.Run(s.bus, s.subscriptions, s.fetch)
	})
	return nil
}

func clampConcurrency(value int) int {
	if value <= 0 {
		return 2
	}
	if value > 3 {
		return 3
	}
	return value
}

func (s *Service) runTaskAsync(task *CrawlTask, run func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				if s.bus != nil {
					s.bus.Emit("subcrawlv2.panic", map[string]any{
						"message": fmt.Sprintf("订阅 V2 抓取任务 panic: %v", r),
					})
				}
			}
			s.mu.Lock()
			if s.activeTask == task {
				s.activeTask = nil
			}
			s.mu.Unlock()
		}()
		run()
	}()
}

func (s *Service) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeTask == nil || !s.activeTask.IsRunning() {
		return nil
	}
	s.activeTask.Stop()
	return nil
}

func (s *Service) Status() CrawlStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeTask == nil {
		return CrawlStatus{Phase: "idle", Status: "idle"}
	}
	return s.activeTask.Status()
}

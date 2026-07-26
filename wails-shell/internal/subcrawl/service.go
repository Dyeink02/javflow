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
	"fmt"
	"sync"
	"time"

	"javflow/internal/avsubscription"
	"javflow/internal/events"
	runtimepaths "javflow/internal/runtime"
)

type Service struct {
	bus           *events.Bus
	paths         runtimepaths.Paths
	subscriptions *avsubscription.Service
	mu            sync.Mutex
	activeTask    *CrawlTask
}

type CrawlRequest struct {
	SubscriptionID string        `json:"subscriptionId"`
	ActressName    string        `json:"actressName"`
	CrawlURL       string        `json:"crawlUrl"`
	PreferredBase  string        `json:"preferredBase"`
	OutputDir      string        `json:"outputDir"`
	TargetCount    int           `json:"targetCount"`
	BaselineCodes  []string      `json:"baselineCodes,omitempty"`
	UserDataDir    string        `json:"userDataDir,omitempty"`
	Proxy          string        `json:"proxy"`
	ConfigCookie   string        `json:"configCookie"`
	Timeout        time.Duration `json:"timeout"`
}

type BatchCrawlRequest struct {
	OutputDir    string        `json:"outputDir"`
	Proxy        string        `json:"proxy"`
	ConfigCookie string        `json:"configCookie"`
	Timeout      time.Duration `json:"timeout"`
}

func NewService(bus *events.Bus, paths runtimepaths.Paths, subscriptions *avsubscription.Service) *Service {
	return &Service{
		bus:           bus,
		paths:         paths,
		subscriptions: subscriptions,
	}
}

func (s *Service) StartSingle(ctx context.Context, req CrawlRequest) error {
	s.mu.Lock()
	if s.activeTask != nil && s.activeTask.IsRunning() {
		s.mu.Unlock()
		return fmt.Errorf("订阅抓取任务正在运行中")
	}

	taskCtx, cancel := context.WithCancel(ctx)
	task := newCrawlTask(taskCtx, cancel, []CrawlRequest{req})
	s.activeTask = task
	s.mu.Unlock()

	s.runTaskAsync(task, func() {
		task.Run(s.bus, s.subscriptions)
	})
	return nil
}

func (s *Service) StartBatch(ctx context.Context, req BatchCrawlRequest) error {
	s.mu.Lock()
	if s.activeTask != nil && s.activeTask.IsRunning() {
		s.mu.Unlock()
		return fmt.Errorf("订阅抓取任务正在运行中")
	}

	items, err := s.subscriptions.List()
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("加载订阅列表失败: %w", err)
	}

	var requests []CrawlRequest
	for _, item := range items {
		pending := item.CurrentCount - item.SyncedCount
		if pending <= 0 {
			continue
		}
		requests = append(requests, CrawlRequest{
			SubscriptionID: item.ID,
			ActressName:    item.ActressName,
			CrawlURL:       item.CrawlURL,
			PreferredBase:  item.PreferredBase,
			OutputDir:      req.OutputDir,
			TargetCount:    pending,
			BaselineCodes:  append([]string{}, item.BaselineCodes...),
			UserDataDir:    s.paths.UserData,
			Proxy:          req.Proxy,
			ConfigCookie:   req.ConfigCookie,
			Timeout:        req.Timeout,
		})
	}

	if len(requests) == 0 {
		s.mu.Unlock()
		return fmt.Errorf("没有待处理的订阅")
	}

	taskCtx, cancel := context.WithCancel(ctx)
	task := newCrawlTask(taskCtx, cancel, requests)
	s.activeTask = task
	s.mu.Unlock()

	s.runTaskAsync(task, func() {
		task.Run(s.bus, s.subscriptions)
	})
	return nil
}

func (s *Service) runTaskAsync(task *CrawlTask, run func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				if s.bus != nil {
					s.bus.Emit("subcrawl.panic", map[string]any{
						"message": fmt.Sprintf("订阅抓取任务 panic: %v", r),
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

func (s *Service) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activeTask != nil && s.activeTask.IsRunning()
}

func (s *Service) Status() CrawlStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeTask == nil {
		return CrawlStatus{Phase: "idle", Status: "idle"}
	}
	return s.activeTask.Status()
}

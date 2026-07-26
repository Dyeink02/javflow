package crawlqueue

import (
	"context"
	"sync"
	"sync/atomic"
)

// Package crawlqueue owns the worker queue and queue event stream used by the
// Go crawler runtime.
//
// Boundary:
// - queue orchestration, worker lifetimes, and queue-level events live here
// - crawl business rules stay in runner/handlers, not in queue mechanics
//
// Ownership summary:
// 1) coordinate queue workers and task channels
// 2) expose queue-level progress events and stats
// 3) keep work scheduling mechanics separate from crawl-domain decisions
//
// File map for maintainers:
// 1) queue contracts and event types
// 2) runner state/concurrency bookkeeping
// 3) worker/channel lifecycle setup
// 4) enqueue/stop/stats/event publication helpers

type QueueStats struct {
	Waiting int `json:"waiting"`
	Running int `json:"running"`
}

type QueueEventType string

const (
	EventIndexPageStart     QueueEventType = "index_page_start"
	EventIndexPageProcessed                = "index_page_processed"
	EventDetailPageStart                   = "detail_page_start"
	EventDetailPageProcessed               = "detail_page_processed"
	EventDetailPageFailed                  = "detail_page_failed"
	EventFilmDataSaved                     = "film_data_saved"
)

type EventHandler func(event QueueEvent)

type QueueEvent struct {
	Type QueueEventType   `json:"type"`
	Data any              `json:"data"`
}

type IndexPageTask struct {
	URL     string
	Resolve func(links []string)
	Reject  func(err error)
}

type DetailPageTask struct {
	Link string
}

type MagnetTask struct {
	SourceLink string
	Metadata   any
	FilmData   any
}

type runState struct {
	active bool
	mu     sync.Mutex
}

func (r *runState) isActive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

func (r *runState) setActive(v bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active = v
}

type Runner struct {
	ctx       context.Context
	cancel    context.CancelFunc
	workers   sync.WaitGroup

	concurrencyIndex   int
	concurrencyDetail  int
	concurrencyMagnet  int
	concurrencyFile    int
	concurrencyImage   int

	indexTaskCh      chan IndexPageTask
	detailTaskCh     chan DetailPageTask
	magnetFastCh     chan MagnetTask
	magnetRecoveryCh chan MagnetTask
	fileCh           chan any
	imageCh          chan any

	indexSem      chan struct{}
	detailSem     chan struct{}
	magnetSem     chan struct{}
	fileSem       chan struct{}
	imageSem      chan struct{}

	indexActive      int64
	detailActive     int64
	magnetActive     int64
	fileActive       int64
	imageActive      int64

	indexWaiting      int64
	detailWaiting     int64
	magnetFastWaiting int64 // 审计 M-16：与 magnetRecoveryWaiting 分离，避免统计串扰。
	magnetRecoveryWaiting int64
	fileWaiting       int64
	imageWaiting      int64

	stopping          runState

	indexHandler      func(ctx context.Context, task IndexPageTask) error
	detailHandler     func(ctx context.Context, task DetailPageTask) error
	magnetFastHandler func(ctx context.Context, task MagnetTask) error
	magnetRecoveryHandler func(ctx context.Context, task MagnetTask) error
	fileHandler       func(ctx context.Context, task any) error
	imageHandler      func(ctx context.Context, task any) error

	eventHandlers map[QueueEventType][]EventHandler
	eventMu       sync.RWMutex
}

type RunnerOptions struct {
	IndexConcurrency   int
	DetailConcurrency  int
	MagnetConcurrency  int
	FileConcurrency    int
	ImageConcurrency   int
	IndexHandler       func(ctx context.Context, task IndexPageTask) error
	DetailHandler      func(ctx context.Context, task DetailPageTask) error
	MagnetFastHandler  func(ctx context.Context, task MagnetTask) error
	MagnetRecoveryHandler func(ctx context.Context, task MagnetTask) error
	FileHandler        func(ctx context.Context, task any) error
	ImageHandler       func(ctx context.Context, task any) error
}

// NewRunner assembles the queue runtime and all worker channels, but it does
// not start processing yet. Keep queue topology decisions centralized here.
func NewRunner(opts RunnerOptions) *Runner {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runner{
		ctx:                 ctx,
		cancel:              cancel,
		concurrencyIndex:    normalizeConcurrency(opts.IndexConcurrency, 2),
		concurrencyDetail:   normalizeConcurrency(opts.DetailConcurrency, 2),
		concurrencyMagnet:   normalizeConcurrency(opts.MagnetConcurrency, 2),
		concurrencyFile:     normalizeConcurrency(opts.FileConcurrency, 1),
		concurrencyImage:    normalizeConcurrency(opts.ImageConcurrency, 1),
		indexHandler:        opts.IndexHandler,
		detailHandler:       opts.DetailHandler,
		magnetFastHandler:   opts.MagnetFastHandler,
		magnetRecoveryHandler: opts.MagnetRecoveryHandler,
		fileHandler:         opts.FileHandler,
		imageHandler:        opts.ImageHandler,
		eventHandlers:       map[QueueEventType][]EventHandler{},
	}
	r.stopping.setActive(false)

	r.indexTaskCh = make(chan IndexPageTask, 8)
	r.detailTaskCh = make(chan DetailPageTask, 128)
	r.magnetFastCh = make(chan MagnetTask, 64)
	r.magnetRecoveryCh = make(chan MagnetTask, 16)
	r.fileCh = make(chan any, 128)
	r.imageCh = make(chan any, 32)

	r.indexSem = make(chan struct{}, r.concurrencyIndex)
	r.detailSem = make(chan struct{}, r.concurrencyDetail)
	r.magnetSem = make(chan struct{}, r.concurrencyMagnet)
	r.fileSem = make(chan struct{}, r.concurrencyFile)
	r.imageSem = make(chan struct{}, r.concurrencyImage)

	return r
}

// Start launches only the workers for handlers that were provided. Queue setup
// and worker activation remain separate so callers can fully wire events before
// work begins.
func (r *Runner) Start() {
	if r.indexHandler != nil {
		r.workers.Add(1)
		go r.runIndexWorker()
	}
	if r.detailHandler != nil {
		r.workers.Add(1)
		go r.runDetailWorker()
	}
	if r.magnetFastHandler != nil {
		r.workers.Add(1)
		go r.runMagnetWorker(r.magnetFastCh, r.magnetFastHandler, &r.magnetFastWaiting)
	}
	if r.magnetRecoveryHandler != nil {
		r.workers.Add(1)
		go r.runMagnetWorker(r.magnetRecoveryCh, r.magnetRecoveryHandler, &r.magnetRecoveryWaiting)
	}
	if r.fileHandler != nil {
		r.workers.Add(1)
		go r.runFileWorker()
	}
	if r.imageHandler != nil {
		r.workers.Add(1)
		go r.runImageWorker()
	}
}

// acquireSem 尝试获取信号量，同时监听 Runner 上下文。若 Runner 已关闭，
// 立即返回 false，避免 worker 在 Shutdown 后仍被信号量阻塞（审计 H-14）。
func (r *Runner) acquireSem(sem chan struct{}) bool {
	select {
	case <-r.ctx.Done():
		return false
	case sem <- struct{}{}:
		return true
	}
}

// runIndexWorker / runDetailWorker / runMagnetWorker / runFileWorker /
// runImageWorker are intentionally small queue executors. They should handle
// queue accounting and event emission only; domain behavior belongs in the
// injected handlers.
func (r *Runner) runIndexWorker() {
	defer r.workers.Done()
	for {
		select {
		case <-r.ctx.Done():
			return
		case task := <-r.indexTaskCh:
			if r.stopping.isActive() {
				task.Resolve([]string{})
				continue
			}
			// 审计 H-14：信号量获取加入 ctx 保护，防止 Shutdown 时 worker 泄漏。
			if !r.acquireSem(r.indexSem) {
				task.Reject(context.Canceled)
				continue
			}
			atomic.AddInt64(&r.indexWaiting, -1)
			atomic.AddInt64(&r.indexActive, 1)
			r.emit(QueueEvent{Type: EventIndexPageStart, Data: map[string]string{"link": task.URL}})
			err := r.indexHandler(r.ctx, task)
			atomic.AddInt64(&r.indexActive, -1)
			<-r.indexSem
			if err != nil {
				task.Reject(err)
			}
		}
	}
}

func (r *Runner) runDetailWorker() {
	defer r.workers.Done()
	for {
		select {
		case <-r.ctx.Done():
			return
		case task := <-r.detailTaskCh:
			if r.stopping.isActive() {
				continue
			}
			// 审计 H-14：信号量获取加入 ctx 保护。
			if !r.acquireSem(r.detailSem) {
				continue
			}
			atomic.AddInt64(&r.detailWaiting, -1)
			atomic.AddInt64(&r.detailActive, 1)
			r.emit(QueueEvent{Type: EventDetailPageStart, Data: map[string]string{"link": task.Link}})
			err := r.detailHandler(r.ctx, task)
			atomic.AddInt64(&r.detailActive, -1)
			<-r.detailSem
			if err != nil {
				r.emit(QueueEvent{Type: EventDetailPageFailed, Data: map[string]string{"link": task.Link, "reason": err.Error()}})
			}
		}
	}
}

func (r *Runner) runMagnetWorker(ch <-chan MagnetTask, handler func(ctx context.Context, task MagnetTask) error, waitingCounter *int64) {
	defer r.workers.Done()
	for {
		select {
		case <-r.ctx.Done():
			return
		case task := <-ch:
			if r.stopping.isActive() {
				continue
			}
			// 审计 H-14：信号量获取加入 ctx 保护。
			if !r.acquireSem(r.magnetSem) {
				continue
			}
			atomic.AddInt64(waitingCounter, -1)
			atomic.AddInt64(&r.magnetActive, 1)
			_ = handler(r.ctx, task)
			atomic.AddInt64(&r.magnetActive, -1)
			<-r.magnetSem
		}
	}
}

func (r *Runner) runFileWorker() {
	defer r.workers.Done()
	for {
		select {
		case <-r.ctx.Done():
			return
		case task := <-r.fileCh:
			if r.stopping.isActive() {
				continue
			}
			// 审计 H-14：信号量获取加入 ctx 保护。
			if !r.acquireSem(r.fileSem) {
				continue
			}
			atomic.AddInt64(&r.fileWaiting, -1)
			atomic.AddInt64(&r.fileActive, 1)
			err := r.fileHandler(r.ctx, task)
			atomic.AddInt64(&r.fileActive, -1)
			<-r.fileSem
			if err == nil {
				r.emit(QueueEvent{Type: EventFilmDataSaved, Data: task})
			}
		}
	}
}

func (r *Runner) runImageWorker() {
	defer r.workers.Done()
	for {
		select {
		case <-r.ctx.Done():
			return
		case task := <-r.imageCh:
			if r.stopping.isActive() {
				continue
			}
			// 审计 H-14：信号量获取加入 ctx 保护。
			if !r.acquireSem(r.imageSem) {
				continue
			}
			atomic.AddInt64(&r.imageWaiting, -1)
			atomic.AddInt64(&r.imageActive, 1)
			_ = r.imageHandler(r.ctx, task)
			atomic.AddInt64(&r.imageActive, -1)
			<-r.imageSem
		}
	}
}

// pushTask 是通用入队辅助函数。审计 H-15：裸写 channel 在 Shutdown 时可能永久阻塞，
// 因此入队时同时监听 ctx.Done()；Runner 关闭后入队立即失败并丢弃任务。
func pushTask[T any](r *Runner, ch chan<- T, waiting *int64, task T) bool {
	if r.stopping.isActive() || r.ctx.Err() != nil {
		return false
	}
	atomic.AddInt64(waiting, 1)
	select {
	case ch <- task:
		return true
	case <-r.ctx.Done():
		// Runner 已关闭，恢复等待计数并丢弃任务。
		atomic.AddInt64(waiting, -1)
		return false
	}
}

func (r *Runner) PushIndex(task IndexPageTask) {
	pushTask(r, r.indexTaskCh, &r.indexWaiting, task)
}

func (r *Runner) PushDetail(task DetailPageTask) {
	pushTask(r, r.detailTaskCh, &r.detailWaiting, task)
}

func (r *Runner) PushDetailBatch(tasks []DetailPageTask) {
	for _, t := range tasks {
		pushTask(r, r.detailTaskCh, &r.detailWaiting, t)
	}
}

func (r *Runner) PushMagnetFast(task MagnetTask) {
	pushTask(r, r.magnetFastCh, &r.magnetFastWaiting, task)
}

func (r *Runner) PushMagnetRecovery(task MagnetTask) {
	pushTask(r, r.magnetRecoveryCh, &r.magnetRecoveryWaiting, task)
}

func (r *Runner) PushFile(task any) {
	pushTask(r, r.fileCh, &r.fileWaiting, task)
}

func (r *Runner) PushImage(task any) {
	pushTask(r, r.imageCh, &r.imageWaiting, task)
}

// Stats is the queue-runtime snapshot consumed by higher-level diagnostics and
// reconciliation logic. Keep naming stable so read models do not each invent
// their own queue labels.
func (r *Runner) Stats() map[string]QueueStats {
	return map[string]QueueStats{
		"indexPageQueue":      {Waiting: int(atomic.LoadInt64(&r.indexWaiting)), Running: int(atomic.LoadInt64(&r.indexActive))},
		"detailPageQueue":     {Waiting: int(atomic.LoadInt64(&r.detailWaiting)), Running: int(atomic.LoadInt64(&r.detailActive))},
		// 审计 M-16：magnetFastQueue 与 magnetRecoveryQueue 使用独立计数器。
		"magnetFastQueue":     {Waiting: int(atomic.LoadInt64(&r.magnetFastWaiting)), Running: int(atomic.LoadInt64(&r.magnetActive))},
		"magnetRecoveryQueue": {Waiting: int(atomic.LoadInt64(&r.magnetRecoveryWaiting)), Running: int(atomic.LoadInt64(&r.magnetActive))},
		"fileWriteQueue":      {Waiting: int(atomic.LoadInt64(&r.fileWaiting)), Running: int(atomic.LoadInt64(&r.fileActive))},
		"imageDownloadQueue":  {Waiting: int(atomic.LoadInt64(&r.imageWaiting)), Running: int(atomic.LoadInt64(&r.imageActive))},
	}
}

// AllFinished and WorkQueuesFinished are queue-level completion predicates only.
// They intentionally do not answer whether crawl business reconciliation has
// finished; higher layers decide that.
func (r *Runner) AllFinished() bool {
	stats := r.Stats()
	for _, s := range stats {
		if s.Waiting > 0 || s.Running > 0 {
			return false
		}
	}
	return true
}

func (r *Runner) WorkQueuesFinished() bool {
	stats := r.Stats()
	for name, s := range stats {
		if name == "magnetRecoveryQueue" {
			continue
		}
		if s.Waiting > 0 || s.Running > 0 {
			return false
		}
	}
	return true
}

func (r *Runner) IndexBacklog() int {
	return int(atomic.LoadInt64(&r.indexWaiting)) + int(atomic.LoadInt64(&r.indexActive))
}

func (r *Runner) DetailBacklog() int {
	return int(atomic.LoadInt64(&r.detailWaiting)) + int(atomic.LoadInt64(&r.detailActive))
}

func (r *Runner) Shutdown() {
	r.stopping.setActive(true)
	r.cancel()
	r.workers.Wait()
}

func (r *Runner) On(eventType QueueEventType, handler EventHandler) {
	r.eventMu.Lock()
	defer r.eventMu.Unlock()
	r.eventHandlers[eventType] = append(r.eventHandlers[eventType], handler)
}

func (r *Runner) emit(event QueueEvent) {
	r.eventMu.RLock()
	defer r.eventMu.RUnlock()
	if handlers, ok := r.eventHandlers[event.Type]; ok {
		for _, h := range handlers {
			h(event)
		}
	}
}

func normalizeConcurrency(val int, fallback int) int {
	if val > 0 {
		return val
	}
	return fallback
}

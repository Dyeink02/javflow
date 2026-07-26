package crawlqueue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunnerStartStop(t *testing.T) {
	var handled int32
	r := NewRunner(RunnerOptions{
		IndexConcurrency: 2,
		DetailConcurrency: 2,
		IndexHandler: func(ctx context.Context, task IndexPageTask) error {
			atomic.AddInt32(&handled, 1)
			task.Resolve([]string{"/link1", "/link2"})
			return nil
		},
		DetailHandler: func(ctx context.Context, task DetailPageTask) error {
			atomic.AddInt32(&handled, 1)
			return nil
		},
	})
	r.Start()

	r.PushIndex(IndexPageTask{URL: "https://example.com/page1", Resolve: func(links []string) {}, Reject: func(err error) {}})
	r.PushIndex(IndexPageTask{URL: "https://example.com/page2", Resolve: func(links []string) {}, Reject: func(err error) {}})
	r.PushDetail(DetailPageTask{Link: "/detail1"})
	r.PushDetail(DetailPageTask{Link: "/detail2"})

	time.Sleep(200 * time.Millisecond)

	r.Shutdown()

	if atomic.LoadInt32(&handled) < 2 {
		t.Errorf("期望至少处理2个任务, 实际 %d", atomic.LoadInt32(&handled))
	}
}

func TestRunnerWorkQueuesFinished(t *testing.T) {
	var count int32
	r := NewRunner(RunnerOptions{
		FileConcurrency: 1,
		FileHandler: func(ctx context.Context, task any) error {
			atomic.AddInt32(&count, 1)
			time.Sleep(50 * time.Millisecond)
			return nil
		},
	})
	r.Start()

	r.PushFile("data-1")
	r.PushFile("data-2")

	time.Sleep(300 * time.Millisecond)

	if !r.WorkQueuesFinished() {
		t.Error("期望队列已完成但未完成")
	}
	if atomic.LoadInt32(&count) != 2 {
		t.Errorf("期望 2 个任务完成, 实际 %d", atomic.LoadInt32(&count))
	}

	r.Shutdown()
}

func TestRunnerShutdownRejectsPending(t *testing.T) {
	blockCh := make(chan struct{})
	r := NewRunner(RunnerOptions{
		IndexConcurrency: 1,
		IndexHandler: func(ctx context.Context, task IndexPageTask) error {
			<-blockCh
			return nil
		},
	})
	r.Start()

	r.PushIndex(IndexPageTask{
		URL: "https://example.com/page",
		Resolve: func(links []string) {},
		Reject: func(err error) {},
	})

	time.Sleep(50 * time.Millisecond)
	r.stopping.setActive(true)
	close(blockCh)

	time.Sleep(200 * time.Millisecond)
	r.Shutdown()
}

func TestRunnerConcurrencyLimit(t *testing.T) {
	var maxConcurrent int32
	var current int32
	r := NewRunner(RunnerOptions{
		FileConcurrency: 2,
		FileHandler: func(ctx context.Context, task any) error {
			v := atomic.AddInt32(&current, 1)
			if v > atomic.LoadInt32(&maxConcurrent) {
				atomic.StoreInt32(&maxConcurrent, v)
			}
			time.Sleep(80 * time.Millisecond)
			atomic.AddInt32(&current, -1)
			return nil
		},
	})
	r.Start()

	for i := 0; i < 10; i++ {
		r.PushFile("data")
	}

	time.Sleep(600 * time.Millisecond)
	r.Shutdown()

	if atomic.LoadInt32(&maxConcurrent) > 2 {
		t.Errorf("最大并发应 <= 2, 实际 %d", maxConcurrent)
	}
}

func TestRunnerEvents(t *testing.T) {
	eventCount := 0
	r := NewRunner(RunnerOptions{
		FileConcurrency: 1,
		FileHandler: func(ctx context.Context, task any) error {
			return nil
		},
	})
	r.On(EventFilmDataSaved, func(event QueueEvent) {
		eventCount++
	})
	r.Start()

	r.PushFile("event-data")
	time.Sleep(100 * time.Millisecond)
	r.Shutdown()

	if eventCount != 1 {
		t.Errorf("期望 1 个事件, 实际 %d", eventCount)
	}
}

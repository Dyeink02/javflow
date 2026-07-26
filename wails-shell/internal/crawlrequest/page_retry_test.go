package crawlrequest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRetryOnServerError(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>ok</body></html>"))
	}))
	defer server.Close()

	client, err := NewClient(PageRequestOptions{
		Timeout:    10 * time.Second,
		RetryCount: 5,
		RetryDelay: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	resp, err := client.GetPage(context.Background(), server.URL, "")
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("期望 200, 得到 %d", resp.StatusCode)
	}
	if attempts < 3 {
		t.Errorf("期望至少 3 次尝试, 实际 %d", attempts)
	}
}

func TestRetryExhausted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client, err := NewClient(PageRequestOptions{
		Timeout:    5 * time.Second,
		RetryCount: 2,
		RetryDelay: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	_, err = client.GetPage(context.Background(), server.URL, "")
	if err == nil {
		t.Fatal("期望请求失败（重试耗尽）")
	}
}

func TestRetryContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client, err := NewClient(PageRequestOptions{
		Timeout:    10 * time.Second,
		RetryCount: 5,
		RetryDelay: 1 * time.Second,
	})
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	_, err = client.GetPage(ctx, server.URL, "")
	if err == nil {
		t.Fatal("期望因 context 取消而失败")
	}
}

func TestBuildSecChUa(t *testing.T) {
	tests := []struct {
		ua      string
		contain string
	}{
		{"Mozilla/5.0 Chrome/120.0.0.0", "Chromium"},
		{"Mozilla/5.0 Edg/121.0.0.0", "Microsoft Edge"},
		{"Mozilla/5.0 Firefox/122.0", "Firefox"},
	}

	for _, tc := range tests {
		result := BuildSecChUa(tc.ua)
		if result == "" {
			t.Errorf("BuildSecChUa(%q) 返回空", tc.ua)
		}
	}
}

func TestGetExponentialBackoffDelay(t *testing.T) {
	maxDelay := 30 * time.Second
	delay := GetExponentialBackoffDelay(3*time.Second, 0, maxDelay)
	if delay < 3*time.Second {
		t.Errorf("基础延迟应 >= 3s, 得到 %v", delay)
	}

	delay = GetExponentialBackoffDelay(3*time.Second, 5, maxDelay)
	if delay > maxDelay {
		t.Errorf("延迟应 <= maxDelay %v, 得到 %v", maxDelay, delay)
	}
}

func TestGetRandomDelay(t *testing.T) {
	for i := 0; i < 10; i++ {
		delay := GetRandomDelay(5, 15)
		if delay < 5*time.Second || delay > 15*time.Second {
			t.Errorf("随机延迟超出范围 [5s,15s]: %v", delay)
		}
	}
}

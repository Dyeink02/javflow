package crawlconfig

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Parallel != 2 {
		t.Errorf("期望 Parallel=2, 得到 %d", cfg.Parallel)
	}
	if cfg.BaseURL != "https://www.javbus.com/" {
		t.Errorf("期望 BaseURL=https://www.javbus.com/, 得到 %s", cfg.BaseURL)
	}
}

func TestToPageRequestOptions(t *testing.T) {
	cfg := DefaultConfig()
	opts := cfg.ToPageRequestOptions("MyTestUA/1.0")
	if opts.Timeout != 30*time.Second {
		t.Errorf("期望 Timeout=30s, 得到 %v", opts.Timeout)
	}
	if opts.UserAgent != "MyTestUA/1.0" {
		t.Errorf("期望 UserAgent=MyTestUA/1.0, 得到 %s", opts.UserAgent)
	}
	if opts.RetryCount != 3 {
		t.Errorf("期望 RetryCount=3, 得到 %d", opts.RetryCount)
	}
}

func TestRandomUserAgent(t *testing.T) {
	for i := 0; i < 10; i++ {
		ua := RandomUserAgent()
		if ua == "" {
			t.Error("RandomUserAgent 返回空")
		}
	}
}

func TestBuildSecChUa(t *testing.T) {
	result := BuildSecChUa("Mozilla/5.0 Chrome/120.0.0.0")
	if result == "" {
		t.Error("BuildSecChUa 返回空")
	}
}

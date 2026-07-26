package crawlconfig

import (
	"time"

	"javflow/internal/crawlrequest"
)

// Package crawlconfig owns the normalized crawl-run configuration shape shared
// between bridge/task-controller code and the runner.
//
// Ownership summary:
// 1) define the canonical crawl configuration contract and defaults
// 2) bridge request/runtime settings into one normalized execution shape
// 3) keep crawl configuration policy centralized and reviewable
//
// File map for maintainers:
// 1) canonical crawl configuration DTOs
// 2) config defaulting and normalization helpers
// 3) request/runtime option bridge helpers

// CrawlConfig is the normalized run-configuration contract shared across
// bridge, task-controller, and runner layers.
type CrawlConfig struct {
	BaseURL                     string            `json:"baseUrl"`
	Base                        string            `json:"base"`
	Parallel                    int               `json:"parallel"`
	Timeout                     time.Duration     `json:"timeout"`
	Limit                       int               `json:"limit"`
	TotalPages                  int               `json:"totalPages"`
	ItemsPerPage                int               `json:"itemsPerPage"`
	Delay                       int               `json:"delay"`
	RetryCount                  int               `json:"retryCount"`
	RetryDelay                  time.Duration     `json:"retryDelay"`
	Nomag                       bool              `json:"nomag"`
	Allmag                      bool              `json:"allmag"`
	Nopic                       bool              `json:"nopic"`
	StrictSSL                   bool              `json:"strictSSL"`
	Proxy                       string            `json:"proxy"`
	Output                      string            `json:"output"`
	Search                      string            `json:"search"`
	UseCloudflareBypass         bool              `json:"useCloudflareBypass"`
	SecondValidation            bool              `json:"secondValidation"`
	TaskTemplate                string            `json:"taskTemplate"`
	MagnetExcludeKeywords       string            `json:"magnetExcludeKeywords"`
	ActressCountFilterThreshold int               `json:"actressCountFilterThreshold"`
	FilmCodeFilterThreshold     string            `json:"filmCodeFilterThreshold"`
	MinReleaseDate              string            `json:"minReleaseDate"`
	MagnetContentValidation     bool              `json:"magnetContentValidation"`
	SupplementMagnetTopN        int               `json:"supplementMagnetTopN"`
	DemoMode                    string            `json:"demoMode"`
	DemoLabel                   string            `json:"demoLabel"`
	Headers                     map[string]string `json:"headers"`
}

// DefaultConfig is the single default-value source for crawl execution
// settings.
func DefaultConfig() CrawlConfig {
	return CrawlConfig{
		BaseURL:                     "https://www.javbus.com/",
		Parallel:                    2,
		Timeout:                     30 * time.Second,
		Limit:                       0,
		ItemsPerPage:                30,
		Delay:                       2,
		RetryCount:                  3,
		RetryDelay:                  3 * time.Second,
		Nomag:                       false,
		Allmag:                      false,
		Nopic:                       false,
		StrictSSL:                   true,
		UseCloudflareBypass:         false,
		SecondValidation:            false,
		MagnetExcludeKeywords:       "",
		ActressCountFilterThreshold: 0,
		FilmCodeFilterThreshold:     "",
		MagnetContentValidation:     false,
		SupplementMagnetTopN:        3,
		DemoMode:                    "base",
		TaskTemplate:                "balanced",
	}
}

// ToPageRequestOptions projects crawl config into the plain HTTP request lane
// contract used by crawlfetch/crawlrequest.
func (c *CrawlConfig) ToPageRequestOptions(userAgent string) crawlrequest.PageRequestOptions {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	headers := map[string]string{
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
		"Accept-Encoding":           "gzip, deflate, br",
		"Accept-Language":           "zh-CN,zh;q=0.9,en;q=0.8,en-US;q=0.7",
		"Cache-Control":             "no-cache",
		"Sec-Ch-Ua-Mobile":          "?0",
		"Sec-Ch-Ua-Platform":        "\"Windows\"",
		"Sec-Fetch-Dest":            "document",
		"Sec-Fetch-Mode":            "navigate",
		"Sec-Fetch-Site":            "none",
		"Sec-Fetch-User":            "?1",
		"Upgrade-Insecure-Requests": "1",
		"Connection":                "keep-alive",
	}

	baseURL := c.BaseURL
	if c.Base != "" {
		baseURL = c.Base
	}
	headers["Referer"] = baseURL

	if cookie, ok := c.Headers["Cookie"]; ok && cookie != "" {
		headers["Cookie"] = cookie
	}

	var configCookie string
	if c.Headers != nil {
		configCookie = c.Headers["Cookie"]
	}

	return crawlrequest.PageRequestOptions{
		Headers:           headers,
		ConfigCookie:      configCookie,
		CookieOverride:    "",
		CloudflareCookies: "",
		Proxy:             c.Proxy,
		Timeout:           timeout,
		UserAgent:         userAgent,
		RetryCount:        c.RetryCount,
		RetryDelay:        c.RetryDelay,
	}
}

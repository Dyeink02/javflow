// Package proxy validates proxy settings and normalizes proxy/target inputs for the desktop app.
//
// Ownership summary:
// 1) normalize raw proxy and target strings into a stable runtime shape
// 2) perform connectivity validation for the settings panel
// 3) keep transport-input hygiene separate from crawl execution logic
//
// Boundary rule:
// proxy validation should stay transport-oriented. It must not grow crawler
// fetch/session/Cloudflare business behavior into the settings helper path.
//
// File map for maintainers:
// 1) proxy validation DTOs and regex/defaults
// 2) normalization helpers
// 3) connectivity validation helpers
package proxy

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const fallbackTargetURL = "https://www.javbus.com/"

var hostPortPattern = regexp.MustCompile(`^[^/\s]+:\d+$`)
var schemePattern = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*://`)

type Service struct{}

type ValidationResult struct {
	Status          string `json:"status"`
	NormalizedProxy string `json:"normalizedProxy"`
	Message         string `json:"message"`
	Detail          string `json:"detail"`
	LatencyMs       int64  `json:"latencyMs,omitempty"`
}

func NewService() *Service {
	return &Service{}
}

func NormalizeProxyValue(value string) string {
	rawValue := strings.TrimSpace(value)
	if rawValue == "" {
		return ""
	}

	proxyValue := rawValue
	if !schemePattern.MatchString(proxyValue) && hostPortPattern.MatchString(proxyValue) {
		proxyValue = "http://" + proxyValue
	}

	parsed, err := url.Parse(proxyValue)
	if err != nil || strings.TrimSpace(parsed.Hostname()) == "" {
		return ""
	}

	return strings.TrimRight(parsed.String(), "/")
}

func NormalizeTargetURL(targetURL string) string {
	rawValue := strings.TrimSpace(targetURL)
	if rawValue == "" {
		return fallbackTargetURL
	}

	parsed, err := url.Parse(rawValue)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") {
		return fallbackTargetURL
	}

	if isForbiddenTargetHost(parsed.Hostname()) {
		return fallbackTargetURL
	}

	return parsed.String()
}

func isForbiddenTargetHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return true
	}
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
	}
	return false
}

func (s *Service) ValidateProxy(proxyValue string, targetURL string) ValidationResult {
	rawValue := strings.TrimSpace(proxyValue)
	if rawValue == "" {
		return ValidationResult{
			Status:          "empty",
			NormalizedProxy: "",
			Message:         "代理未填写",
			Detail:          "当前将使用直连方式运行。",
		}
	}

	normalizedProxy := NormalizeProxyValue(rawValue)
	if normalizedProxy == "" {
		return ValidationResult{
			Status:          "invalid",
			NormalizedProxy: "",
			Message:         "代理失败",
			Detail:          "代理地址格式无效，请检查协议、地址和端口。",
		}
	}

	latencyMs, err := s.probeProxy(normalizedProxy, NormalizeTargetURL(targetURL))
	if err != nil {
		return ValidationResult{
			Status:          "invalid",
			NormalizedProxy: normalizedProxy,
			Message:         "代理失败",
			Detail:          err.Error(),
		}
	}

	return ValidationResult{
		Status:          "valid",
		NormalizedProxy: normalizedProxy,
		Message:         "代理正常",
		Detail:          fmt.Sprintf("检测通过，当前连通延迟约 %d ms。", latencyMs),
		LatencyMs:       latencyMs,
	}
}

func (s *Service) probeProxy(proxyURL string, targetURL string) (int64, error) {
	parsedProxy, err := url.Parse(proxyURL)
	if err != nil {
		return 0, fmt.Errorf("代理地址格式无效，请检查协议、地址和端口。")
	}

	transport := &http.Transport{
		Proxy: http.ProxyURL(parsedProxy),
		// 不对代理连接禁用 TLS 证书验证，避免中间人攻击风险。
		// 若因自签证书导致连通性检测失败，请正确配置系统证书链。
	}

	client := &http.Client{
		Timeout:   6 * time.Second,
		Transport: transport,
	}

	request, err := http.NewRequest(http.MethodHead, targetURL, nil)
	if err != nil {
		return 0, err
	}

	request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36")
	request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	startedAt := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return 0, normalizeProbeError(err)
	}
	defer response.Body.Close()

	statusCode := response.StatusCode
	if statusCode == http.StatusProxyAuthRequired {
		return 0, fmt.Errorf("代理认证失败")
	}
	if statusCode <= 0 || statusCode >= 500 {
		return 0, fmt.Errorf("目标站点响应异常（%d）", statusCode)
	}

	return time.Since(startedAt).Milliseconds(), nil
}

func normalizeProbeError(err error) error {
	message := strings.TrimSpace(err.Error())
	lowerMessage := strings.ToLower(message)

	switch {
	case strings.Contains(lowerMessage, "timeout"):
		return fmt.Errorf("连接超时")
	case strings.Contains(lowerMessage, "proxyconnect tcp"):
		return fmt.Errorf("代理连接失败")
	case strings.Contains(lowerMessage, "authenticationrequired"):
		return fmt.Errorf("代理认证失败")
	case strings.Contains(lowerMessage, "connection refused"):
		return fmt.Errorf("代理连接被拒绝")
	default:
		return fmt.Errorf("%s", message)
	}
}

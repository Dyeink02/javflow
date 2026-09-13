package proxy

// Ownership summary:
// 1) resolve the exit IP country through public JSON geolocation endpoints
// 2) expose an advisory Japan/foreign/invalid result for the renderer
// 3) keep geolocation transport separate from ordinary proxy connectivity checks
//
// File map for maintainers:
// 1) region result DTO and public check entrypoint
// 2) endpoint probing and bounded response parsing
// 3) localized country-name mapping

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RegionCheckResult is the read-only exit-node verdict shown by the Actor
// Atlas proxy panel. A failed geo lookup is represented as invalid and never
// changes the saved proxy or blocks unrelated workflows.
type RegionCheckResult struct {
	Status      string `json:"status"`
	CountryCode string `json:"countryCode,omitempty"`
	Country     string `json:"country,omitempty"`
	City        string `json:"city,omitempty"`
	IP          string `json:"ip,omitempty"`
	ViaProxy    bool   `json:"viaProxy"`
	LatencyMs   int64  `json:"latencyMs,omitempty"`
	Message     string `json:"message"`
	Detail      string `json:"detail"`
}

const (
	regionProbeTimeout = 8 * time.Second
	// Geo responses are small JSON documents, but ipinfo may include extra
	// fields. Keep a finite limit without truncating a valid response.
	regionBodyLimit = 64 * 1024
)

// Both endpoints are keyless HTTPS services. The variable is intentionally
// package-local so tests can point it at deterministic local HTTP servers.
var geoEndpoints = []string{
	"https://ipinfo.io/json",
	"https://api.country.is/",
}

// CheckProxyRegion resolves the exit IP country for the supplied proxy. An
// empty proxy probes the direct connection, which also covers system-level VPN
// users. This is advisory information only.
func (s *Service) CheckProxyRegion(proxyValue string) RegionCheckResult {
	viaProxy := false
	proxyURL := ""
	if raw := strings.TrimSpace(proxyValue); raw != "" {
		normalized := NormalizeProxyValue(raw)
		if normalized == "" {
			return RegionCheckResult{
				Status:   "invalid",
				ViaProxy: true,
				Message:  "节点检测失败",
				Detail:   "代理地址格式无效，请检查协议、地址和端口。",
			}
		}
		proxyURL = normalized
		viaProxy = true
	}

	startedAt := time.Now()
	countryCode, country, city, exitIP, err := s.probeGeoEndpoints(proxyURL)
	latencyMs := time.Since(startedAt).Milliseconds()
	if err != nil {
		return RegionCheckResult{
			Status:    "invalid",
			ViaProxy:  viaProxy,
			LatencyMs: latencyMs,
			Message:   "节点检测失败",
			Detail:    err.Error(),
		}
	}

	code := strings.ToUpper(strings.TrimSpace(countryCode))
	if code == "JP" {
		return RegionCheckResult{
			Status:      "japan",
			CountryCode: code,
			Country:     "日本",
			City:        city,
			IP:          exitIP,
			ViaProxy:    viaProxy,
			LatencyMs:   latencyMs,
			Message:     "日本节点",
			Detail:      formatRegionDetail("日本", city, "可加载 FANZA/DMM 官方榜单"),
		}
	}

	if country == "" {
		country = code
	}
	return RegionCheckResult{
		Status:      "foreign",
		CountryCode: code,
		Country:     country,
		City:        city,
		IP:          exitIP,
		ViaProxy:    viaProxy,
		LatencyMs:   latencyMs,
		Message:     fmt.Sprintf("非日本节点（%s）", country),
		Detail:      formatRegionDetail(country, city, "官方 FANZA/DMM 榜单可能受地区限制"),
	}
}

func formatRegionDetail(country, city, suffix string) string {
	location := strings.TrimSpace(country)
	if strings.TrimSpace(city) != "" {
		location += "（" + strings.TrimSpace(city) + "）"
	}
	return fmt.Sprintf("出口 IP 位于%s，%s。", location, suffix)
}

func (s *Service) probeGeoEndpoints(proxyURL string) (string, string, string, string, error) {
	transport := &http.Transport{}
	if proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return "", "", "", "", fmt.Errorf("代理地址格式无效，请检查协议、地址和端口。")
		}
		transport.Proxy = http.ProxyURL(parsed)
	}
	client := &http.Client{Timeout: regionProbeTimeout, Transport: transport}

	var lastErr error
	for _, endpoint := range geoEndpoints {
		code, country, city, exitIP, err := probeGeoEndpoint(client, endpoint)
		if err == nil {
			return code, country, city, exitIP, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("所有出口 IP 检测服务均无响应。")
	}
	return "", "", "", "", normalizeProbeError(lastErr)
}

func probeGeoEndpoint(client *http.Client, endpoint string) (string, string, string, string, error) {
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return "", "", "", "", err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/135.0 Safari/537.36")
	request.Header.Set("Accept", "application/json,text/plain;q=0.9,*/*;q=0.8")
	response, err := client.Do(request)
	if err != nil {
		return "", "", "", "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", "", "", "", fmt.Errorf("出口 IP 检测服务响应异常（%d）", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, regionBodyLimit))
	if err != nil {
		return "", "", "", "", err
	}
	var payload struct {
		IP      string `json:"ip"`
		Query   string `json:"query"`
		Country string `json:"country"`
		City    string `json:"city"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", "", "", "", fmt.Errorf("出口 IP 检测返回内容无法解析。")
	}
	code := strings.ToUpper(strings.TrimSpace(payload.Country))
	if len(code) != 2 {
		return "", "", "", "", fmt.Errorf("出口 IP 检测未返回有效国家代码。")
	}
	exitIP := strings.TrimSpace(payload.IP)
	if exitIP == "" {
		exitIP = strings.TrimSpace(payload.Query)
	}
	return code, countryDisplayName(code), strings.TrimSpace(payload.City), exitIP, nil
}

func countryDisplayName(countryCode string) string {
	switch strings.ToUpper(strings.TrimSpace(countryCode)) {
	case "JP":
		return "日本"
	case "CN":
		return "中国大陆"
	case "HK":
		return "香港"
	case "TW":
		return "台湾"
	case "SG":
		return "新加坡"
	case "KR":
		return "韩国"
	case "US":
		return "美国"
	case "GB":
		return "英国"
	case "DE":
		return "德国"
	case "FR":
		return "法国"
	case "NL":
		return "荷兰"
	case "RU":
		return "俄罗斯"
	case "CA":
		return "加拿大"
	case "AU":
		return "澳大利亚"
	case "MY":
		return "马来西亚"
	case "TH":
		return "泰国"
	case "VN":
		return "越南"
	case "PH":
		return "菲律宾"
	case "ID":
		return "印度尼西亚"
	case "IN":
		return "印度"
	default:
		return strings.ToUpper(strings.TrimSpace(countryCode))
	}
}

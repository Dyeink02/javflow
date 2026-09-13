// Package actresslookup resolves actress crawl targets and count hints without
// starting a crawl.
//
// Maintenance boundary:
// - resolve actress target URLs and total-count hints
// - normalize proxy/base inputs for lookup calls
// - keep request/parsing logic local to this package
// - do not let crawl orchestration or subscription state storage drift in here
//
// Ownership summary:
// 1) expose actress target-resolution and count-hint lookup helpers
// 2) keep request/parsing logic and fallback bases local to this package
// 3) return normalized target profiles without absorbing crawl/subscription state ownership
package actresslookup

import (
	"context"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"

	"javflow/internal/actressalias"
	"javflow/internal/contracts/subscriptiontarget"
	"javflow/internal/proxy"
)

// maxResponseBodySize 限制单次 HTTP 响应读取的最大字节数，防止恶意响应导致 OOM。
// 审计 H-13：HTTP 响应体无大小限制读取。
const maxResponseBodySize = 16 * 1024 * 1024

// File map for maintainers:
// 1) lookup option/profile contracts and default bases
// 2) target URL / actress-name normalization helpers
// 3) search-page and star-page fetch/parse helpers
// 4) final TargetProfile shaping for crawler-prefill/subscription consumers
//
// Troubleshooting rule:
// - actress target resolution/count-hint issues should start here
// - downstream subscription/crawl orchestration should consume the normalized
//   profile returned here, not reimplement lookup parsing

const (
	defaultTimeoutMS    = 15 * time.Second
	defaultItemsPerPage = 30
	defaultCookieHeader = "existmag=mag; age=verified; dv=1; age_verified=1; adult_verified=1; age_verification=1; age_verification_passed=true; is_adult=true; javbus_age=1"
)

var (
	trailingCategoryPattern = regexp.MustCompile(`(有码|无码|破解)\s*$`)
	countPattern            = regexp.MustCompile(`(?s)已有磁力\s*(\d+)\s*部.*?全部影片\s*(\d+)\s*部`)
	defaultBaseOrigins      = []string{
		"https://www.javbus.com",
		"https://www.busjav.cyou",
		"https://www.fanbus.bond",
		"https://www.cdnbus.bond",
	}
	actressLookupMirrorHosts = map[string]struct{}{
		"javbus.com":      {},
		"www.javbus.com":  {},
		"busjav.cyou":     {},
		"www.busjav.cyou": {},
		"fanbus.bond":     {},
		"www.fanbus.bond": {},
		"cdnbus.bond":     {},
		"www.cdnbus.bond": {},
	}
)

// Service is a stateless coordinator that turns a name or target URL into a
// reusable subscriptiontarget.TargetProfile contract.
type Service struct {
	pageFetcher verifiedPageFetcher
	aliases     *actressalias.Index
}

// verifiedPageFetcher is implemented by the existing crawler fetch service.
// Lookup owns actor parsing, while the crawler remains the sole owner of
// browser-based age/verification recovery.
type verifiedPageFetcher interface {
	FetchLookupHTML(ctx context.Context, targetURL string) (string, string, error)
}

// proxyVerifiedPageFetcher is implemented by the Go crawler fetch facade. It
// keeps lookup verification on the proxy that the Actor Atlas user applied,
// rather than on the one captured when the application launched.
type proxyVerifiedPageFetcher interface {
	FetchLookupHTMLWithProxy(ctx context.Context, targetURL string, proxyValue string) (string, string, error)
}

// ResolveOptions is the thin lookup input shared by crawler prefill and future
// subscription refresh flows.
type ResolveOptions struct {
	ActressName   string
	TargetURL     string
	PreferredBase string
	FallbackBases []string
	Proxy         string
	// SkipAliasResolution is used only after an explicit target URL was
	// supplied and direct inspection needs a name-search fallback. The URL
	// already identifies the selected directory, so a colliding local alias
	// must not block that fallback search.
	SkipAliasResolution bool
	// EnrichProfile controls the optional second-source profile lookup. It is
	// enabled by the actor-detail UI, but remains opt-in for crawl/subscription
	// target resolution so a background workflow does not make extra requests.
	EnrichProfile bool
	// BasicProfileOnly is used by the ranking cache warmer. It keeps the same
	// verified identity and body-profile lookup but omits work cards so opening
	// the Actor Atlas does not preload film lists for every ranked performer.
	BasicProfileOnly bool
}

// searchCandidate is one search-result card extracted from /searchstar pages.
type searchCandidate struct {
	ActressName string
	Href        string
}

// starPage is the normalized summary parsed from one actress landing page.
type starPage struct {
	ActressName   string
	ItemsPerPage  int
	MagnetCount   int
	AllCount      int
	HasMovieGrid  bool
	LatestItemURL string
	AvatarURL     string
	Works         []subscriptiontarget.ActressWork
}

// fetchedPage keeps the raw HTML together with the parsed page summary so the
// caller can reuse resolved URL information without reparsing transport state.
type fetchedPage struct {
	HTML        string
	ResolvedURL string
	StarPage    starPage
}

func NewService(fetchers ...verifiedPageFetcher) *Service {
	return NewServiceWithAliasCache("", fetchers...)
}

// NewServiceWithAliasCache keeps the application-data location outside the
// lookup algorithm. Tests may use NewService without touching a user cache.
func NewServiceWithAliasCache(userDataDir string, fetchers ...verifiedPageFetcher) *Service {
	service := &Service{aliases: actressalias.New(userDataDir)}
	if len(fetchers) > 0 {
		service.pageFetcher = fetchers[0]
	}
	return service
}

func normalizeName(value string) string {
	// Searches are frequently entered using Chinese variants while JAV sources
	// publish Japanese glyphs. Normalize the common variants before comparing so
	// "三上悠亚" resolves the same directory as "三上悠亜".
	nameVariants := strings.NewReplacer(
		"亚", "亜", "爱", "愛", "泽", "沢", "桥", "橋", "樱", "桜",
		"岛", "島", "户", "戸", "边", "辺", "叶", "葉", "织", "織",
		"风", "風", "齐", "斉", "园", "園", "宫", "宮", "冈", "岡",
		"华", "華", "优", "優", "内", "内",
	)
	return strings.ToLower(
		strings.NewReplacer(" ", "", "\t", "", "\n", "", "\r", "", "·", "", "・", "", "•", "", "(", "", ")", "", "（", "", "）", "").Replace(
			nameVariants.Replace(strings.TrimSpace(value)),
		),
	)
}

func isVerificationPage(htmlText string) bool {
	normalized := strings.ToLower(strings.TrimSpace(htmlText))
	return strings.Contains(normalized, "age verification javbus") ||
		strings.Contains(normalized, "driver-verify") ||
		strings.Contains(normalized, "just a moment") ||
		strings.Contains(normalized, "one moment, please") ||
		strings.Contains(normalized, "cf-chl")
}

// fetchLookupPage starts with the light HTTP path. When JavBus returns a
// verification document, it delegates to the crawler's existing recovery
// service instead of duplicating or altering the Cloudflare/age-check code.
func (s *Service) fetchLookupPage(ctx context.Context, targetURL string, proxyValue string) (string, string, error) {
	body, resolvedURL, err := fetchHTMLContext(ctx, targetURL, proxyValue)
	if err != nil || !isVerificationPage(body) || s == nil || s.pageFetcher == nil {
		return body, resolvedURL, err
	}

	var verifiedBody string
	var verifiedURL string
	var verifiedErr error
	if proxyFetcher, ok := s.pageFetcher.(proxyVerifiedPageFetcher); ok {
		verifiedBody, verifiedURL, verifiedErr = proxyFetcher.FetchLookupHTMLWithProxy(ctx, targetURL, proxyValue)
	} else {
		verifiedBody, verifiedURL, verifiedErr = s.pageFetcher.FetchLookupHTML(ctx, targetURL)
	}
	if verifiedErr != nil {
		return "", "", verifiedErr
	}
	if isVerificationPage(verifiedBody) {
		return "", "", fmt.Errorf("站点仍处于验证页，请先在 JAV 爬虫中完成一次验证后再搜索")
	}
	return verifiedBody, verifiedURL, nil
}

func toOrigin(input string) string {
	const fallback = "https://www.javbus.com"
	parsed, err := neturl.Parse(strings.TrimSpace(input))
	if err != nil || strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
		return fallback
	}
	return parsed.Scheme + "://" + parsed.Host
}

// IsAllowedActressLookupHost is the single allow-list for actor-directory
// pages and their mirror hosts. Keep it aligned with defaultBaseOrigins so the
// lazy work-page and cover-cache paths accept the same verified sources.
func IsAllowedActressLookupHost(host string) bool {
	normalized := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	_, allowed := actressLookupMirrorHosts[normalized]
	return allowed
}

// IsAllowedActressLookupBaseURL accepts only HTTPS origins from the verified
// actor-directory mirror set. The bridge uses it before a renderer payload can
// influence a network request.
func IsAllowedActressLookupBaseURL(rawURL string) bool {
	parsed, err := neturl.ParseRequestURI(strings.TrimSpace(rawURL))
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.User != nil || parsed.Port() != "" {
		return false
	}
	return IsAllowedActressLookupHost(parsed.Hostname())
}

// IsAllowedActressLookupTargetURL further constrains a lazy work-page request
// to one resolved /star/<slug> page. It prevents the bridge from becoming a
// generic renderer-controlled fetch endpoint.
func IsAllowedActressLookupTargetURL(rawURL string) bool {
	parsed, err := neturl.ParseRequestURI(strings.TrimSpace(rawURL))
	if err != nil || !IsAllowedActressLookupBaseURL(rawURL) || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	segments := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	return len(segments) == 2 && strings.EqualFold(segments[0], "star") && strings.TrimSpace(segments[1]) != ""
}

func uniqStrings(items ...string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func uniqOrigins(items ...string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		origin := toOrigin(trimmed)
		if origin == "" {
			continue
		}
		if _, exists := seen[origin]; exists {
			continue
		}
		seen[origin] = struct{}{}
		result = append(result, origin)
	}
	return result
}

func buildBaseOrigins(options ResolveOptions) []string {
	// Base-origin planning stays centralized here so crawler prefill and future
	// subscription refresh share the same fallback ordering rules.
	items := []string{options.TargetURL, options.PreferredBase}
	items = append(items, options.FallbackBases...)
	items = append(items, defaultBaseOrigins...)
	return uniqOrigins(items...)
}

func newHTTPClient(proxyValue string) (*http.Client, error) {
	// Lookup transport is intentionally small and isolated. If Cloudflare or
	// browser automation becomes necessary, that belongs to a different module.
	// 审计 H-12：TLS 证书验证被禁用。
	// 此处使用系统默认 TLS 配置，启用证书验证，避免 MITM 中间人攻击风险。
	transport := &http.Transport{}

	if normalizedProxy := proxy.NormalizeProxyValue(proxyValue); normalizedProxy != "" {
		parsedProxy, err := neturl.Parse(normalizedProxy)
		if err != nil {
			return nil, fmt.Errorf("代理地址格式无效，请检查协议、地址和端口。")
		}
		transport.Proxy = http.ProxyURL(parsedProxy)
	}

	return &http.Client{
		Timeout:   defaultTimeoutMS,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}, nil
}

func fetchHTML(targetURL string, proxyValue string) (string, string, error) {
	return fetchHTMLContext(context.Background(), targetURL, proxyValue)
}

// fetchHTMLContext is the small HTTP path used by public profile providers.
// Keeping the context-aware request here prevents a profile lookup from
// surviving a cancelled actor-detail request indefinitely.
func fetchHTMLContext(ctx context.Context, targetURL string, proxyValue string) (string, string, error) {
	client, err := newHTTPClient(proxyValue)
	if err != nil {
		return "", "", err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(targetURL), nil)
	if err != nil {
		return "", "", err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36")
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,ja;q=0.8,en;q=0.7")
	request.Header.Set("Cookie", defaultCookieHeader)

	response, err := client.Do(request)
	if err != nil {
		return "", "", err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return "", "", fmt.Errorf("HTTP %d", response.StatusCode)
	}

	// 审计 H-13：限制响应体大小，防止恶意服务器导致 OOM。
	limitedBody := io.LimitReader(response.Body, maxResponseBodySize)
	body, err := io.ReadAll(limitedBody)
	if err != nil {
		return "", "", err
	}

	resolvedURL := strings.TrimSpace(targetURL)
	if response.Request != nil && response.Request.URL != nil {
		resolvedURL = response.Request.URL.String()
	}

	return string(body), resolvedURL, nil
}

func getAttr(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return attr.Val
		}
	}
	return ""
}

func hasClass(node *html.Node, className string) bool {
	classAttr := strings.TrimSpace(getAttr(node, "class"))
	if classAttr == "" {
		return false
	}

	for _, item := range strings.Fields(classAttr) {
		if item == className {
			return true
		}
	}
	return false
}

func nodeText(node *html.Node) string {
	if node == nil {
		return ""
	}

	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current == nil {
			return
		}
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
			builder.WriteString(" ")
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return strings.Join(strings.Fields(builder.String()), " ")
}

func firstNodeBy(node *html.Node, match func(*html.Node) bool) *html.Node {
	if node == nil {
		return nil
	}
	if match(node) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := firstNodeBy(child, match); found != nil {
			return found
		}
	}
	return nil
}

func countNodesBy(node *html.Node, match func(*html.Node) bool) int {
	if node == nil {
		return 0
	}
	count := 0
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current == nil {
			return
		}
		if match(current) {
			count++
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return count
}

// findCandidates only parses search-result markup. Match selection stays in
// selectBestCandidate so parse and policy are not mixed together.
func findCandidates(doc *html.Node, baseOrigin string) []searchCandidate {
	candidates := []searchCandidate{}
	seen := map[string]struct{}{}

	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node == nil {
			return
		}

		if node.Type == html.ElementNode && strings.EqualFold(node.Data, "a") && hasClass(node, "avatar-box") {
			href := strings.TrimSpace(getAttr(node, "href"))
			if strings.Contains(href, "/star/") {
				absoluteURL := href
				if parsedBase, err := neturl.Parse(baseOrigin); err == nil {
					if parsedHref, hrefErr := neturl.Parse(href); hrefErr == nil {
						absoluteURL = parsedBase.ResolveReference(parsedHref).String()
					}
				}

				actressName := ""
				if imageNode := firstNodeBy(node, func(current *html.Node) bool {
					return current.Type == html.ElementNode && strings.EqualFold(current.Data, "img") && strings.TrimSpace(getAttr(current, "title")) != ""
				}); imageNode != nil {
					actressName = strings.TrimSpace(getAttr(imageNode, "title"))
				}
				if actressName == "" {
					if leftNode := firstNodeBy(node, func(current *html.Node) bool {
						return current.Type == html.ElementNode && hasClass(current, "mleft")
					}); leftNode != nil {
						actressName = nodeText(leftNode)
					}
				}
				if actressName == "" {
					actressName = nodeText(node)
				}

				actressName = strings.TrimSpace(trailingCategoryPattern.ReplaceAllString(strings.Join(strings.Fields(actressName), " "), ""))
				if actressName != "" && absoluteURL != "" {
					signature := actressName + "||" + absoluteURL
					if _, exists := seen[signature]; !exists {
						seen[signature] = struct{}{}
						candidates = append(candidates, searchCandidate{
							ActressName: actressName,
							Href:        absoluteURL,
						})
					}
				}
			}
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	return candidates
}

// parseStarPage extracts count and paging hints from a resolved actress page.
func parseStarPage(htmlText string, sourceURLs ...string) (starPage, error) {
	doc, err := html.Parse(strings.NewReader(htmlText))
	if err != nil {
		return starPage{}, err
	}
	sourceURL := ""
	if len(sourceURLs) > 0 {
		sourceURL = strings.TrimSpace(sourceURLs[0])
	}

	bodyText := nodeText(doc)
	match := countPattern.FindStringSubmatch(bodyText)
	magnetCount := 0
	allCount := 0
	if len(match) == 3 {
		fmt.Sscanf(match[1], "%d", &magnetCount)
		fmt.Sscanf(match[2], "%d", &allCount)
	}

	itemsPerPage := countNodesBy(doc, func(node *html.Node) bool {
		return node.Type == html.ElementNode && strings.EqualFold(node.Data, "a") && hasClass(node, "movie-box")
	})
	if itemsPerPage <= 0 {
		itemsPerPage = defaultItemsPerPage
	}

	latestItemURL := ""
	if latestNode := firstNodeBy(doc, func(node *html.Node) bool {
		return node.Type == html.ElementNode && strings.EqualFold(node.Data, "a") && hasClass(node, "movie-box")
	}); latestNode != nil {
		latestItemURL = absolutePublicURL(getAttr(latestNode, "href"), sourceURL)
	}

	actressName := ""
	if starNameNode := firstNodeBy(doc, func(node *html.Node) bool {
		return node.Type == html.ElementNode && hasClass(node, "star-name")
	}); starNameNode != nil {
		actressName = nodeText(starNameNode)
	}
	if actressName == "" {
		if titleNode := firstNodeBy(doc, func(node *html.Node) bool {
			return node.Type == html.ElementNode && strings.EqualFold(node.Data, "title")
		}); titleNode != nil {
			titleText := nodeText(titleNode)
			if titleText != "" {
				actressName = strings.TrimSpace(strings.Split(titleText, "-")[0])
			}
		}
	}

	avatarURL := ""
	if avatarBox := firstNodeBy(doc, func(node *html.Node) bool {
		return node.Type == html.ElementNode && hasClass(node, "avatar-box")
	}); avatarBox != nil {
		if image := firstNodeBy(avatarBox, func(node *html.Node) bool {
			return node.Type == html.ElementNode && strings.EqualFold(node.Data, "img")
		}); image != nil {
			avatarURL = absolutePublicURL(getAttr(image, "src"), sourceURL)
		}
	}
	works := parseJAVBusWorks(doc, sourceURL)

	return starPage{
		ActressName:  strings.TrimSpace(actressName),
		ItemsPerPage: itemsPerPage,
		MagnetCount:  magnetCount,
		AllCount:     allCount,
		HasMovieGrid: countNodesBy(doc, func(node *html.Node) bool {
			return node.Type == html.ElementNode && strings.EqualFold(node.Data, "a") && hasClass(node, "movie-box")
		}) > 0,
		LatestItemURL: strings.TrimSpace(latestItemURL),
		AvatarURL:     avatarURL,
		Works:         works,
	}, nil
}

func selectBestCandidate(candidates []searchCandidate, actressName string) (searchCandidate, string, bool) {
	// Candidate selection must stay deterministic because callers persist the
	// chosen target and later compare counts/URLs against that decision.
	rawTarget := strings.TrimSpace(actressName)
	normalizedTarget := normalizeName(actressName)

	rawExactMatches := make([]searchCandidate, 0, len(candidates))
	exactMatches := make([]searchCandidate, 0, len(candidates))
	containsMatches := make([]searchCandidate, 0, len(candidates))

	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.ActressName) == rawTarget {
			rawExactMatches = append(rawExactMatches, candidate)
		}

		normalizedName := normalizeName(candidate.ActressName)
		if normalizedName == normalizedTarget {
			exactMatches = append(exactMatches, candidate)
		}
		if normalizedTarget != "" && (strings.Contains(normalizedName, normalizedTarget) || strings.Contains(normalizedTarget, normalizedName)) {
			containsMatches = append(containsMatches, candidate)
		}
	}

	switch {
	case len(rawExactMatches) == 1:
		return rawExactMatches[0], "exact", true
	case len(rawExactMatches) > 1:
		return rawExactMatches[0], "exact-ambiguous", true
	case len(exactMatches) == 1:
		return exactMatches[0], "exact", true
	case len(exactMatches) > 1:
		return exactMatches[0], "exact-ambiguous", true
	case len(containsMatches) == 1:
		return containsMatches[0], "contains", true
	case len(candidates) == 1:
		return candidates[0], "single", true
	default:
		return searchCandidate{}, "missing", false
	}
}

func getFillCount(page starPage) int {
	if page.MagnetCount > 0 {
		return page.MagnetCount
	}
	return page.AllCount
}

func isUsableStarPage(page starPage) bool {
	return strings.TrimSpace(page.ActressName) != "" || page.HasMovieGrid || page.AllCount > 0 || page.MagnetCount > 0
}

func buildTargetCandidates(targetURL string, options ResolveOptions) []string {
	trimmedURL := strings.TrimSpace(targetURL)
	if trimmedURL == "" {
		return nil
	}

	candidates := []string{trimmedURL}
	parsed, err := neturl.Parse(trimmedURL)
	if err != nil || strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
		return uniqStrings(candidates...)
	}

	pathSegments := []string{}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if trimmed := strings.TrimSpace(segment); trimmed != "" {
			pathSegments = append(pathSegments, trimmed)
		}
	}

	origins := buildBaseOrigins(options)
	if len(pathSegments) >= 2 && strings.EqualFold(pathSegments[len(pathSegments)-2], "star") {
		slug := pathSegments[len(pathSegments)-1]
		for _, origin := range origins {
			candidates = append(candidates, origin+"/star/"+slug)
		}
		return uniqStrings(candidates...)
	}

	if len(pathSegments) > 0 {
		normalizedPath := strings.Join(pathSegments, "/")
		for _, origin := range origins {
			candidates = append(candidates, origin+"/"+normalizedPath)
		}
	}

	return uniqStrings(candidates...)
}

// fetchStarPage tries the candidate URL set, validates that the response looks
// like an actress directory page, and returns one normalized parse result.
func (s *Service) fetchStarPage(ctx context.Context, targetURL string, options ResolveOptions) (fetchedPage, error) {
	candidates := buildTargetCandidates(targetURL, options)
	errors := make([]string, 0, len(candidates))

	for _, candidateURL := range candidates {
		htmlText, resolvedURL, err := s.fetchLookupPage(ctx, candidateURL, options.Proxy)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s：%s", candidateURL, err.Error()))
			continue
		}

		page, err := parseStarPage(htmlText, resolvedURL)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s：%s", candidateURL, err.Error()))
			continue
		}
		if !isUsableStarPage(page) {
			errors = append(errors, fmt.Sprintf("%s：页面内容无法识别为女优目录", candidateURL))
			continue
		}

		return fetchedPage{
			HTML:        htmlText,
			ResolvedURL: resolvedURL,
			StarPage:    page,
		}, nil
	}

	return fetchedPage{}, fmt.Errorf("%s", strings.Join(errors, "；"))
}

// buildProfile converts parsed lookup data into the shared lightweight target
// contract used by subscription and crawl-prefill flows.
func buildProfile(actressName string, resolvedURL string, page starPage) subscriptiontarget.TargetProfile {
	fillCount := getFillCount(page)
	totalPages := 1
	if fillCount > 0 && page.ItemsPerPage > 0 {
		totalPages = (fillCount + page.ItemsPerPage - 1) / page.ItemsPerPage
	}

	resolvedActressName := strings.TrimSpace(page.ActressName)
	if resolvedActressName == "" {
		resolvedActressName = strings.TrimSpace(actressName)
	}

	profile := subscriptiontarget.TargetProfile{
		ActressName:         strings.TrimSpace(actressName),
		ResolvedActressName: resolvedActressName,
		ResolvedBase:        strings.TrimSpace(resolvedURL),
		LookupBaseOrigin:    toOrigin(resolvedURL),
		MagnetCount:         page.MagnetCount,
		AllCount:            page.AllCount,
		FillCount:           fillCount,
		PreferredCount:      fillCount,
		ItemsPerPage:        page.ItemsPerPage,
		TotalPages:          totalPages,
		LatestItemURL:       strings.TrimSpace(page.LatestItemURL),
		AvatarURL:           strings.TrimSpace(page.AvatarURL),
		Works:               append([]subscriptiontarget.ActressWork(nil), page.Works...),
		DisplayedWorks:      len(page.Works),
		DataSources:         []string{"JAVBus"},
	}
	return profile
}

func selectCanonicalWorks(primary, secondary []subscriptiontarget.ActressWork) []subscriptiontarget.ActressWork {
	if len(primary) > 0 {
		return uniqueWork(primary)
	}
	return uniqueWork(secondary)
}

func (s *Service) enrichProfile(ctx context.Context, profile subscriptiontarget.TargetProfile, options ResolveOptions) subscriptiontarget.TargetProfile {
	if !options.EnrichProfile {
		return profile
	}
	profile.DataFetchedAt = time.Now().Format(time.RFC3339)
	details, err := s.fetchMinnanoProfile(ctx, profile.ResolvedActressName, options.Proxy)
	if err != nil {
		// JAVBus data remains useful when the secondary profile source is
		// unavailable. The UI will show the source list and missing fields rather
		// than manufacturing placeholder measurements.
		return profile
	}
	if details.ResolvedName != "" {
		profile.ResolvedActressName = details.ResolvedName
	}
	if details.AvatarURL != "" {
		if profile.AvatarURL == "" {
			profile.AvatarURL = details.AvatarURL
		}
	}
	if len(details.Images) > 0 {
		profile.PromotionImageURLs = distinctPromotionImages(profile.AvatarURL, profile.PromotionImageURLs, details.Images)
	}
	if len(details.Fields) > 0 {
		profile.ProfileFields = details.Fields
		s.rememberProviderAliases(profile.ResolvedActressName, details.Fields)
	}
	// JAVBus is the canonical source for JAV work identifiers. Minnano's
	// `av123456.html` values are site-internal record IDs, not JAV numbers, so
	// only use its works when the primary JAVBus page returned none.
	if !options.BasicProfileOnly {
		profile.Works = selectCanonicalWorks(profile.Works, details.Works)
		profile.DisplayedWorks = len(details.Works)
		if len(profile.Works) > 0 {
			profile.DisplayedWorks = len(profile.Works)
		}
	}
	if options.BasicProfileOnly {
		profile.Works = nil
		profile.DisplayedWorks = 0
	}
	profile.DataSources = uniqStrings(append(profile.DataSources, "みんなのAV.com")...)
	return profile
}

// distinctPromotionImages keeps the public-photo collection semantically
// separate from the actor avatar. Some public profile pages expose their
// portrait in both fields; displaying it twice makes the Atlas look as though
// two different publicity photos were found.
func distinctPromotionImages(avatarURL string, groups ...[]string) []string {
	avatarKey := canonicalProfileImageURL(avatarURL)
	seen := map[string]struct{}{}
	result := make([]string, 0)
	for _, group := range groups {
		for _, rawURL := range group {
			value := strings.TrimSpace(rawURL)
			key := canonicalProfileImageURL(value)
			if value == "" || key == "" || key == avatarKey {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}

func canonicalProfileImageURL(rawURL string) string {
	value := strings.TrimSpace(rawURL)
	parsed, err := neturl.Parse(value)
	if err != nil || parsed.Host == "" {
		return strings.ToLower(value)
	}
	parsed.Fragment = ""
	parsed.RawQuery = ""
	return strings.ToLower(parsed.String())
}

// ResolveTarget keeps the existing synchronous public contract for callers
// outside the desktop bridge. New UI paths should call ResolveTargetContext so
// closing the desktop application cancels their provider requests promptly.
func (s *Service) ResolveTarget(options ResolveOptions) (subscriptiontarget.TargetProfile, error) {
	return s.ResolveTargetContext(context.Background(), options)
}

// ResolveTargetContext starts from actress name search, selects one candidate,
// and returns a stable target profile for later crawl execution.
func (s *Service) ResolveTargetContext(parent context.Context, options ResolveOptions) (subscriptiontarget.TargetProfile, error) {
	requestedName := strings.TrimSpace(options.ActressName)
	actressName := requestedName
	if actressName == "" {
		return subscriptiontarget.TargetProfile{}, fmt.Errorf("缺少女优名称，无法填充抓取信息。")
	}
	aliasResolution := actressalias.Resolution{Query: requestedName, MatchKind: "missing"}
	if !options.SkipAliasResolution {
		resolvedName, resolvedAlias, aliasErr := s.resolveAliasQuery(requestedName)
		if aliasErr != nil {
			return subscriptiontarget.TargetProfile{}, aliasErr
		}
		actressName = resolvedName
		aliasResolution = resolvedAlias
	}

	origins := buildBaseOrigins(options)
	lookupErrors := make([]string, 0, len(origins))
	if parent == nil {
		parent = context.Background()
	}
	lookupContext, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()

	for _, origin := range origins {
		if lookupContext.Err() != nil {
			lookupErrors = append(lookupErrors, "全站演员检索超时")
			break
		}
		// Use the directory's Japanese glyphs for the request while keeping the
		// original input for strict candidate matching and UI presentation.
		searchStarURL := origin + "/searchstar/" + neturl.QueryEscape(siteSearchName(actressName))
		searchHTML, _, err := s.fetchLookupPage(lookupContext, searchStarURL, options.Proxy)
		if err != nil {
			lookupErrors = append(lookupErrors, fmt.Sprintf("%s：%s", origin, err.Error()))
			continue
		}

		doc, err := html.Parse(strings.NewReader(searchHTML))
		if err != nil {
			lookupErrors = append(lookupErrors, fmt.Sprintf("%s：%s", origin, err.Error()))
			continue
		}

		candidates := findCandidates(doc, origin)
		candidate, matchMode, ok := selectBestCandidate(candidates, actressName)
		if !ok {
			if len(candidates) > 1 {
				names := make([]string, 0, minInt(len(candidates), 4))
				for index, item := range candidates {
					if index >= 4 {
						break
					}
					names = append(names, item.ActressName)
				}
				lookupErrors = append(lookupErrors, fmt.Sprintf("%s：找到多个匹配目录：%s", origin, strings.Join(names, "、")))
			} else {
				lookupErrors = append(lookupErrors, fmt.Sprintf("%s：未找到可用的女优目录。", origin))
			}
			_ = matchMode
			continue
		}

		fetched, err := s.fetchStarPage(lookupContext, candidate.Href, ResolveOptions{
			ActressName:   actressName,
			TargetURL:     candidate.Href,
			PreferredBase: origin,
			FallbackBases: origins,
			Proxy:         options.Proxy,
		})
		if err != nil {
			lookupErrors = append(lookupErrors, fmt.Sprintf("%s：%s", origin, err.Error()))
			continue
		}

		profile := buildProfile(candidate.ActressName, fetched.ResolvedURL, fetched.StarPage)
		profile.RequestedActressName = requestedName
		profile.AliasMatchKind = aliasResolution.MatchKind
		profile.AliasMatchedAs = aliasResolution.MatchedAs
		if options.BasicProfileOnly {
			profile.Works = nil
			profile.DisplayedWorks = 0
		}
		return s.enrichProfile(lookupContext, profile, options), nil
	}

	return subscriptiontarget.TargetProfile{}, fmt.Errorf("未能定位女优目录。%s", strings.Join(lookupErrors, "；"))
}

// InspectTarget keeps the existing synchronous public contract. New UI paths
// should call InspectTargetContext to inherit desktop cancellation.
func (s *Service) InspectTarget(options ResolveOptions) (subscriptiontarget.TargetProfile, error) {
	return s.InspectTargetContext(context.Background(), options)
}

// InspectTargetContext prefers an already known target URL and falls back to
// name resolution only when direct inspection cannot produce a usable profile.
func (s *Service) InspectTargetContext(parent context.Context, options ResolveOptions) (subscriptiontarget.TargetProfile, error) {
	targetURL := strings.TrimSpace(options.TargetURL)
	actressName := strings.TrimSpace(options.ActressName)
	if targetURL == "" {
		return s.ResolveTargetContext(parent, options)
	}

	if parent == nil {
		parent = context.Background()
	}
	inspectContext, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	fetched, err := s.fetchStarPage(inspectContext, targetURL, options)
	if err == nil {
		profile := buildProfile(actressName, fetched.ResolvedURL, fetched.StarPage)
		if options.BasicProfileOnly {
			profile.Works = nil
			profile.DisplayedWorks = 0
		}
		return s.enrichProfile(inspectContext, profile, options), nil
	}
	if actressName == "" {
		return subscriptiontarget.TargetProfile{}, err
	}

	// The caller supplied an explicit target URL, so a local alias collision is
	// not actionable here. Search the original displayed name directly if the
	// explicit page was unavailable.
	fallbackOptions := options
	fallbackOptions.SkipAliasResolution = true
	profile, resolveErr := s.ResolveTargetContext(inspectContext, fallbackOptions)
	if resolveErr != nil {
		return subscriptiontarget.TargetProfile{}, fmt.Errorf("%s；%s", err.Error(), resolveErr.Error())
	}
	return profile, nil
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

package actresslookup

// Ownership summary:
// 1) parse real public actor profile data from the fixed secondary source
// 2) normalize JAVBus and profile-provider work/photo entries into the shared DTO
// 3) keep source validation and strict name matching inside actress lookup
//
// File map for maintainers:
// 1) URL allow-list and common HTML helpers
// 2) JAVBus actor work parsing
// 3) minnano-av profile/JSON-LD/work parsing
// 4) optional profile enrichment orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"

	"javflow/internal/contracts/subscriptiontarget"
)

// Profile enrichment is deliberately limited to a fixed, documented source.
// Do not turn this into an arbitrary URL fetcher: actor names are user input
// and the detail view must not become a server-side request proxy.
const minnanoOrigin = "https://www.minnano-av.com"

var (
	minnanoActressURLPattern = regexp.MustCompile(`(?i)(?:^|/)actress[0-9]+\.html(?:\?[^#]*)?$`)
	minnanoWorkURLPattern    = regexp.MustCompile(`(?i)(?:^|/)av[0-9]+\.html(?:\?[^#]*)?$`)
	minnanoCodePattern       = regexp.MustCompile(`(?i)/av([0-9]+)\.html`)
	minnanoDatePattern       = regexp.MustCompile(`\b(\d{4})[/-](\d{1,2})[/-](\d{1,2})\b`)
	minnanoBirthdayPattern   = regexp.MustCompile(`(\d{4})年\s*(\d{1,2})月\s*(\d{1,2})日`)
)

// absolutePublicURL resolves only ordinary HTTP(S) links. Empty and
// javascript/data URLs are intentionally discarded before they reach the UI.
func absolutePublicURL(rawValue string, baseURL string) string {
	rawValue = strings.TrimSpace(rawValue)
	if rawValue == "" || strings.HasPrefix(strings.ToLower(rawValue), "javascript:") || strings.HasPrefix(strings.ToLower(rawValue), "data:") {
		return ""
	}
	parsed, err := url.Parse(rawValue)
	if err != nil {
		return ""
	}
	if parsed.IsAbs() {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return ""
		}
		return parsed.String()
	}
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return ""
	}
	resolved := base.ResolveReference(parsed)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return ""
	}
	return resolved.String()
}

func uniqueWork(items []subscriptiontarget.ActressWork) []subscriptiontarget.ActressWork {
	seen := make(map[string]struct{}, len(items))
	result := make([]subscriptiontarget.ActressWork, 0, len(items))
	for _, item := range items {
		item.URL = strings.TrimSpace(item.URL)
		if item.URL == "" {
			continue
		}
		if _, exists := seen[item.URL]; exists {
			continue
		}
		seen[item.URL] = struct{}{}
		result = append(result, item)
	}
	return result
}

func parseJAVBusWorks(doc *html.Node, sourceURL string) []subscriptiontarget.ActressWork {
	works := make([]subscriptiontarget.ActressWork, 0)
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node == nil {
			return
		}
		if node.Type == html.ElementNode && strings.EqualFold(node.Data, "a") && hasClass(node, "movie-box") {
			work := subscriptiontarget.ActressWork{
				URL: absolutePublicURL(getAttr(node, "href"), sourceURL),
			}
			if image := firstNodeBy(node, func(current *html.Node) bool {
				return current.Type == html.ElementNode && strings.EqualFold(current.Data, "img")
			}); image != nil {
				work.CoverURL = absolutePublicURL(getAttr(image, "src"), sourceURL)
				if work.CoverURL == "" {
					work.CoverURL = absolutePublicURL(getAttr(image, "data-src"), sourceURL)
				}
				work.Title = strings.TrimSpace(getAttr(image, "title"))
			}
			if work.Title == "" {
				if info := firstNodeBy(node, func(current *html.Node) bool {
					return current.Type == html.ElementNode && hasClass(current, "photo-info")
				}); info != nil {
					work.Title = strings.TrimSpace(nodeText(info))
				}
			}
			if dateNodes := nodesBy(node, func(current *html.Node) bool {
				return current.Type == html.ElementNode && strings.EqualFold(current.Data, "date")
			}); len(dateNodes) > 0 {
				work.Code = strings.TrimSpace(nodeText(dateNodes[0]))
				if len(dateNodes) > 1 {
					work.ReleaseDate = strings.TrimSpace(nodeText(dateNodes[1]))
				}
			}
			if work.URL != "" {
				works = append(works, work)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return uniqueWork(works)
}

func nodesBy(node *html.Node, match func(*html.Node) bool) []*html.Node {
	result := make([]*html.Node, 0)
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current == nil {
			return
		}
		if match(current) {
			result = append(result, current)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return result
}

type minnanoProfile struct {
	ResolvedName string
	AvatarURL    string
	Fields       map[string]string
	Images       []string
	Works        []subscriptiontarget.ActressWork
	URL          string
}

func isMinnanoURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && strings.EqualFold(parsed.Scheme, "https") && strings.EqualFold(parsed.Host, "www.minnano-av.com")
}

func parseMinnanoCandidateURL(rawURL string, baseURL string) string {
	resolved := absolutePublicURL(rawURL, baseURL)
	if !isMinnanoURL(resolved) || !minnanoActressURLPattern.MatchString(strings.TrimSpace(strings.Split(resolved, "#")[0])) {
		return ""
	}
	return resolved
}

func normalizedProfileName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "()（）[]【】")
	return normalizeName(value)
}

func profileNamesFromHeading(value string) []string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "　", " ")
	if index := strings.IndexAny(value, "-|"); index > 0 {
		value = value[:index]
	}
	value = strings.TrimSpace(value)
	result := []string{value}
	if open := strings.IndexAny(value, "（("); open >= 0 {
		if close := strings.LastIndexAny(value, "）)"); close > open {
			primary := strings.TrimSpace(value[:open])
			_, delimiterSize := utf8.DecodeRuneInString(value[open:])
			aliases := strings.TrimSpace(value[open+delimiterSize : close])
			result = []string{primary}
			for _, alias := range strings.Split(aliases, "/") {
				if alias = strings.TrimSpace(alias); alias != "" {
					result = append(result, alias)
				}
			}
		}
	}
	return result
}

func profileNameMatches(target string, names ...string) bool {
	normalizedTarget := normalizedProfileName(target)
	if normalizedTarget == "" {
		return false
	}
	for _, name := range names {
		if normalizedProfileName(name) == normalizedTarget {
			return true
		}
	}
	return false
}

func parseMinnanoJSONLD(doc *html.Node) (map[string]any, string) {
	var parsed map[string]any
	node := firstNodeBy(doc, func(current *html.Node) bool {
		return current.Type == html.ElementNode && strings.EqualFold(current.Data, "script") && strings.EqualFold(getAttr(current, "type"), "application/ld+json")
	})
	if node == nil {
		return nil, ""
	}
	var raw strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			raw.WriteString(child.Data)
		}
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw.String())), &parsed); err != nil {
		return nil, ""
	}
	return parsed, ""
}

func stringFromJSON(value any) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	if object, ok := value.(map[string]any); ok {
		if name, ok := object["name"].(string); ok {
			return strings.TrimSpace(name)
		}
	}
	return ""
}

func profileTableFields(doc *html.Node) map[string]string {
	fields := make(map[string]string)
	labels := map[string]string{
		"生年月日":   "生日",
		"サイズ":    "身材",
		"出身地":    "出生地",
		"所属事務所":  "所属",
		"趣味・特技":  "兴趣/特长",
		"AV出演期間": "出演期间",
		"デビュー作品": "出道作品",
		"ブログ":    "博客",
		"公式サイト":  "官方网站",
		"タグ":     "标签",
	}
	profile := firstNodeBy(doc, func(current *html.Node) bool {
		return current.Type == html.ElementNode && hasClass(current, "act-profile")
	})
	if profile == nil {
		return fields
	}
	for _, row := range nodesBy(profile, func(current *html.Node) bool {
		return current.Type == html.ElementNode && strings.EqualFold(current.Data, "tr")
	}) {
		labelNode := firstNodeBy(row, func(current *html.Node) bool {
			return current.Type == html.ElementNode && strings.EqualFold(current.Data, "span")
		})
		valueNode := firstNodeBy(row, func(current *html.Node) bool {
			return current.Type == html.ElementNode && strings.EqualFold(current.Data, "p")
		})
		if labelNode == nil || valueNode == nil {
			continue
		}
		key := strings.TrimSpace(nodeText(labelNode))
		value := strings.TrimSpace(nodeText(valueNode))
		if translated, ok := labels[key]; ok {
			key = translated
		}
		if key != "" && value != "" {
			fields[key] = value
		}
	}
	return fields
}

func parseMinnanoWorks(doc *html.Node, sourceURL string) []subscriptiontarget.ActressWork {
	works := make([]subscriptiontarget.ActressWork, 0)
	// Minnano places a small "recommended-videos" rail before the actor's
	// actual filmography. Walking the whole document mixes unrelated actors'
	// titles into every profile. Restrict traversal to the actor list only.
	root := firstNodeBy(doc, func(current *html.Node) bool {
		return current.Type == html.ElementNode && hasClass(current, "act-video-list")
	})
	if root == nil {
		return works
	}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node == nil {
			return
		}
		if node.Type == html.ElementNode && strings.EqualFold(node.Data, "a") {
			href := strings.TrimSpace(getAttr(node, "href"))
			resolved := absolutePublicURL(href, sourceURL)
			if resolved != "" && isMinnanoURL(resolved) && minnanoWorkURLPattern.MatchString(strings.TrimSpace(strings.Split(resolved, "#")[0])) {
				work := subscriptiontarget.ActressWork{URL: resolved}
				if match := minnanoCodePattern.FindStringSubmatch(resolved); len(match) == 2 {
					work.Code = "AV" + match[1]
				}
				if image := firstNodeBy(node, func(current *html.Node) bool {
					return current.Type == html.ElementNode && strings.EqualFold(current.Data, "img")
				}); image != nil {
					work.CoverURL = absolutePublicURL(getAttr(image, "data-src"), sourceURL)
					if work.CoverURL == "" {
						work.CoverURL = absolutePublicURL(getAttr(image, "src"), sourceURL)
					}
					work.Title = strings.TrimSpace(getAttr(image, "alt"))
				}
				if titleNode := firstNodeBy(node, func(current *html.Node) bool {
					return current.Type == html.ElementNode && (hasClass(current, "video-title") || hasClass(current, "ttl") || hasClass(current, "av-title"))
				}); titleNode != nil {
					work.Title = strings.TrimSpace(nodeText(titleNode))
				}
				if work.Title == "" {
					work.Title = strings.TrimSpace(nodeText(node))
				}
				if date := minnanoDatePattern.FindStringSubmatch(nodeText(node)); len(date) == 4 {
					work.ReleaseDate = fmt.Sprintf("%s-%02s-%02s", date[1], date[2], date[3])
				}
				works = append(works, work)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return uniqueWork(works)
}

func parseMinnanoProfile(htmlText string, sourceURL string, targetName string) (minnanoProfile, error) {
	doc, err := html.Parse(strings.NewReader(htmlText))
	if err != nil {
		return minnanoProfile{}, err
	}
	profile := minnanoProfile{Fields: profileTableFields(doc)}
	profile.URL = absolutePublicURL(sourceURL, minnanoOrigin)
	if !isMinnanoURL(profile.URL) {
		profile.URL = minnanoOrigin
	}

	var heading *html.Node
	if profileBox := firstNodeBy(doc, func(current *html.Node) bool {
		return current.Type == html.ElementNode && hasClass(current, "act-profile")
	}); profileBox != nil {
		heading = firstNodeBy(profileBox, func(current *html.Node) bool {
			return current.Type == html.ElementNode && strings.EqualFold(current.Data, "h2")
		})
	}
	if heading == nil {
		heading = firstNodeBy(doc, func(current *html.Node) bool {
			return current.Type == html.ElementNode && strings.EqualFold(current.Data, "h1")
		})
	}
	if heading == nil {
		heading = firstNodeBy(doc, func(current *html.Node) bool {
			return current.Type == html.ElementNode && strings.EqualFold(current.Data, "title")
		})
	}
	var headingNames []string
	if heading != nil {
		headingNames = profileNamesFromHeading(nodeText(heading))
	}
	if !profileNameMatches(targetName, headingNames...) {
		// A strict match prevents a same-name or unrelated search result from
		// silently supplying body measurements for the wrong performer.
		return minnanoProfile{}, fmt.Errorf("みんなのAV资料页未严格匹配演员名称")
	}
	profile.ResolvedName = strings.TrimSpace(headingNames[0])

	if structured, _ := parseMinnanoJSONLD(doc); len(structured) > 0 {
		if profile.ResolvedName == "" {
			profile.ResolvedName = stringFromJSON(structured["name"])
		}
		if image := stringFromJSON(structured["image"]); image != "" {
			profile.AvatarURL = absolutePublicURL(image, profile.URL)
		}
		if birth := stringFromJSON(structured["birthDate"]); birth != "" {
			profile.Fields["生日"] = birth
		}
		if place := stringFromJSON(structured["birthPlace"]); place != "" {
			profile.Fields["出生地"] = place
		}
		if affiliation := stringFromJSON(structured["affiliation"]); affiliation != "" {
			profile.Fields["所属"] = affiliation
		}
		if alias := stringFromJSON(structured["alternateName"]); alias != "" {
			profile.Fields["别名"] = alias
		}
		if romanized := stringFromJSON(structured["additionalName"]); romanized != "" {
			if existing := profile.Fields["别名"]; existing != "" {
				profile.Fields["别名"] = existing + " / " + romanized
			} else {
				profile.Fields["别名"] = romanized
			}
		}
		if gender := stringFromJSON(structured["gender"]); gender != "" {
			profile.Fields["性别"] = gender
		}
	}

	if profile.AvatarURL == "" {
		if image := firstNodeBy(doc, func(current *html.Node) bool {
			return current.Type == html.ElementNode && strings.EqualFold(current.Data, "img") && strings.Contains(getAttr(current, "src"), "p_actress_")
		}); image != nil {
			profile.AvatarURL = absolutePublicURL(getAttr(image, "src"), profile.URL)
		}
	}
	if profile.AvatarURL != "" {
		profile.Images = []string{profile.AvatarURL}
	}
	if birth := profile.Fields["生日"]; birth != "" {
		if match := minnanoBirthdayPattern.FindStringSubmatch(birth); len(match) == 4 {
			profile.Fields["生日"] = fmt.Sprintf("%s-%02s-%02s", match[1], match[2], match[3])
		} else if match := minnanoDatePattern.FindStringSubmatch(birth); len(match) == 4 {
			profile.Fields["生日"] = fmt.Sprintf("%s-%02s-%02s", match[1], match[2], match[3])
		}
		if parsed, parseErr := time.Parse("2006-01-02", profile.Fields["生日"]); parseErr == nil {
			age := time.Now().Year() - parsed.Year()
			anniversary := time.Date(time.Now().Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, time.Local)
			if time.Now().Before(anniversary) {
				age--
			}
			if age >= 0 && age < 150 {
				profile.Fields["年龄"] = fmt.Sprintf("%d岁", age)
			}
		}
	}
	profile.Works = parseMinnanoWorks(doc, profile.URL)
	return profile, nil
}

func findMinnanoProfileURLs(doc *html.Node, sourceURL string, targetName string) []string {
	urls := make([]string, 0)
	// A search request may redirect straight to the profile page. Prefer the
	// canonical URL in that case, but still verify the page title in the parser.
	if canonical := firstNodeBy(doc, func(current *html.Node) bool {
		return current.Type == html.ElementNode && strings.EqualFold(current.Data, "link") && strings.EqualFold(getAttr(current, "rel"), "canonical")
	}); canonical != nil {
		if candidate := parseMinnanoCandidateURL(getAttr(canonical, "href"), sourceURL); candidate != "" {
			urls = append(urls, candidate)
		}
	}
	var fallback string
	for _, node := range nodesBy(doc, func(current *html.Node) bool {
		return current.Type == html.ElementNode && strings.EqualFold(current.Data, "a")
	}) {
		resolved := parseMinnanoCandidateURL(getAttr(node, "href"), sourceURL)
		if resolved == "" {
			continue
		}
		if profileNameMatches(targetName, nodeText(node), getAttr(node, "title")) {
			urls = append(urls, resolved)
			continue
		}
		if fallback == "" {
			fallback = resolved
		}
	}
	if len(urls) == 0 && fallback != "" {
		urls = append(urls, fallback)
	}
	return uniqStrings(urls...)
}

func findMinnanoProfileURL(doc *html.Node, sourceURL string, targetName string) string {
	urls := findMinnanoProfileURLs(doc, sourceURL, targetName)
	if len(urls) == 0 {
		return ""
	}
	return urls[0]
}

func (s *Service) fetchMinnanoProfile(ctx context.Context, targetName string, proxyValue string) (minnanoProfile, error) {
	queryURL := minnanoOrigin + "/search_result.php?search_scope=actress&search_word=" + url.QueryEscape(strings.TrimSpace(targetName)) + "&search=Go"
	searchHTML, resolvedURL, err := fetchHTMLContext(ctx, queryURL, proxyValue)
	if err != nil {
		return minnanoProfile{}, err
	}
	profileURLs := findMinnanoProfileURLs(mustParseHTML(searchHTML), resolvedURL, targetName)
	if len(profileURLs) == 0 {
		return minnanoProfile{}, fmt.Errorf("みんなのAV未找到演员资料页")
	}
	var lastErr error
	for _, profileURL := range profileURLs {
		profileHTML := searchHTML
		profileResolvedURL := resolvedURL
		if profileURL != strings.TrimSpace(resolvedURL) || !strings.Contains(searchHTML, "act-profile") {
			profileHTML, profileResolvedURL, err = fetchHTMLContext(ctx, profileURL, proxyValue)
			if err != nil {
				lastErr = err
				continue
			}
		}
		profile, parseErr := parseMinnanoProfile(profileHTML, profileResolvedURL, targetName)
		if parseErr == nil {
			return profile, nil
		}
		lastErr = parseErr
	}
	if lastErr != nil {
		return minnanoProfile{}, lastErr
	}
	return minnanoProfile{}, fmt.Errorf("みんなのAV未找到严格匹配的演员资料页")
}

func mustParseHTML(value string) *html.Node {
	doc, err := html.Parse(strings.NewReader(value))
	if err != nil {
		return &html.Node{Type: html.DocumentNode}
	}
	return doc
}

package crawlparse

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// Package crawlparse owns the HTML-to-structured-data extraction layer for
// index/detail/magnet pages. If the site markup changes, start here.
//
// Ownership summary:
// 1) parse raw HTML into crawl metadata/index/magnet structures
// 2) keep selector/pattern heuristics local to one extraction layer
// 3) avoid mixing network retry or output policy into parsing code
//
// File map for maintainers:
// 1) metadata/index/magnet parse contracts
// 2) regex/selector heuristics for site markup extraction
// 3) HTML traversal and structured result assembly helpers

var (
	gidPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bgid\b\s*[:=]\s*['"]?(\d+)['"]?`),
		regexp.MustCompile(`(?i)["']gid["']\s*:\s*["']?(\d+)["']?`),
		regexp.MustCompile(`(?i)\bgid\b\s*[:=]\s*parseInt\(\s*['"]?(\d+)['"]?`),
		regexp.MustCompile(`(?i)[?&]gid=(\d+)`),
		regexp.MustCompile(`(?i)data-gid\s*=\s*["'](\d+)["']`),
	}
	ucPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\buc\b\s*[:=]\s*['"]?(\d+)['"]?`),
		regexp.MustCompile(`(?i)["']uc["']\s*:\s*["']?(\d+)["']?`),
		regexp.MustCompile(`(?i)\buc\b\s*[:=]\s*parseInt\(\s*['"]?(\d+)['"]?`),
		regexp.MustCompile(`(?i)[?&]uc=(\d+)`),
		regexp.MustCompile(`(?i)data-uc\s*=\s*["'](\d+)["']`),
	}
	imgPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bimg\b\s*[:=]\s*['"]([^'"]+)['"]`),
		regexp.MustCompile(`(?i)["']img["']\s*:\s*["']([^"']+)["']`),
		regexp.MustCompile(`(?i)[?&]img=([^&"'>\s]+)`),
		regexp.MustCompile(`(?i)data-img\s*=\s*["']([^"']+)["']`),
	}
)

// Metadata is the detail-page parse contract used before magnet fetching and
// filmData projection.
type Metadata struct {
	GID         string   `json:"gid"`
	UC          string   `json:"uc"`
	Img         string   `json:"img"`
	Title       string   `json:"title"`
	Category    []string `json:"category"`
	Actress     []string `json:"actress"`
	ReleaseDate string   `json:"releaseDate,omitempty"`
	Maker       string   `json:"maker,omitempty"`
	Label       string   `json:"label,omitempty"`
	Series      string   `json:"series,omitempty"`
}

// FilmData is the persisted crawler artifact shape written to filmData.json.
type FilmData struct {
	Title       string   `json:"title"`
	SourceLink  string   `json:"sourceLink"`
	Category    []string `json:"category"`
	Actress     []string `json:"actress"`
	CoverImage  string   `json:"coverImage,omitempty"`
	ReleaseDate string   `json:"releaseDate,omitempty"`
	Maker       string   `json:"maker,omitempty"`
	Label       string   `json:"label,omitempty"`
	Series      string   `json:"series,omitempty"`
}

// ParsePageLinks extracts movie-box links from one listing page and leaves
// paging/control flow to upper layers.
func ParsePageLinks(htmlText string) []string {
	root, err := html.Parse(strings.NewReader(htmlText))
	if err != nil {
		return nil
	}
	links := make([]string, 0)
	seen := map[string]struct{}{}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" && (hasClass(node, "movie-box") || looksLikeFilmDetailHref(getAttr(node, "href"))) {
			if href := strings.TrimSpace(getAttr(node, "href")); href != "" {
				if _, exists := seen[href]; !exists {
					seen[href] = struct{}{}
					links = append(links, href)
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return links
}

func looksLikeFilmDetailHref(href string) bool {
	trimmed := strings.TrimSpace(href)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(strings.ToLower(trimmed), "javascript:") {
		return false
	}
	parsed, err := url.Parse(trimmed)
	if err == nil {
		trimmed = parsed.Path
	}
	segment := strings.Trim(strings.TrimSpace(trimmed), "/")
	if segment == "" || strings.Contains(segment, "/") {
		return false
	}
	return regexp.MustCompile(`(?i)^[a-z]{2,12}-?\d{1,8}[a-z]*$`).MatchString(segment)
}

// ParseMetadata converts one detail page into the structured fields required by
// magnet fetching and artifact writing.
func ParseMetadata(htmlText string) (Metadata, error) {
	root, err := html.Parse(strings.NewReader(htmlText))
	if err != nil {
		return Metadata{}, err
	}

	scriptTexts := collectElementTexts(root, "script")
	primarySources := make([]string, 0, len(scriptTexts)+1)
	for _, scriptText := range scriptTexts {
		if containsAnyIgnoreCase(scriptText, []string{"gid", "uc", "img", "uncledatoolsbyajax", "sample_dmm"}) {
			primarySources = append(primarySources, scriptText)
		}
	}
	sources := append(primarySources, scriptTexts...)
	sources = append(sources, htmlText)

	gid := extractMetadataField(sources, gidPatterns)
	uc := extractMetadataField(sources, ucPatterns)
	img := strings.ReplaceAll(extractMetadataField(sources, imgPatterns), `\/`, `/`)
	if img == "" {
		img = firstNonEmpty(
			findImageByAncestorClass(root, "div", "bigImage"),
			findImageByAncestorClass(root, "a", "bigImage"),
			findMetaContentByProperty(root, "og:image"),
		)
	}

	if gid == "" || uc == "" || img == "" {
		return Metadata{}, fmt.Errorf("failed to parse required metadata from page")
	}

	title := firstNonEmpty(
		findFirstElementText(root, "h3"),
		findMetaContentByProperty(root, "og:title"),
		findFirstElementText(root, "title"),
	)

	return Metadata{
		GID:         gid,
		UC:          uc,
		Img:         img,
		Title:       title,
		Category:    ParseCategories(htmlText),
		Actress:     ParseActress(htmlText),
		ReleaseDate: ParseReleaseDate(htmlText),
		Maker:       ParseInfoField(htmlText, infoFieldMaker),
		Label:       ParseInfoField(htmlText, infoFieldLabel),
		Series:      ParseInfoField(htmlText, infoFieldSeries),
	}, nil
}

// Info field labels vary with the page locale and have changed slightly over
// time. Keep the accepted aliases in one place so a markup/translation change
// does not leak into the crawler runner or output writer.
const (
	infoFieldMaker  = "maker"
	infoFieldLabel  = "label"
	infoFieldSeries = "series"
)

var infoFieldAliases = map[string][]string{
	infoFieldMaker: {
		"maker", "manufacturer", "studio", "productioncompany", "producer",
		"メーカー", "制作商", "制作公司", "片商",
	},
	infoFieldLabel: {
		"label", "レーベル", "厂牌",
	},
	infoFieldSeries: {
		"series", "シリーズ", "系列",
	},
}

// ParseInfoField extracts one value from JavBus-style information rows. The
// parser intentionally uses the row's label and descendant anchor rather than
// positional selectors, because rows are optional and their order is not
// stable across mirrors/locales.
func ParseInfoField(htmlText string, field string) string {
	root, err := html.Parse(strings.NewReader(htmlText))
	if err != nil {
		return ""
	}
	aliases := infoFieldAliases[strings.ToLower(strings.TrimSpace(field))]
	if len(aliases) == 0 {
		return ""
	}

	var result string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if result != "" || node == nil {
			return
		}
		if node.Type == html.ElementNode && (node.Data == "p" || node.Data == "li" || node.Data == "div") {
			labelNode := firstDescendantByTag(node, "span")
			label := normalizeInfoFieldLabel(nodeText(labelNode))
			if matchesInfoFieldAlias(label, aliases) {
				result = firstNonEmpty(
					firstDescendantTextByTag(node, "a"),
					firstDescendantTextByTag(node, "strong"),
					valueTextAfterLabel(node, labelNode),
				)
				return
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return strings.TrimSpace(result)
}

func normalizeInfoFieldLabel(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	return strings.Map(func(r rune) rune {
		switch r {
		case ':', '\uff1a', ' ', '\t', '\r', '\n':
			return -1
		default:
			return r
		}
	}, value)
}

func matchesInfoFieldAlias(label string, aliases []string) bool {
	if label == "" {
		return false
	}
	for _, alias := range aliases {
		if label == normalizeInfoFieldLabel(alias) {
			return true
		}
	}
	return false
}

func firstDescendantByTag(root *html.Node, tag string) *html.Node {
	if root == nil {
		return nil
	}
	var result *html.Node
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if result != nil || node == nil {
			return
		}
		if node.Type == html.ElementNode && strings.EqualFold(node.Data, tag) {
			result = node
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		walk(child)
	}
	return result
}

func firstDescendantTextByTag(root *html.Node, tag string) string {
	return strings.TrimSpace(nodeText(firstDescendantByTag(root, tag)))
}

func valueTextAfterLabel(root, labelNode *html.Node) string {
	if root == nil {
		return ""
	}
	text := strings.TrimSpace(nodeText(root))
	label := strings.TrimSpace(nodeText(labelNode))
	if label == "" {
		return text
	}
	text = strings.TrimSpace(strings.TrimPrefix(text, label))
	return strings.TrimSpace(strings.TrimLeft(text, ":： \\t\\r\\n"))
}

// ParseCategories extracts normalized genre labels from one detail page.
func ParseCategories(htmlText string) []string {
	root, err := html.Parse(strings.NewReader(htmlText))
	if err != nil {
		return nil
	}
	values := make([]string, 0)
	seen := map[string]struct{}{}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" && hasAncestor(node, func(parent *html.Node) bool {
			return parent.Data == "span" && hasClass(parent, "genre")
		}) {
			value := strings.TrimSpace(nodeText(node))
			if value != "" {
				if _, exists := seen[value]; !exists {
					seen[value] = struct{}{}
					values = append(values, value)
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return values
}

// ParseActress extracts the actress list exactly as the detail page exposes it.
func ParseActress(htmlText string) []string {
	root, err := html.Parse(strings.NewReader(htmlText))
	if err != nil {
		return nil
	}
	values := make([]string, 0)
	seen := map[string]struct{}{}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" && hasAncestor(node, func(parent *html.Node) bool {
			return hasClass(parent, "star-name")
		}) {
			value := strings.TrimSpace(nodeText(node))
			if value != "" {
				if _, exists := seen[value]; !exists {
					seen[value] = struct{}{}
					values = append(values, value)
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return values
}

// releaseDatePattern normalizes recognized date strings into YYYY-MM-DD.
// It tolerates common JAV site formats: "2023-01-01", "2023/01/01",
// "2023年01月01日", and "Jan 01, 2023".
var releaseDatePattern = regexp.MustCompile(`(?i)(\d{4})\s*[-/年．.]\s*(\d{1,2})\s*[-/月．.]\s*(\d{1,2})\s*[日]?`)
var releaseDateLabelPattern = regexp.MustCompile(`(?i)(發行日期|发行日期|発売日|Release\s*Date|発売日期)`)

// ParseReleaseDate extracts the release date from a JavBus-style detail page.
// It searches for info rows whose header text matches a known date label and
// returns the first date found in YYYY-MM-DD form, or an empty string.
func ParseReleaseDate(htmlText string) string {
	root, err := html.Parse(strings.NewReader(htmlText))
	if err != nil {
		return ""
	}

	var result string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if result != "" {
			return
		}

		if node.Type == html.ElementNode && (node.Data == "p" || node.Data == "div" || node.Data == "span") {
			text := nodeText(node)
			if releaseDateLabelPattern.MatchString(text) {
				if matches := releaseDatePattern.FindStringSubmatch(text); len(matches) >= 4 {
					year := matches[1]
					month := matches[2]
					day := matches[3]
					result = fmt.Sprintf("%s-%02s-%02s", year, month, day)
					return
				}
			}
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return result
}

// ParseFilmData projects parsed metadata into the persisted crawl artifact
// shape used by organizer/subscription downstream.
func ParseFilmData(metadata Metadata, link string) FilmData {
	filmData := FilmData{
		Title:       strings.TrimSpace(metadata.Title),
		SourceLink:  strings.TrimSpace(link),
		Category:    append([]string(nil), metadata.Category...),
		Actress:     append([]string(nil), metadata.Actress...),
		ReleaseDate: strings.TrimSpace(metadata.ReleaseDate),
		Maker:       strings.TrimSpace(metadata.Maker),
		Label:       strings.TrimSpace(metadata.Label),
		Series:      strings.TrimSpace(metadata.Series),
	}
	if strings.TrimSpace(metadata.Img) != "" {
		filmData.CoverImage = strings.TrimSpace(metadata.Img)
	}
	return filmData
}

// ExtractAntiBlockURLs parses informational "anti block" links without mixing
// them into the main movie-link extraction path.
func ExtractAntiBlockURLs(htmlText string) []string {
	root, err := html.Parse(strings.NewReader(htmlText))
	if err != nil {
		return nil
	}
	result := make([]string, 0)
	seen := map[string]struct{}{}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "div" && hasClass(node, "alert") && hasClass(node, "alert-info") {
			collectAntiBlockLinks(node, seen, &result)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return result
}

func collectAntiBlockLinks(root *html.Node, seen map[string]struct{}, result *[]string) {
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "div" &&
			hasClass(node, "col-xs-12") &&
			hasClass(node, "col-md-6") &&
			hasClass(node, "col-lg-3") &&
			hasClass(node, "text-center") {
			if containsAnyIgnoreCase(nodeText(findFirstChild(node, "strong")), []string{"防屏蔽地址"}) {
				if linkNode := findFirstChild(node, "a"); linkNode != nil {
					if href := strings.TrimSpace(getAttr(linkNode, "href")); href != "" {
						if _, exists := seen[href]; !exists {
							seen[href] = struct{}{}
							*result = append(*result, href)
						}
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
}

func extractMetadataField(sources []string, patterns []*regexp.Regexp) string {
	for _, source := range sources {
		for _, pattern := range patterns {
			matches := pattern.FindStringSubmatch(source)
			if len(matches) >= 2 && strings.TrimSpace(matches[1]) != "" {
				return strings.TrimSpace(matches[1])
			}
		}
	}
	return ""
}

func collectElementTexts(root *html.Node, tag string) []string {
	result := make([]string, 0)
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && strings.EqualFold(node.Data, tag) {
			text := strings.TrimSpace(nodeText(node))
			if text != "" {
				result = append(result, text)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return result
}

func findFirstElementText(root *html.Node, tag string) string {
	var result string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if result != "" {
			return
		}
		if node.Type == html.ElementNode && strings.EqualFold(node.Data, tag) {
			result = strings.TrimSpace(nodeText(node))
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return result
}

func findMetaContentByProperty(root *html.Node, property string) string {
	var result string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if result != "" {
			return
		}
		if node.Type == html.ElementNode && node.Data == "meta" && strings.EqualFold(getAttr(node, "property"), property) {
			result = strings.TrimSpace(getAttr(node, "content"))
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return result
}

func findImageByAncestorClass(root *html.Node, ancestorTag string, className string) string {
	var result string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if result != "" {
			return
		}
		if node.Type == html.ElementNode && node.Data == "img" {
			if hasAncestor(node, func(parent *html.Node) bool {
				return parent.Data == ancestorTag && hasClass(parent, className)
			}) {
				result = strings.TrimSpace(getAttr(node, "src"))
				return
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return result
}

func findFirstChild(root *html.Node, tag string) *html.Node {
	var result *html.Node
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if result != nil {
			return
		}
		if node.Type == html.ElementNode && strings.EqualFold(node.Data, tag) {
			result = node
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	if root != nil {
		for child := root.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	return result
}

func hasAncestor(node *html.Node, predicate func(*html.Node) bool) bool {
	for parent := node.Parent; parent != nil; parent = parent.Parent {
		if predicate(parent) {
			return true
		}
	}
	return false
}

func hasClass(node *html.Node, className string) bool {
	classes := strings.Fields(strings.TrimSpace(getAttr(node, "class")))
	for _, item := range classes {
		if item == className {
			return true
		}
	}
	return false
}

func getAttr(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, key) {
			return attr.Val
		}
	}
	return ""
}

func nodeText(node *html.Node) string {
	if node == nil {
		return ""
	}
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return strings.TrimSpace(builder.String())
}

func containsAnyIgnoreCase(value string, keywords []string) bool {
	normalized := strings.ToLower(value)
	for _, keyword := range keywords {
		if strings.Contains(normalized, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

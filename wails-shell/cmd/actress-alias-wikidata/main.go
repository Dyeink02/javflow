// Command actress-alias-wikidata builds a distributable alias pack from the
// CC0 Wikidata labels. It is a maintainer-only generator: the desktop app
// consumes the generated JSON locally and never contacts Wikidata at runtime.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"javflow/internal/actressalias"
)

const queryEndpoint = "https://query.wikidata.org/sparql"

// queryText deliberately requires both Japanese and Chinese labels.
// This keeps the generated pack to source-provided facts rather than a machine
// translation or a guessed Kanji-to-kana conversion.
const queryText = `SELECT ?item ?ja ?zh WHERE {
  ?item wdt:P27 wd:Q17.
  ?item wdt:P21 wd:Q6581072.
  ?item wdt:P106/wdt:P279* wd:Q488111.
  ?item rdfs:label ?ja. FILTER(LANG(?ja) = "ja").
  ?item rdfs:label ?zh. FILTER(LANG(?zh) = "zh")
} ORDER BY ?ja`

type value struct {
	Value string `json:"value"`
}

type wikidataBinding struct {
	Item     value `json:"item"`
	Japanese value `json:"ja"`
	Chinese  value `json:"zh"`
}

type wikidataDocument struct {
	Results struct {
		Bindings []wikidataBinding `json:"bindings"`
	} `json:"results"`
}

type rejected struct {
	Entity   string `json:"entity"`
	Japanese string `json:"japanese"`
	Chinese  string `json:"chinese"`
	Reason   string `json:"reason"`
}

type report struct {
	SourceURL string     `json:"sourceUrl"`
	License   string     `json:"license"`
	Imported  int        `json:"imported"`
	Rejected  []rejected `json:"rejected,omitempty"`
}

func main() {
	basePath := flag.String("base", "", "existing bundled alias JSON to retain")
	outputPath := flag.String("output", "", "generated alias JSON output")
	reportPath := flag.String("report", "", "optional import report JSON")
	proxyValue := flag.String("proxy", "", "optional HTTP proxy")
	flag.Parse()

	if strings.TrimSpace(*basePath) == "" || strings.TrimSpace(*outputPath) == "" {
		fmt.Fprintln(os.Stderr, "-base and -output are required")
		flag.Usage()
		os.Exit(2)
	}
	base, err := readRecords(*basePath)
	if err != nil {
		fail(err)
	}
	document, err := fetchWikidata(*proxyValue)
	if err != nil {
		fail(err)
	}
	generated, rejectedRows := recordsFromWikidata(document)
	merged, mergeRejected := mergeRecords(base, generated)
	rejectedRows = append(rejectedRows, mergeRejected...)
	if err := writeJSON(*outputPath, merged); err != nil {
		fail(err)
	}
	if strings.TrimSpace(*reportPath) != "" {
		reportValue := report{
			SourceURL: queryURL(),
			License:   "CC0-1.0 (Wikidata data)",
			Imported:  len(merged) - len(base),
			Rejected:  rejectedRows,
		}
		if err := writeJSON(*reportPath, reportValue); err != nil {
			fail(err)
		}
	}
	fmt.Printf("bundled aliases: %d; Wikidata additions: %d; rejected: %d\n", len(merged), len(merged)-len(base), len(rejectedRows))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func queryURL() string {
	params := url.Values{"format": {"json"}, "query": {queryText}}
	return queryEndpoint + "?" + params.Encode()
}

func fetchWikidata(proxyValue string) (wikidataDocument, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if strings.TrimSpace(proxyValue) != "" {
		proxyURL, err := url.Parse(strings.TrimSpace(proxyValue))
		if err != nil {
			return wikidataDocument{}, fmt.Errorf("parse proxy: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	client := &http.Client{Transport: transport, Timeout: 45 * time.Second}
	request, err := http.NewRequest(http.MethodGet, queryURL(), nil)
	if err != nil {
		return wikidataDocument{}, err
	}
	request.Header.Set("Accept", "application/sparql-results+json")
	request.Header.Set("User-Agent", "JavFlow alias catalog (https://github.com/Dyeink02/javflow)")
	response, err := client.Do(request)
	if err != nil {
		return wikidataDocument{}, fmt.Errorf("request Wikidata: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return wikidataDocument{}, fmt.Errorf("Wikidata returned HTTP %d", response.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return wikidataDocument{}, fmt.Errorf("read Wikidata response: %w", err)
	}
	var document wikidataDocument
	if err := json.Unmarshal(payload, &document); err != nil {
		return wikidataDocument{}, fmt.Errorf("decode Wikidata response: %w", err)
	}
	return document, nil
}

func recordsFromWikidata(document wikidataDocument) ([]actressalias.Record, []rejected) {
	records := make([]actressalias.Record, 0, len(document.Results.Bindings))
	rejectedRows := make([]rejected, 0)
	for _, row := range document.Results.Bindings {
		canonical := strings.TrimSpace(row.Japanese.Value)
		alias := strings.TrimSpace(row.Chinese.Value)
		entity := entityID(row.Item.Value)
		if entity == "" || !hasJapaneseName(canonical) || !hasChineseName(alias) {
			rejectedRows = append(rejectedRows, rejected{Entity: entity, Japanese: canonical, Chinese: alias, Reason: "missing a usable Japanese/Chinese label pair"})
			continue
		}
		records = append(records, actressalias.Record{
			Canonical:  canonical,
			Aliases:    []string{alias},
			Source:     "wikidata-cc0:" + entity,
			Confidence: "source-labeled",
		})
	}
	sort.Slice(records, func(left, right int) bool { return records[left].Canonical < records[right].Canonical })
	return records, rejectedRows
}

func entityID(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	entity := filepath.Base(parsed.Path)
	if len(entity) < 2 || entity[0] != 'Q' {
		return ""
	}
	for _, value := range entity[1:] {
		if value < '0' || value > '9' {
			return ""
		}
	}
	return entity
}

func hasJapaneseName(value string) bool {
	for _, item := range value {
		if (item >= '\u3040' && item <= '\u30ff') || unicode.In(item, unicode.Han) {
			return true
		}
	}
	return false
}

func hasChineseName(value string) bool {
	for _, item := range value {
		if unicode.In(item, unicode.Han) {
			return true
		}
	}
	return false
}

func readRecords(path string) ([]actressalias.Record, error) {
	payload, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	var records []actressalias.Record
	if err := json.Unmarshal(payload, &records); err != nil {
		return nil, err
	}
	return records, nil
}

func mergeRecords(base, additions []actressalias.Record) ([]actressalias.Record, []rejected) {
	byCanonical := make(map[string]actressalias.Record, len(base)+len(additions))
	owners := make(map[string]string)
	rejectedRows := make([]rejected, 0)
	for _, record := range append(append([]actressalias.Record(nil), base...), additions...) {
		canonical := strings.TrimSpace(record.Canonical)
		if canonical == "" {
			continue
		}
		aliases := append([]string{canonical}, record.Aliases...)
		conflict := ""
		for _, alias := range aliases {
			key := actressalias.Normalize(alias)
			if owner := owners[key]; key != "" && owner != "" && owner != canonical {
				conflict = owner
				break
			}
		}
		if conflict != "" {
			rejectedRows = append(rejectedRows, rejected{Entity: record.Source, Japanese: canonical, Chinese: strings.Join(record.Aliases, " / "), Reason: "alias collision with " + conflict})
			continue
		}
		current := byCanonical[canonical]
		if current.Canonical == "" {
			current = record
		} else {
			current.Aliases = append(current.Aliases, record.Aliases...)
		}
		current.Canonical = canonical
		current.Aliases = uniqueAliases(current.Aliases)
		byCanonical[canonical] = current
		for _, alias := range append([]string{canonical}, current.Aliases...) {
			if key := actressalias.Normalize(alias); key != "" {
				owners[key] = canonical
			}
		}
	}
	merged := make([]actressalias.Record, 0, len(byCanonical))
	for _, record := range byCanonical {
		merged = append(merged, record)
	}
	sort.Slice(merged, func(left, right int) bool { return merged[left].Canonical < merged[right].Canonical })
	return merged, rejectedRows
}

func uniqueAliases(aliases []string) []string {
	seen := map[string]struct{}{}
	output := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		alias = strings.TrimSpace(alias)
		key := actressalias.Normalize(alias)
		if alias == "" || key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		output = append(output, alias)
	}
	sort.Strings(output)
	return output
}

func writeJSON(path string, value any) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("output path is required")
	}
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Clean(path)), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Clean(path), append(payload, '\n'), 0o644)
}

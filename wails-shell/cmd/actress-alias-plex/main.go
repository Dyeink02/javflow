// Command actress-alias-plex imports the MIT-licensed plex-jav actress alias
// table into JavFlow's shipped alias pack. It is a maintainer tool: the app
// itself stays fully local and never contacts this upstream project.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"javflow/internal/actressalias"
)

const (
	upstreamRepository = "https://github.com/nxxxsooo/plex-jav"
	upstreamCommit     = "3236bed"
	upstreamFile       = "scraper/data/actress_alias.json"
)

type rejectedAlias struct {
	Canonical string `json:"canonical"`
	Alias     string `json:"alias"`
	Reason    string `json:"reason"`
}

type importReport struct {
	Repository      string          `json:"repository"`
	Commit          string          `json:"commit"`
	Path            string          `json:"path"`
	License         string          `json:"license"`
	SHA256          string          `json:"sha256"`
	ImportedRecords int             `json:"importedRecords"`
	ImportedAliases int             `json:"importedAliases"`
	MergedRecords   int             `json:"mergedRecords"`
	RejectedAliases []rejectedAlias `json:"rejectedAliases,omitempty"`
	GeneratedAt     string          `json:"generatedAt"`
}

func main() {
	basePath := flag.String("base", "", "existing bundled alias JSON")
	outputPath := flag.String("output", "", "merged alias JSON output")
	reportPath := flag.String("report", "", "optional import report JSON output")
	inputPath := flag.String("input", "", "optional downloaded upstream JSON; otherwise fetch the pinned upstream revision")
	proxyValue := flag.String("proxy", "", "optional HTTP proxy for the pinned upstream download")
	flag.Parse()
	if strings.TrimSpace(*basePath) == "" || strings.TrimSpace(*outputPath) == "" {
		fail(errors.New("-base and -output are required"))
	}

	base, err := readRecords(*basePath)
	if err != nil {
		fail(fmt.Errorf("read base pack: %w", err))
	}
	payload, err := readInput(*inputPath, *proxyValue)
	if err != nil {
		fail(err)
	}
	aliases, err := parseUpstream(payload)
	if err != nil {
		fail(err)
	}
	base = removePreviousPlexImport(base, aliases)
	merged, report := mergePlexRecords(base, aliases, sha256Hex(payload), time.Now().UTC())
	if err := writeJSON(*outputPath, merged); err != nil {
		fail(err)
	}
	if strings.TrimSpace(*reportPath) != "" {
		if err := writeJSON(*reportPath, report); err != nil {
			fail(err)
		}
	}
	fmt.Printf("aliases: %d -> %d records; added %d aliases; rejected %d collisions\n", len(base), len(merged), report.ImportedAliases, len(report.RejectedAliases))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func upstreamURL() string {
	return "https://raw.githubusercontent.com/nxxxsooo/plex-jav/" + upstreamCommit + "/" + upstreamFile
}

func readInput(path, proxyValue string) ([]byte, error) {
	if strings.TrimSpace(path) != "" {
		payload, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return nil, fmt.Errorf("read input: %w", err)
		}
		return payload, nil
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if strings.TrimSpace(proxyValue) != "" {
		proxyURL, err := http.NewRequest(http.MethodGet, strings.TrimSpace(proxyValue), nil)
		if err != nil {
			return nil, fmt.Errorf("parse proxy: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxyURL.URL)
	}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	response, err := client.Get(upstreamURL())
	if err != nil {
		return nil, fmt.Errorf("download pinned plex-jav alias table: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pinned plex-jav alias table returned HTTP %d", response.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read pinned plex-jav alias table: %w", err)
	}
	return payload, nil
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

func parseUpstream(payload []byte) (map[string][]string, error) {
	payload = trimUTF8BOM(payload)
	var aliases map[string][]string
	if err := json.Unmarshal(payload, &aliases); err != nil {
		return nil, fmt.Errorf("parse plex-jav alias table: %w", err)
	}
	if len(aliases) == 0 {
		return nil, errors.New("plex-jav alias table is empty")
	}
	return aliases, nil
}

func trimUTF8BOM(payload []byte) []byte {
	if len(payload) >= 3 && payload[0] == 0xef && payload[1] == 0xbb && payload[2] == 0xbf {
		return payload[3:]
	}
	return payload
}

func mergePlexRecords(base []actressalias.Record, upstream map[string][]string, digest string, now time.Time) ([]actressalias.Record, importReport) {
	byCanonical := make(map[string]actressalias.Record, len(base)+len(upstream))
	owners := make(map[string]string)
	for _, record := range base {
		canonical := strings.TrimSpace(record.Canonical)
		if canonical == "" {
			continue
		}
		record.Canonical = canonical
		record.Aliases = uniqueAliases(record.Aliases)
		byCanonical[canonical] = record
		for _, name := range append([]string{canonical}, record.Aliases...) {
			if key := actressalias.Normalize(name); key != "" {
				owners[key] = canonical
			}
		}
	}

	report := importReport{
		Repository:  upstreamRepository,
		Commit:      upstreamCommit,
		Path:        upstreamFile,
		License:     "MIT",
		SHA256:      digest,
		GeneratedAt: now.Format(time.RFC3339),
	}
	canonicalNames := make([]string, 0, len(upstream))
	for canonical := range upstream {
		canonicalNames = append(canonicalNames, canonical)
	}
	sort.Strings(canonicalNames)
	for _, upstreamCanonical := range canonicalNames {
		canonical := actressalias.DirectoryName(upstreamCanonical)
		if canonical == "" {
			continue
		}
		canonicalKey := actressalias.Normalize(canonical)
		if owner := owners[canonicalKey]; owner != "" && owner != canonical {
			report.RejectedAliases = append(report.RejectedAliases, rejectedAlias{Canonical: canonical, Alias: upstreamCanonical, Reason: "normalized canonical name already belongs to " + owner})
			continue
		}
		current, existed := byCanonical[canonical]
		if !existed {
			current = actressalias.Record{Canonical: canonical, Source: "plex-jav@" + upstreamCommit, Confidence: "upstream-mit-alias-table", UpdatedAt: now.Format(time.RFC3339)}
			report.ImportedRecords++
		} else {
			report.MergedRecords++
			current.Source = appendSource(current.Source, "plex-jav@"+upstreamCommit)
			if strings.TrimSpace(current.Confidence) == "" {
				current.Confidence = "mixed-source"
			}
			current.UpdatedAt = now.Format(time.RFC3339)
		}

		for _, alias := range uniqueAliases(append([]string{canonical}, upstream[upstreamCanonical]...)) {
			key := actressalias.Normalize(alias)
			if key == "" {
				continue
			}
			if owner := owners[key]; owner != "" && owner != canonical {
				report.RejectedAliases = append(report.RejectedAliases, rejectedAlias{Canonical: canonical, Alias: alias, Reason: "normalized alias already belongs to " + owner})
				continue
			}
			if !containsAlias(current.Aliases, alias) && actressalias.Normalize(alias) != actressalias.Normalize(canonical) {
				current.Aliases = append(current.Aliases, alias)
				report.ImportedAliases++
			}
			owners[key] = canonical
		}
		current.Aliases = uniqueAliases(current.Aliases)
		byCanonical[canonical] = current
	}

	merged := make([]actressalias.Record, 0, len(byCanonical))
	for _, record := range byCanonical {
		merged = append(merged, record)
	}
	sort.Slice(merged, func(left, right int) bool { return merged[left].Canonical < merged[right].Canonical })
	sort.Slice(report.RejectedAliases, func(left, right int) bool {
		if report.RejectedAliases[left].Canonical == report.RejectedAliases[right].Canonical {
			return report.RejectedAliases[left].Alias < report.RejectedAliases[right].Alias
		}
		return report.RejectedAliases[left].Canonical < report.RejectedAliases[right].Canonical
	})
	return merged, report
}

// removePreviousPlexImport makes a pinned import idempotent. It removes only
// data marked with this importer's own source tag, then lets the current
// upstream snapshot rebuild it. Existing sources and unrelated aliases stay.
func removePreviousPlexImport(records []actressalias.Record, upstream map[string][]string) []actressalias.Record {
	sourceTag := "plex-jav@" + upstreamCommit
	upstreamAliases := make(map[string]map[string]struct{}, len(upstream))
	for canonical, aliases := range upstream {
		key := actressalias.DirectoryName(canonical)
		if key == "" {
			continue
		}
		items := make(map[string]struct{}, len(aliases))
		for _, alias := range aliases {
			if normalized := actressalias.Normalize(alias); normalized != "" {
				items[normalized] = struct{}{}
			}
		}
		upstreamAliases[key] = items
	}
	output := make([]actressalias.Record, 0, len(records))
	for _, record := range records {
		sourceParts := strings.Split(record.Source, ";")
		containsTag := false
		retainedSources := make([]string, 0, len(sourceParts))
		for _, source := range sourceParts {
			if strings.TrimSpace(source) == sourceTag {
				containsTag = true
				continue
			}
			if strings.TrimSpace(source) != "" {
				retainedSources = append(retainedSources, strings.TrimSpace(source))
			}
		}
		if !containsTag {
			output = append(output, record)
			continue
		}
		if len(retainedSources) == 0 {
			continue
		}
		removeAliases := upstreamAliases[record.Canonical]
		retainedAliases := make([]string, 0, len(record.Aliases))
		for _, alias := range record.Aliases {
			if _, imported := removeAliases[actressalias.Normalize(alias)]; !imported {
				retainedAliases = append(retainedAliases, alias)
			}
		}
		record.Aliases = uniqueAliases(retainedAliases)
		record.Source = strings.Join(retainedSources, ";")
		record.UpdatedAt = ""
		output = append(output, record)
	}
	return output
}

func appendSource(current, incoming string) string {
	for _, value := range strings.Split(current, ";") {
		if strings.TrimSpace(value) == incoming {
			return current
		}
	}
	if strings.TrimSpace(current) == "" {
		return incoming
	}
	return current + ";" + incoming
}

func containsAlias(aliases []string, target string) bool {
	targetKey := actressalias.Normalize(target)
	for _, alias := range aliases {
		if actressalias.Normalize(alias) == targetKey {
			return true
		}
	}
	return false
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

func sha256Hex(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func writeJSON(path string, value any) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Clean(path)), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Clean(path), append(payload, '\n'), 0o644)
}

// Command actress-alias-catalog turns a reviewed list of Chinese actor-name
// candidates into a release-ready alias pack. It is a maintainer tool only:
// the desktop runtime reads the generated JSON but never crawls a bulk source.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"javflow/internal/actressalias"
	"javflow/internal/librarymetadata"
)

type candidate struct {
	Alias  string `json:"alias"`
	Source string `json:"source"`
}

type candidateDocument struct {
	Candidates []candidate `json:"candidates"`
}

type rejectedCandidate struct {
	Alias  string `json:"alias"`
	Source string `json:"source,omitempty"`
	Reason string `json:"reason"`
}

type catalogReport struct {
	GeneratedAt string                `json:"generatedAt"`
	Records     []actressalias.Record `json:"records"`
	Rejected    []rejectedCandidate   `json:"rejected"`
}

// aliasResolver allows catalog construction to be tested without a network
// provider. The production adapter below uses the existing metadata engine.
type aliasResolver interface {
	ResolveActorAlias(context.Context, string, string) (librarymetadata.ActorAlias, error)
}

type catalogResult struct {
	candidate candidate
	alias     librarymetadata.ActorAlias
	err       error
}

func main() {
	inputPath := flag.String("input", "", "candidate JSON file (array or {candidates: [...]})")
	outputPath := flag.String("output", "", "validated alias JSON output")
	reportPath := flag.String("report", "", "rejected-candidate report JSON output")
	proxyValue := flag.String("proxy", "", "optional HTTP proxy used by metadata providers")
	workers := flag.Int("workers", 1, "maximum concurrent provider requests")
	requestDelay := flag.Duration("delay", 750*time.Millisecond, "minimum delay between jobs per worker")
	flag.Parse()

	if strings.TrimSpace(*inputPath) == "" || strings.TrimSpace(*outputPath) == "" {
		fmt.Fprintln(os.Stderr, "-input and -output are required")
		flag.Usage()
		os.Exit(2)
	}
	if *workers < 1 {
		*workers = 1
	}
	if *workers > 3 {
		*workers = 3
	}

	candidates, err := readCandidates(*inputPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx := context.Background()
	service := librarymetadata.NewService()
	results := resolveCandidates(ctx, service, candidates, strings.TrimSpace(*proxyValue), *workers, *requestDelay)
	report := buildReport(results, time.Now().UTC())
	if err := writeJSON(*outputPath, report.Records); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if strings.TrimSpace(*reportPath) != "" {
		if err := writeJSON(*reportPath, report); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	fmt.Printf("validated aliases: %d; rejected candidates: %d\n", len(report.Records), len(report.Rejected))
}

func readCandidates(path string) ([]candidate, error) {
	payload, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read candidates: %w", err)
	}
	var raw []candidate
	if err := json.Unmarshal(payload, &raw); err == nil {
		return normalizeCandidates(raw), nil
	}
	var document candidateDocument
	if err := json.Unmarshal(payload, &document); err != nil {
		return nil, fmt.Errorf("parse candidates: %w", err)
	}
	return normalizeCandidates(document.Candidates), nil
}

func normalizeCandidates(items []candidate) []candidate {
	seen := make(map[string]struct{}, len(items))
	result := make([]candidate, 0, len(items))
	for _, item := range items {
		item.Alias = strings.TrimSpace(item.Alias)
		item.Source = strings.TrimSpace(item.Source)
		key := actressalias.Normalize(item.Alias)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Alias < result[right].Alias })
	return result
}

func resolveCandidates(ctx context.Context, resolver aliasResolver, candidates []candidate, proxyValue string, workers int, delay time.Duration) []catalogResult {
	jobs := make(chan candidate)
	results := make(chan catalogResult, len(candidates))
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for item := range jobs {
				requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				resolved, err := resolver.ResolveActorAlias(requestCtx, item.Alias, proxyValue)
				cancel()
				results <- catalogResult{candidate: item, alias: resolved, err: err}
				if delay > 0 {
					time.Sleep(delay)
				}
			}
		}()
	}
	go func() {
		for _, item := range candidates {
			jobs <- item
		}
		close(jobs)
		group.Wait()
		close(results)
	}()

	output := make([]catalogResult, 0, len(candidates))
	for item := range results {
		output = append(output, item)
	}
	sort.Slice(output, func(left, right int) bool { return output[left].candidate.Alias < output[right].candidate.Alias })
	return output
}

func buildReport(results []catalogResult, now time.Time) catalogReport {
	records := make(map[string]actressalias.Record)
	aliasOwners := make(map[string]string)
	rejected := make([]rejectedCandidate, 0)
	for _, result := range results {
		if result.err != nil {
			rejected = append(rejected, rejectedCandidate{Alias: result.candidate.Alias, Source: result.candidate.Source, Reason: result.err.Error()})
			continue
		}
		canonical := strings.TrimSpace(result.alias.Name)
		if !hasJapaneseScript(canonical) {
			rejected = append(rejected, rejectedCandidate{Alias: result.candidate.Alias, Source: result.candidate.Source, Reason: "provider did not return a Japanese canonical name"})
			continue
		}
		aliases := mergeAliases([]string{result.candidate.Alias}, result.alias.Aliases)
		if conflict := firstAliasConflict(aliasOwners, aliases, canonical); conflict != "" {
			rejected = append(rejected, rejectedCandidate{Alias: result.candidate.Alias, Source: result.candidate.Source, Reason: "alias collision with " + conflict})
			continue
		}
		for _, alias := range aliases {
			aliasOwners[actressalias.Normalize(alias)] = canonical
		}
		record := records[canonical]
		record.Canonical = canonical
		record.Aliases = mergeAliases(record.Aliases, aliases)
		record.Source = strings.TrimSpace(result.alias.Provider)
		if record.Source == "" {
			record.Source = "metadata-provider"
		}
		record.Confidence = "provider-verified"
		record.UpdatedAt = now.Format(time.RFC3339)
		records[canonical] = record
	}

	output := make([]actressalias.Record, 0, len(records))
	for _, record := range records {
		output = append(output, record)
	}
	sort.Slice(output, func(left, right int) bool { return output[left].Canonical < output[right].Canonical })
	sort.Slice(rejected, func(left, right int) bool { return rejected[left].Alias < rejected[right].Alias })
	return catalogReport{GeneratedAt: now.Format(time.RFC3339), Records: output, Rejected: rejected}
}

func mergeAliases(groups ...[]string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0)
	for _, group := range groups {
		for _, value := range group {
			value = strings.TrimSpace(value)
			key := actressalias.Normalize(value)
			if key == "" {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func firstAliasConflict(owners map[string]string, aliases []string, canonical string) string {
	for _, alias := range aliases {
		if existing := owners[actressalias.Normalize(alias)]; existing != "" && existing != canonical {
			return existing
		}
	}
	return ""
}

func hasJapaneseScript(value string) bool {
	for _, item := range value {
		if (item >= '\u3040' && item <= '\u30ff') || unicode.In(item, unicode.Han) {
			return true
		}
	}
	return false
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

// Package actressalias resolves alternate actor spellings to one verified
// Japanese canonical name. It contains no provider requests and no UI code so
// it can be tested and updated independently from the Actor Atlas.
//
// Ownership summary:
// 1) normalize alternate performer spellings into a canonical Japanese name
// 2) merge the shipped verified aliases with the user's verified cache
// 3) preserve ambiguity instead of guessing a performer selection
package actressalias

import (
	"embed"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

	"javflow/internal/common"

	"golang.org/x/text/unicode/norm"
)

//go:embed data/bundled_aliases.json
var bundledData embed.FS

// File map for maintainers:
// 1) index.go: alias normalization, collision-safe lookup, and user-cache IO
// 2) data/bundled_aliases.json: small audited aliases shipped with the app
// 3) index_test.go: normalization, collision, cache, and lookup contracts

// Record is a verified mapping from alternate spellings to a single source
// directory name. Source and confidence are retained so an imported pack can
// be audited instead of silently overriding a previous mapping.
type Record struct {
	Canonical  string   `json:"canonical"`
	Aliases    []string `json:"aliases"`
	Source     string   `json:"source"`
	Confidence string   `json:"confidence"`
	UpdatedAt  string   `json:"updatedAt,omitempty"`
}

// Candidate is returned for collisions and low-confidence fuzzy matches. The
// caller must never auto-select a non-unique candidate.
type Candidate struct {
	Canonical  string `json:"canonical"`
	MatchedAs  string `json:"matchedAs"`
	Source     string `json:"source"`
	Confidence string `json:"confidence"`
}

// Resolution is deliberately descriptive: consumers may use Canonical only
// when Unique is true. Candidates make ambiguous aliases visible to the UI.
type Resolution struct {
	Query      string      `json:"query"`
	Canonical  string      `json:"canonical,omitempty"`
	MatchedAs  string      `json:"matchedAs,omitempty"`
	Unique     bool        `json:"unique"`
	MatchKind  string      `json:"matchKind"`
	Candidates []Candidate `json:"candidates,omitempty"`
}

// Index merges the bundled pack and the per-user verified cache. All mutable
// state stays behind the mutex because ranking warmup and a manual search can
// complete at the same time.
type Index struct {
	mu             sync.RWMutex
	writeMu        sync.Mutex
	cachePath      string
	bundledRecords map[string]Record
	userRecords    map[string]Record
	records        map[string]Record
	byAlias        map[string][]Candidate
}

// New loads the small shipped pack, followed by the user cache. An unreadable
// user cache is ignored: actor lookup must remain usable after a damaged cache.
func New(userDataDir string) *Index {
	index := &Index{
		bundledRecords: make(map[string]Record),
		userRecords:    make(map[string]Record),
		records:        make(map[string]Record),
		byAlias:        make(map[string][]Candidate),
	}
	if strings.TrimSpace(userDataDir) != "" {
		index.cachePath = filepath.Join(userDataDir, "actress-aliases.user.json")
	}
	index.mergeBundledRecords(loadBundledRecords())
	index.mergeUserRecords(index.loadUserRecords())
	return index
}

func loadBundledRecords() []Record {
	payload, err := bundledData.ReadFile("data/bundled_aliases.json")
	if err != nil {
		return nil
	}
	var records []Record
	if json.Unmarshal(payload, &records) != nil {
		return nil
	}
	return records
}

func (i *Index) loadUserRecords() []Record {
	if i == nil || strings.TrimSpace(i.cachePath) == "" {
		return nil
	}
	payload, err := os.ReadFile(i.cachePath)
	if err != nil {
		return nil
	}
	var records []Record
	if json.Unmarshal(payload, &records) != nil {
		return nil
	}
	return records
}

// Normalize removes presentation-only differences. The Chinese-to-Japanese
// character table handles common literal script variants, but it intentionally
// does not invent phonetic translations such as an arbitrary Kanji-to-kana
// conversion. Those require a verified alias record.
func Normalize(value string) string {
	value = directoryName(value)
	var builder strings.Builder
	for _, runeValue := range strings.ToLower(value) {
		if unicode.IsSpace(runeValue) || strings.ContainsRune("()[]{}<>（）【】・·,，.。-'_", runeValue) {
			continue
		}
		builder.WriteRune(runeValue)
	}
	return builder.String()
}

// DirectoryName converts only documented visual-script variants. Unlike
// Normalize it retains spacing and punctuation suitable for a source request.
func DirectoryName(value string) string {
	return directoryName(value)
}

func directoryName(value string) string {
	value = norm.NFKC.String(strings.TrimSpace(value))
	return strings.NewReplacer(
		"亚", "亜", "亞", "亜", "愛", "愛", "爱", "愛", "泽", "沢", "澤", "沢",
		"桥", "橋", "樱", "桜", "櫻", "桜", "岛", "島", "户", "戸",
		"濑", "瀬", "瀨", "瀬", "环", "環", "边", "辺", "邊", "辺",
		"叶", "葉", "织", "織", "风", "風", "齐", "斉", "齊", "斉",
		"园", "園", "宫", "宮", "冈", "岡", "华", "華", "优", "優",
		"宁", "寧",
		"结", "結", "乡", "郷", "挂", "掛", "丽", "麗", "绫", "綾",
		"绪", "緒", "绮", "綺", "凉", "涼", "穗", "穂", "铃", "鈴",
		"滨", "浜", "泷", "滝", "龙", "龍", "仓", "倉", "纱", "紗",
		"凤", "鳳", "鹰", "鷹", "里", "里", "黒", "黒", "黑", "黒",
		"咲", "咲", "咲", "咲",
	).Replace(value)
}

func normalizeRecord(record Record) (Record, bool) {
	canonical := strings.TrimSpace(record.Canonical)
	if canonical == "" {
		return Record{}, false
	}
	record.Canonical = canonical
	record.Aliases = uniqueNames(append(record.Aliases, canonical))
	return record, true
}

func (i *Index) mergeBundledRecords(records []Record) {
	if i == nil {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	for _, record := range records {
		normalized, ok := normalizeRecord(record)
		if !ok {
			continue
		}
		i.bundledRecords[normalized.Canonical] = mergeRecord(i.bundledRecords[normalized.Canonical], normalized)
	}
	i.rebuildLocked()
}

// mergeUserRecords keeps the cache logically separate from the shipped pack.
// Earlier builds merged both first and then filtered by Source while saving.
// A provider enrichment could change a bundled record's Source and make the
// entire bundled catalog get written into each user's cache file.
func (i *Index) mergeUserRecords(records []Record) {
	if i == nil {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	for _, record := range records {
		normalized, ok := i.normalizeUserRecordLocked(record)
		if !ok {
			continue
		}
		i.userRecords[normalized.Canonical] = mergeRecord(i.userRecords[normalized.Canonical], normalized)
	}
	i.rebuildLocked()
}

// normalizeUserRecordLocked drops aliases already supplied by the bundled
// catalog. It also repairs cache files written by earlier builds that copied a
// full bundled record into the per-user cache after a provider enrichment.
func (i *Index) normalizeUserRecordLocked(record Record) (Record, bool) {
	canonical := strings.TrimSpace(record.Canonical)
	if canonical == "" {
		return Record{}, false
	}
	record.Canonical = canonical
	record.Aliases = uniqueNames(record.Aliases)
	if bundled, exists := i.bundledRecords[canonical]; exists {
		known := make(map[string]struct{}, len(bundled.Aliases)+1)
		for _, alias := range append(append([]string{}, bundled.Aliases...), bundled.Canonical) {
			if key := Normalize(alias); key != "" {
				known[key] = struct{}{}
			}
		}
		aliases := make([]string, 0, len(record.Aliases))
		for _, alias := range record.Aliases {
			if _, duplicate := known[Normalize(alias)]; !duplicate {
				aliases = append(aliases, alias)
			}
		}
		record.Aliases = aliases
		if len(record.Aliases) == 0 {
			return Record{}, false
		}
		return record, true
	}

	record.Aliases = uniqueNames(append(record.Aliases, canonical))
	return record, true
}

func mergeRecord(current Record, incoming Record) Record {
	if strings.TrimSpace(current.Canonical) == "" {
		return incoming
	}
	current.Aliases = uniqueNames(append(current.Aliases, incoming.Aliases...))
	if strings.TrimSpace(incoming.Source) != "" {
		current.Source = incoming.Source
	}
	if strings.TrimSpace(incoming.Confidence) != "" {
		current.Confidence = incoming.Confidence
	}
	if strings.TrimSpace(incoming.UpdatedAt) != "" {
		current.UpdatedAt = incoming.UpdatedAt
	}
	return current
}

func (i *Index) rebuildLocked() {
	i.records = make(map[string]Record, len(i.bundledRecords)+len(i.userRecords))
	for _, record := range i.bundledRecords {
		normalized, ok := normalizeRecord(record)
		if !ok {
			continue
		}
		i.records[normalized.Canonical] = mergeRecord(i.records[normalized.Canonical], normalized)
	}
	for _, record := range i.userRecords {
		normalized, ok := normalizeRecord(record)
		if !ok {
			continue
		}
		i.records[normalized.Canonical] = mergeRecord(i.records[normalized.Canonical], normalized)
	}
	i.byAlias = make(map[string][]Candidate)
	for _, record := range i.records {
		for _, alias := range record.Aliases {
			key := Normalize(alias)
			if key == "" {
				continue
			}
			candidate := Candidate{Canonical: record.Canonical, MatchedAs: alias, Source: record.Source, Confidence: record.Confidence}
			i.byAlias[key] = appendDistinctCandidate(i.byAlias[key], candidate)
		}
	}
	for key := range i.byAlias {
		sort.Slice(i.byAlias[key], func(left, right int) bool {
			return i.byAlias[key][left].Canonical < i.byAlias[key][right].Canonical
		})
	}
}

func appendDistinctCandidate(items []Candidate, candidate Candidate) []Candidate {
	for _, item := range items {
		if item.Canonical == candidate.Canonical {
			return items
		}
	}
	return append(items, candidate)
}

func uniqueNames(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		value := strings.TrimSpace(item)
		key := Normalize(value)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

// Resolve uses a unique exact normalized alias automatically. Prefix and
// contains matches are returned as candidates only; automatic fuzzy matching
// would be unsafe for short or similarly named performers.
func (i *Index) Resolve(query string) Resolution {
	result := Resolution{Query: strings.TrimSpace(query), MatchKind: "missing"}
	key := Normalize(query)
	if i == nil || key == "" {
		return result
	}
	i.mu.RLock()
	exact := append([]Candidate(nil), i.byAlias[key]...)
	if len(exact) == 1 {
		i.mu.RUnlock()
		result.Canonical = exact[0].Canonical
		result.MatchedAs = exact[0].MatchedAs
		result.Unique = true
		result.MatchKind = "exact"
		return result
	}
	if len(exact) > 1 {
		i.mu.RUnlock()
		result.Candidates = exact
		result.MatchKind = "ambiguous"
		return result
	}
	matched := make([]Candidate, 0, 4)
	for aliasKey, candidates := range i.byAlias {
		if len(key) >= 3 && (strings.HasPrefix(aliasKey, key) || strings.HasPrefix(key, aliasKey)) {
			for _, candidate := range candidates {
				matched = appendDistinctCandidate(matched, candidate)
			}
		}
	}
	i.mu.RUnlock()
	if len(matched) == 1 {
		result.Candidates = matched
		result.MatchKind = "suggestion"
		return result
	}
	if len(matched) > 1 {
		result.Candidates = matched
		result.MatchKind = "ambiguous"
	}
	return result
}

// Remember merges a provider-verified record into the user cache. Ambiguous
// data must be filtered by the caller before it reaches this method.
func (i *Index) Remember(record Record) error {
	if i == nil || strings.TrimSpace(record.Canonical) == "" {
		return errors.New("actress alias record is missing a canonical name")
	}
	// Multiple profile enrichments may finish together. Serialize the complete
	// merge-plus-persist sequence so a slower file write cannot erase a newer
	// verified alias discovered by another request.
	i.writeMu.Lock()
	defer i.writeMu.Unlock()
	i.mergeUserRecords([]Record{record})
	return i.saveUserRecords()
}

func (i *Index) saveUserRecords() error {
	if i == nil || strings.TrimSpace(i.cachePath) == "" {
		return nil
	}
	i.mu.RLock()
	records := make([]Record, 0, len(i.userRecords))
	for _, record := range i.userRecords {
		records = append(records, record)
	}
	i.mu.RUnlock()
	sort.Slice(records, func(left, right int) bool { return records[left].Canonical < records[right].Canonical })
	payload, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	return common.WriteFileAtomic(i.cachePath, append(payload, '\n'), 0o600)
}

// Snapshot returns a stable copy for maintenance commands and tests.
func (i *Index) Snapshot() []Record {
	if i == nil {
		return nil
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	records := make([]Record, 0, len(i.records))
	for _, record := range i.records {
		records = append(records, record)
	}
	sort.Slice(records, func(left, right int) bool { return records[left].Canonical < records[right].Canonical })
	return records
}

// Command actress-alias-ranking adds canonical names that appear in JavFlow's
// bundled AVfan ranking snapshot but are not yet covered by the alias pack.
// It does not contact AVfan or any third-party service at runtime.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"javflow/internal/actressalias"
)

const rankingSnapshotSource = "javflow-avfan-ranking-snapshot:2026-03..2026-07"

var rankingNames = []string{
	"MINAMO",
	"あかね麗",
	"うんぱい",
	"ひなの花音",
	"マリアバレンタイン",
	"みなみ羽琉",
	"愛才りあ",
	"安達夕莉",
	"奥井千晴",
	"巴ひかり",
	"白石なみ",
	"白石るな",
	"白石透羽",
	"百田光稀",
	"柏木こなつ",
	"柏木ふみか",
	"坂井美桜",
	"浜辺やよい",
	"北岡果林",
	"博多彩葉",
	"彩月七緒",
	"倉本すみれ",
	"初美なのか",
	"雛形みくる",
	"純白彩永",
	"村上悠華",
	"東峯日奈子",
	"渡部ほの",
	"二羽紗愛",
	"福田ゆあ",
	"宮城りえ",
	"宮西ひかる",
	"古東まりこ",
	"黒島玲衣",
	"虹村ゆみ",
	"花守夏歩",
	"輝星きら",
	"結城花乃羽",
	"金松季歩",
	"井上もも",
	"九井スナオ",
	"鈴木希",
	"美ノ嶋めぐり",
	"糸井瑠花",
	"蜜このは",
	"明日葉みつは",
	"木村愛心",
	"木村玲衣",
	"浅野こころ",
	"青坂あおい",
	"日向由奈",
	"宍戸里帆",
	"三澄寧々",
	"三好佑香",
	"三佳詩",
	"森下茉莉",
	"榊原萌",
	"矢埜愛茉",
	"守屋よしの",
	"松井日奈子",
	"藤かんな",
	"天神羽衣",
	"天月あず",
	"田村香奈",
	"五芭",
	"西元めいさ",
	"夏目彩春",
	"響蓮",
	"小笠原菜乃",
	"小日向みゆう",
	"小松空",
	"篠崎沙帆",
	"篠原いよ",
	"篠真有",
	"新井リマ",
	"新木希空",
	"星空ねる",
	"星乃莉子",
	"幸村泉希",
	"葉山さゆり",
	"一宮るい",
	"依本しおり",
	"役野満里奈",
	"桜みお",
	"桜ゆの",
	"由良かな",
	"羽月乃蒼",
	"園梨音",
	"月野かすみ",
	"早坂奏音",
	"佐々木さき",
}

type rejectedName struct {
	Canonical string `json:"canonical"`
	Reason    string `json:"reason"`
}

type importReport struct {
	Source          string         `json:"source"`
	SnapshotPeriods []string       `json:"snapshotPeriods"`
	NameListSHA256  string         `json:"nameListSHA256"`
	AddedRecords    int            `json:"addedRecords"`
	ExistingRecords int            `json:"existingRecords"`
	RejectedNames   []rejectedName `json:"rejectedNames,omitempty"`
	GeneratedAt     string         `json:"generatedAt"`
}

func main() {
	basePath := flag.String("base", "", "existing bundled alias JSON")
	outputPath := flag.String("output", "", "merged alias JSON output")
	reportPath := flag.String("report", "", "optional import report JSON output")
	flag.Parse()
	if strings.TrimSpace(*basePath) == "" || strings.TrimSpace(*outputPath) == "" {
		fail(errors.New("-base and -output are required"))
	}
	base, err := readRecords(*basePath)
	if err != nil {
		fail(fmt.Errorf("read base pack: %w", err))
	}
	merged, report := mergeRankingRecords(base, time.Now().UTC())
	if err := writeJSON(*outputPath, merged); err != nil {
		fail(err)
	}
	if strings.TrimSpace(*reportPath) != "" {
		if err := writeJSON(*reportPath, report); err != nil {
			fail(err)
		}
	}
	fmt.Printf("ranking names: %d added, %d already present, %d rejected\n", report.AddedRecords, report.ExistingRecords, len(report.RejectedNames))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
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

func mergeRankingRecords(base []actressalias.Record, now time.Time) ([]actressalias.Record, importReport) {
	byCanonical := make(map[string]actressalias.Record, len(base)+len(rankingNames))
	owners := make(map[string]string)
	for _, record := range base {
		canonical := strings.TrimSpace(record.Canonical)
		if canonical == "" {
			continue
		}
		record.Canonical = canonical
		byCanonical[canonical] = record
		for _, name := range append([]string{canonical}, record.Aliases...) {
			if key := actressalias.Normalize(name); key != "" {
				owners[key] = canonical
			}
		}
	}

	report := importReport{
		Source:          rankingSnapshotSource,
		SnapshotPeriods: []string{"2026-03", "2026-04", "2026-05", "2026-06", "2026-07"},
		NameListSHA256:  namesSHA256(rankingNames),
		GeneratedAt:     now.Format(time.RFC3339),
	}
	for _, name := range rankingNames {
		canonical := actressalias.DirectoryName(name)
		if canonical == "" {
			report.RejectedNames = append(report.RejectedNames, rejectedName{Canonical: name, Reason: "empty canonical name after normalization"})
			continue
		}
		key := actressalias.Normalize(canonical)
		if owner := owners[key]; owner != "" && owner != canonical {
			report.RejectedNames = append(report.RejectedNames, rejectedName{Canonical: canonical, Reason: "normalized name already belongs to " + owner})
			continue
		}
		if _, exists := byCanonical[canonical]; exists {
			report.ExistingRecords++
			continue
		}
		byCanonical[canonical] = actressalias.Record{
			Canonical:  canonical,
			Source:     rankingSnapshotSource,
			Confidence: "bundled-ranking-canonical-name",
			UpdatedAt:  now.Format(time.RFC3339),
		}
		owners[key] = canonical
		report.AddedRecords++
	}

	merged := make([]actressalias.Record, 0, len(byCanonical))
	for _, record := range byCanonical {
		merged = append(merged, record)
	}
	sort.Slice(merged, func(left, right int) bool { return merged[left].Canonical < merged[right].Canonical })
	sort.Slice(report.RejectedNames, func(left, right int) bool {
		return report.RejectedNames[left].Canonical < report.RejectedNames[right].Canonical
	})
	return merged, report
}

func namesSHA256(names []string) string {
	payload := []byte(strings.Join(names, "\n") + "\n")
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

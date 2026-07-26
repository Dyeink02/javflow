package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"javflow/internal/avsubscriptionv2"
	"javflow/internal/contracts/crawlartifact"
	runtimepaths "javflow/internal/runtime"
)

func main() {
	defaultUserData := ""
	if configDir, err := os.UserConfigDir(); err == nil {
		defaultUserData = filepath.Join(configDir, "javflow")
	}
	userDataDir := flag.String("user-data", defaultUserData, "应用用户数据目录")
	apply := flag.Bool("apply", false, "执行迁移和订阅导入")
	flag.Parse()

	if strings.TrimSpace(*userDataDir) == "" {
		fatalf("无法确定应用用户数据目录")
	}
	if !*apply {
		fatalf("这是写入操作，请确认后增加 -apply")
	}

	items, err := crawlartifact.DiscoverCacheSnapshots(*userDataDir, nil)
	if err != nil {
		fatalf("迁移隐藏抓取产物失败：%v", err)
	}
	if len(items) == 0 {
		fatalf("没有找到历史抓取快照")
	}
	prunedDirectories, err := pruneUnreferencedArtifactDirectories(*userDataDir, items)
	if err != nil {
		fatalf("清理未引用的隐藏产物目录失败：%v", err)
	}

	storagePath := filepath.Join(*userDataDir, "subscriptions-v2", "av-subscriptions-v2.json")
	if err := backupFile(storagePath); err != nil {
		fatalf("备份现有订阅失败：%v", err)
	}

	// Import smaller/older outputs first. Repeated imports merge code sets, so
	// later comprehensive snapshots also become the preferred output directory.
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].CompletedCount != items[j].CompletedCount {
			return items[i].CompletedCount < items[j].CompletedCount
		}
		return items[i].UpdatedAt < items[j].UpdatedAt
	})

	service := avsubscriptionv2.NewService(runtimepaths.Paths{UserData: *userDataDir}, nil)
	importedOutputs := 0
	failedOutputs := 0
	for _, item := range items {
		result, importErr := service.ImportFromOutput(item.OutputDir)
		if importErr != nil {
			failedOutputs++
			fmt.Printf("跳过：%s：%v\n", item.OutputDir, importErr)
			continue
		}
		importedOutputs++
		fmt.Printf("已导入：%s，累计基线 %d 部\n", result.Subscription.ActressName, len(result.Subscription.BaselineCodes))
	}

	subscriptions, err := service.List()
	if err != nil {
		fatalf("读取导入后的订阅失败：%v", err)
	}
	// Persist normalization such as legacy file:// avatar URL conversion.
	if err := service.ReplaceAll(subscriptions); err != nil {
		fatalf("保存导入后的订阅失败：%v", err)
	}

	missingURLs := make([]string, 0)
	for _, item := range subscriptions {
		if strings.TrimSpace(item.CrawlURL) == "" {
			missingURLs = append(missingURLs, item.ActressName)
		}
	}
	sort.Strings(missingURLs)

	fmt.Printf("迁移完成：隐藏快照 %d 份，清理未引用目录 %d 个，成功导入输出 %d 份，失败 %d 份，订阅演员 %d 位。\n", len(items), prunedDirectories, importedOutputs, failedOutputs, len(subscriptions))
	if len(missingURLs) > 0 {
		fmt.Printf("以下演员的历史文件没有可恢复的抓取地址：%s\n", strings.Join(missingURLs, "、"))
	}
}

func pruneUnreferencedArtifactDirectories(userDataDir string, items []crawlartifact.CacheSnapshot) (int, error) {
	artifactRoot, err := filepath.Abs(filepath.Join(userDataDir, "crawl-artifacts"))
	if err != nil {
		return 0, err
	}
	referenced := map[string]struct{}{}
	for _, item := range items {
		for _, artifactPath := range []string{item.FilmDataPath, item.CrawlProfilePath, item.OrganizerCodesPath} {
			absolutePath, pathErr := filepath.Abs(strings.TrimSpace(artifactPath))
			if pathErr != nil || strings.TrimSpace(artifactPath) == "" {
				continue
			}
			relative, relativeErr := filepath.Rel(artifactRoot, absolutePath)
			if relativeErr != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				continue
			}
			segments := strings.Split(relative, string(filepath.Separator))
			if len(segments) > 0 && strings.TrimSpace(segments[0]) != "" {
				referenced[strings.ToLower(segments[0])] = struct{}{}
			}
		}
	}

	entries, err := os.ReadDir(artifactRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, exists := referenced[strings.ToLower(entry.Name())]; exists {
			continue
		}
		targetPath, err := filepath.Abs(filepath.Join(artifactRoot, entry.Name()))
		if err != nil {
			return removed, err
		}
		relative, err := filepath.Rel(artifactRoot, targetPath)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return removed, fmt.Errorf("拒绝删除隐藏根目录之外的路径：%s", targetPath)
		}
		if err := os.RemoveAll(targetPath); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

func backupFile(sourcePath string) error {
	payload, err := os.ReadFile(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	backupPath := sourcePath + ".backup-" + time.Now().Format("20060102-150405")
	return os.WriteFile(backupPath, payload, 0o644)
}

func fatalf(format string, values ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}

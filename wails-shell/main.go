package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"javflow/internal/appupdate"
)

//go:embed all:frontend
var assets embed.FS

func isRepoRoot(candidate string) bool {
	if strings.TrimSpace(candidate) == "" {
		return false
	}

	packageFile := filepath.Join(candidate, "package.json")

	if info, err := os.Stat(packageFile); err != nil || info.IsDir() {
		return false
	}

	return true
}

func resolveRepoRoot() (string, error) {
	candidates := make([]string, 0, 4)

	if envRoot := strings.TrimSpace(os.Getenv("JAVFLOW_REPO_ROOT")); envRoot != "" {
		candidates = append(candidates, envRoot)
	}

	if executablePath, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(executablePath))
	}

	if workingDir, err := os.Getwd(); err == nil {
		candidates = append(candidates, workingDir)
	}

	candidates = append(candidates, ".")

	visited := map[string]struct{}{}
	for _, candidate := range candidates {
		absoluteCandidate, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}

		current := absoluteCandidate
		for depth := 0; depth < 5; depth += 1 {
			if _, seen := visited[current]; !seen {
				visited[current] = struct{}{}
				if isRepoRoot(current) {
					return current, nil
				}
			}

			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}

	return "", fmt.Errorf("无法定位 JavFlow 项目根目录，请确保 exe 仍位于源码目录内，或手动设置 JAVFLOW_REPO_ROOT")
}

func main() {
	if handled, helperErr := appupdate.RunUpdateHelper(os.Args[1:]); handled {
		if helperErr != nil {
			log.Printf("JavFlow 更新辅助进程失败：%v", helperErr)
			os.Exit(1)
		}
		return
	}

	repoRoot, err := resolveRepoRoot()
	if err != nil {
		log.Fatal(err)
	}

	app := NewApp(repoRoot)

	err = wails.Run(&options.App{
		Title:     "JavFlow",
		Width:     1440,
		Height:    920,
		MinWidth:  1120,
		MinHeight: 720,
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: app.subscriptionMediaHandler(),
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}

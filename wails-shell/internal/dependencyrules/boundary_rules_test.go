package dependencyrules

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

type packageBoundaryRule struct {
	name             string
	packageDir       string
	forbiddenImports []string
}

const internalModuleImportPrefix = "javflow"

func rule(name string, packageDir string, forbiddenImports ...string) packageBoundaryRule {
	return packageBoundaryRule{
		name:             name,
		packageDir:       packageDir,
		forbiddenImports: append([]string(nil), forbiddenImports...),
	}
}

func appendRulesForPackageGroup(
	rules []packageBoundaryRule,
	packageDirs []string,
	nameSuffix string,
	forbiddenImports ...string,
) []packageBoundaryRule {
	for _, packageDir := range packageDirs {
		displayName := strings.TrimPrefix(filepath.ToSlash(packageDir), "internal/")
		rules = append(rules, rule(displayName+" "+nameSuffix, packageDir, forbiddenImports...))
	}
	return rules
}

func internalImport(packageDir string) string {
	return internalModuleImportPrefix + "/" + filepath.ToSlash(packageDir)
}

func internalImports(packageDirs ...string) []string {
	imports := make([]string, 0, len(packageDirs))
	for _, packageDir := range packageDirs {
		imports = append(imports, internalImport(packageDir))
	}
	return imports
}

func organizerOrSubscriptionDomainPackages() []string {
	return []string{
		"internal/organizer",
		"internal/avsubscription",
		"internal/avsubscriptionv2",
		"internal/subcrawl",
		"internal/subcrawlv2",
	}
}

func readModelPackages() []string {
	return []string{
		"internal/crawlresult",
		"internal/crawlreview",
		"internal/crawlruncontext",
		"internal/crawluistate",
		"internal/crawlstage",
	}
}

func shellPackages() []string {
	return []string{
		"internal/settings",
		"internal/proxy",
		"internal/desktop",
		"internal/sidecar",
	}
}

func sharedNeutralPackages() []string {
	return []string{
		"internal/common",
		"internal/messages",
		"internal/interfaces",
		"internal/runtime",
		"internal/events",
		"internal/modulelog",
		"internal/contracts/crawlartifact",
		"internal/contracts/subscriptiontarget",
	}
}

func crawlFoundationPackages() []string {
	return []string{
		"internal/crawlconfig",
		"internal/crawlrequest",
		"internal/crawlparse",
		"internal/crawlidentity",
		"internal/crawlindex",
		"internal/crawlfetch",
	}
}

func infrastructureNeutralPackages() []string {
	return []string{
		"internal/actressalias",
		"internal/actressranking",
		"internal/adlearning",
		"internal/antiblock",
		"internal/crawloutput",
		"internal/crawlqueue",
		"internal/dependency",
		"internal/librarymetadata",
		"internal/observer",
		"internal/runtimecache",
	}
}

func executionNeutralPackages() []string {
	return []string{
		"internal/crawlexecution",
		"internal/crawltaskstate",
	}
}

func runtimePackages() []string {
	return []string{
		"internal/crawlrunner",
		"internal/crawltask",
		"internal/crawlquality",
	}
}

func activeInternalBoundaryCoverageGroups() [][]string {
	return [][]string{
		{"internal/actresslookup"},
		organizerOrSubscriptionDomainPackages(),
		readModelPackages(),
		shellPackages(),
		sharedNeutralPackages(),
		crawlFoundationPackages(),
		infrastructureNeutralPackages(),
		executionNeutralPackages(),
		runtimePackages(),
		{"internal/bridge"},
	}
}

func coveredActiveInternalPackages() map[string]struct{} {
	covered := map[string]struct{}{}
	for _, group := range activeInternalBoundaryCoverageGroups() {
		for _, packageDir := range group {
			covered[packageDir] = struct{}{}
		}
	}
	return covered
}

func explicitlyExcludedActiveInternalPackages() map[string]struct{} {
	return map[string]struct{}{
		"internal/dependencyrules": {},
	}
}

func TestModuleBoundaryRules(t *testing.T) {
	rootDir := projectRootDir(t)

	// These rules are not about aesthetics. They protect the current
	// decoupling direction:
	// 1) organizer should stay focused on local file processing plus crawl
	//    output artifacts, not runtime crawl orchestration or UI state helpers
	// 2) avsubscription should stay on persisted artifact / subscription-target
	//    contracts, not on JAV crawler business internals
	// 3) crawler runtime packages should not grow organizer/subscription-specific
	//    business dependencies back in through convenience imports
	//
	// If one of these tests starts failing after a refactor, treat it as an
	// architectural review point rather than a "just make the test green" task.
	organizerOrSubscriptionDomains := internalImports(organizerOrSubscriptionDomainPackages()...)

	rules := []packageBoundaryRule{
		rule(
			"actresslookup does not import avsubscription",
			"internal/actresslookup",
			internalImport("internal/avsubscription"),
		),
		rule(
			"organizer does not import crawl runner or crawl task internals",
			"internal/organizer",
			internalImports("internal/crawlrunner", "internal/crawltask")...,
		),
		rule(
			"organizer does not import crawl execution internals",
			"internal/organizer",
			internalImport("internal/crawlexecution"),
		),
		rule(
			"organizer does not import avsubscription or crawl ui review layers",
			"internal/organizer",
			internalImports(
				"internal/avsubscription",
				"internal/bridge",
				"internal/crawlquality",
				"internal/crawlresult",
				"internal/crawlreview",
				"internal/crawlruncontext",
				"internal/crawluistate",
			)...,
		),
		rule(
			"organizer does not import actress lookup or ranking services directly",
			"internal/organizer",
			internalImports("internal/actresslookup", "internal/actressranking")...,
		),
		rule(
			"organizer does not import sidecar or desktop runtime shells",
			"internal/organizer",
			internalImports("internal/sidecar", "internal/desktop", "internal/runtimecache")...,
		),
		rule(
			"avsubscription does not import organizer",
			"internal/avsubscription",
			internalImport("internal/organizer"),
		),
		rule(
			"avsubscriptionv2 does not import organizer",
			"internal/avsubscriptionv2",
			internalImport("internal/organizer"),
		),
		rule(
			"avsubscription does not import crawl runner or crawl task internals",
			"internal/avsubscription",
			internalImports("internal/crawlrunner", "internal/crawltask", "internal/crawlexecution")...,
		),
		rule(
			"avsubscriptionv2 does not import crawl runner or crawl task internals",
			"internal/avsubscriptionv2",
			internalImports("internal/crawlrunner", "internal/crawltask", "internal/crawlexecution")...,
		),
		rule(
			"avsubscription does not import crawl ui review or bridge layers",
			"internal/avsubscription",
			internalImports(
				"internal/bridge",
				"internal/crawlquality",
				"internal/crawlresult",
				"internal/crawlreview",
				"internal/crawlruncontext",
				"internal/crawluistate",
			)...,
		),
		rule(
			"avsubscriptionv2 does not import crawl ui review or bridge layers",
			"internal/avsubscriptionv2",
			internalImports(
				"internal/bridge",
				"internal/crawlquality",
				"internal/crawlresult",
				"internal/crawlreview",
				"internal/crawlruncontext",
				"internal/crawluistate",
			)...,
		),
		rule(
			"avsubscription does not import actress lookup or ranking services directly",
			"internal/avsubscription",
			internalImports("internal/actresslookup", "internal/actressranking")...,
		),
		rule(
			"avsubscriptionv2 does not import actress lookup or ranking services directly",
			"internal/avsubscriptionv2",
			internalImports("internal/actresslookup", "internal/actressranking")...,
		),
		rule(
			"avsubscription does not import organizer-adlearning or dependency shells",
			"internal/avsubscription",
			internalImports("internal/adlearning", "internal/dependency", "internal/proxy", "internal/sidecar")...,
		),
		rule(
			"avsubscriptionv2 does not import organizer-adlearning or dependency shells",
			"internal/avsubscriptionv2",
			internalImports("internal/adlearning", "internal/dependency", "internal/proxy", "internal/sidecar")...,
		),
		rule(
			"avsubscription does not import sidecar or desktop runtime shells",
			"internal/avsubscription",
			internalImports("internal/sidecar", "internal/desktop", "internal/runtimecache")...,
		),
		rule(
			"avsubscriptionv2 does not import sidecar or desktop runtime shells",
			"internal/avsubscriptionv2",
			internalImports("internal/sidecar", "internal/desktop", "internal/runtimecache")...,
		),
		rule(
			"bridge keeps shared interface and message boundaries explicit",
			"internal/bridge",
			internalImports("internal/interfaces", "internal/messages")...,
		),
		rule(
			"actresslookup stays independent from organizer domain",
			"internal/actresslookup",
			internalImport("internal/organizer"),
		),
	}

	rules = appendRulesForPackageGroup(
		rules,
		runtimePackages(),
		"does not import organizer or avsubscription domains",
		organizerOrSubscriptionDomains...,
	)
	rules = appendRulesForPackageGroup(
		rules,
		readModelPackages(),
		"does not import organizer or avsubscription domains",
		organizerOrSubscriptionDomains...,
	)
	rules = appendRulesForPackageGroup(
		rules,
		shellPackages(),
		"does not import organizer or avsubscription domains",
		organizerOrSubscriptionDomains...,
	)
	rules = appendRulesForPackageGroup(
		rules,
		sharedNeutralPackages(),
		"stays independent from organizer and avsubscription domains",
		organizerOrSubscriptionDomains...,
	)
	rules = appendRulesForPackageGroup(
		rules,
		crawlFoundationPackages(),
		"does not import organizer or avsubscription domains",
		organizerOrSubscriptionDomains...,
	)
	rules = appendRulesForPackageGroup(
		rules,
		infrastructureNeutralPackages(),
		"stays independent from organizer and avsubscription domains",
		organizerOrSubscriptionDomains...,
	)
	rules = appendRulesForPackageGroup(
		rules,
		executionNeutralPackages(),
		"stays independent from organizer and avsubscription domains",
		organizerOrSubscriptionDomains...,
	)

	for _, rule := range rules {
		rule := rule
		t.Run(rule.name, func(t *testing.T) {
			packagePath := filepath.Join(rootDir, filepath.FromSlash(rule.packageDir))
			importsByFile := readPackageImports(t, packagePath)
			for filePath, imports := range importsByFile {
				for _, importPath := range imports {
					for _, forbiddenImport := range rule.forbiddenImports {
						if !isForbiddenImport(importPath, forbiddenImport) {
							continue
						}
						relativeFile, err := filepath.Rel(rootDir, filePath)
						if err != nil {
							relativeFile = filePath
						}
						t.Fatalf("%s imports forbidden package %q in %s", rule.packageDir, importPath, filepath.ToSlash(relativeFile))
					}
				}
			}
		})
	}
}

func TestBoundaryRuleCoverageForActivePackages(t *testing.T) {
	rootDir := projectRootDir(t)
	covered := coveredActiveInternalPackages()
	explicitlyExcluded := explicitlyExcludedActiveInternalPackages()

	internalRoot := filepath.Join(rootDir, "internal")
	entries, err := os.ReadDir(internalRoot)
	if err != nil {
		t.Fatalf("read internal root: %v", err)
	}

	missing := []string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		packageDir := filepath.ToSlash(filepath.Join("internal", entry.Name()))
		if _, skip := explicitlyExcluded[packageDir]; skip {
			continue
		}
		files, err := os.ReadDir(filepath.Join(internalRoot, entry.Name()))
		if err != nil {
			t.Fatalf("read package directory %s: %v", packageDir, err)
		}
		hasActiveGo := false
		for _, file := range files {
			if file.IsDir() {
				continue
			}
			name := file.Name()
			if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
				hasActiveGo = true
				break
			}
		}
		if !hasActiveGo {
			continue
		}
		if _, ok := covered[packageDir]; !ok {
			missing = append(missing, packageDir)
		}
	}

	if len(missing) > 0 {
		t.Fatalf("active internal packages missing dependency boundary coverage: %s", strings.Join(missing, ", "))
	}
}

func projectRootDir(t *testing.T) string {
	t.Helper()

	_, filePath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve dependency rule test file path")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(filePath), "..", ".."))
}

func readPackageImports(t *testing.T, packagePath string) map[string][]string {
	t.Helper()

	entries, err := os.ReadDir(packagePath)
	if err != nil {
		t.Fatalf("read package directory %s: %v", packagePath, err)
	}

	fileSet := token.NewFileSet()
	importsByFile := map[string][]string{}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		fileName := entry.Name()
		if !strings.HasSuffix(fileName, ".go") || strings.HasSuffix(fileName, "_test.go") {
			continue
		}

		filePath := filepath.Join(packagePath, fileName)
		parsedFile, err := parser.ParseFile(fileSet, filePath, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse imports for %s: %v", filePath, err)
		}

		imports := make([]string, 0, len(parsedFile.Imports))
		for _, importSpec := range parsedFile.Imports {
			importPath, err := strconv.Unquote(importSpec.Path.Value)
			if err != nil {
				t.Fatalf("unquote import path in %s: %v", filePath, err)
			}
			imports = append(imports, importPath)
		}

		importsByFile[filePath] = imports
	}

	return importsByFile
}

func isForbiddenImport(importPath string, forbiddenImport string) bool {
	return importPath == forbiddenImport || strings.HasPrefix(importPath, forbiddenImport+"/")
}

package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeDialogSelectionMapsNativeQuestionResults(t *testing.T) {
	buttons := []string{"立即重启", "稍后"}
	tests := []struct {
		name   string
		result string
		want   string
	}{
		{name: "windows yes", result: "Yes", want: "立即重启"},
		{name: "windows no", result: "No", want: "稍后"},
		{name: "localized yes", result: "是(Y)", want: "立即重启"},
		{name: "localized no", result: "否(N)", want: "稍后"},
		{name: "custom result", result: "立即重启", want: "立即重启"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeDialogSelection(test.result, buttons); got != test.want {
				t.Fatalf("normalizeDialogSelection(%q) = %q, want %q", test.result, got, test.want)
			}
		})
	}
}

func TestNormalizeDialogSelectionLeavesSingleButtonUntouched(t *testing.T) {
	if got := normalizeDialogSelection("Yes", []string{"我知道了"}); got != "Yes" {
		t.Fatalf("single-button result = %q, want Yes", got)
	}
}

func TestOpenPathUsesFileAssociationForRegularFiles(t *testing.T) {
	pathValue := filepath.Join(t.TempDir(), "magnet-links.txt")
	if err := os.WriteFile(pathValue, []byte("magnet:?xt=urn:btih:test"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(pathValue)
	if err != nil {
		t.Fatal(err)
	}
	if info.IsDir() {
		t.Fatal("test fixture unexpectedly became a directory")
	}
	command := openPathCommand(pathValue, info.IsDir())
	if len(command.Args) != 5 || command.Args[0] != "cmd.exe" || command.Args[1] != "/c" || command.Args[2] != "start" || command.Args[3] != "" || command.Args[4] != pathValue {
		t.Fatalf("regular files must use Windows file association command, got %#v", command.Args)
	}
	directoryCommand := openPathCommand(filepath.Dir(pathValue), true)
	if len(directoryCommand.Args) != 2 || directoryCommand.Args[0] != "explorer.exe" || directoryCommand.Args[1] != filepath.Dir(pathValue) {
		t.Fatalf("directories must use Explorer, got %#v", directoryCommand.Args)
	}
}

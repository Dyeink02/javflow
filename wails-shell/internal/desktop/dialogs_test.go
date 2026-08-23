package desktop

import "testing"

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

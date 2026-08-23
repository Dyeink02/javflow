package appupdate

import "testing"

func TestProjectVersionOrdering(t *testing.T) {
	tests := []struct {
		left     string
		right    string
		expected int
	}{
		{left: "0.4.32", right: "0.4.40", expected: -1},
		{left: "0.4.4", right: "0.4.40", expected: 0},
		{left: "0.4.41", right: "0.4.40", expected: 1},
		{left: "v0.4.40", right: "0.4.40", expected: 0},
	}

	for _, testCase := range tests {
		actual, err := CompareVersions(testCase.left, testCase.right)
		if err != nil {
			t.Fatalf("CompareVersions(%q, %q) failed: %v", testCase.left, testCase.right, err)
		}
		if actual != testCase.expected {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", testCase.left, testCase.right, actual, testCase.expected)
		}
	}
}

func TestParseVersionRejectsAmbiguousValues(t *testing.T) {
	for _, value := range []string{"", "0.4.4.1", "0.4.x", "v0.4."} {
		if _, err := ParseVersion(value); err == nil {
			t.Errorf("ParseVersion(%q) unexpectedly succeeded", value)
		}
	}
}

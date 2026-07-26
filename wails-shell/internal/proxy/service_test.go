package proxy

import "testing"

func TestNormalizeProxyValue(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "empty", input: "", expected: ""},
		{name: "host port", input: "127.0.0.1:7890", expected: "http://127.0.0.1:7890"},
		{name: "https proxy", input: "https://proxy.example.com:443", expected: "https://proxy.example.com:443"},
		{name: "invalid", input: "not a proxy", expected: ""},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if actual := NormalizeProxyValue(testCase.input); actual != testCase.expected {
				t.Fatalf("NormalizeProxyValue(%q) = %q, expected %q", testCase.input, actual, testCase.expected)
			}
		})
	}
}

func TestNormalizeTargetURL(t *testing.T) {
	t.Parallel()

	if actual := NormalizeTargetURL(""); actual != fallbackTargetURL {
		t.Fatalf("NormalizeTargetURL(empty) = %q, expected %q", actual, fallbackTargetURL)
	}

	if actual := NormalizeTargetURL("http://example.com"); actual != fallbackTargetURL {
		t.Fatalf("NormalizeTargetURL(http) = %q, expected fallback", actual)
	}

	if actual := NormalizeTargetURL("https://www.javbus.com/star/wc8"); actual != "https://www.javbus.com/star/wc8" {
		t.Fatalf("NormalizeTargetURL(valid) = %q", actual)
	}
}

func TestValidateProxyEmptyAndInvalid(t *testing.T) {
	t.Parallel()

	service := NewService()

	emptyResult := service.ValidateProxy("", "")
	if emptyResult.Status != "empty" {
		t.Fatalf("empty proxy status = %q", emptyResult.Status)
	}

	invalidResult := service.ValidateProxy("bad proxy", "")
	if invalidResult.Status != "invalid" {
		t.Fatalf("invalid proxy status = %q", invalidResult.Status)
	}
}

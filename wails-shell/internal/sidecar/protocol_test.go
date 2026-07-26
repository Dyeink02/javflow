package sidecar

import "testing"

func TestResultErrorKeepsSidecarMessageLiteral(t *testing.T) {
	err := ResultError(ResultPacket{
		OK: false,
		Error: &ErrorPacket{
			Message: "sidecar failed: 100% raw %s",
		},
	})

	if err == nil {
		t.Fatal("expected ResultError to return an error")
	}

	if err.Error() != "sidecar failed: 100% raw %s" {
		t.Fatalf("unexpected error message: %q", err.Error())
	}
}

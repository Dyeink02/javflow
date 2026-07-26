package adlearning

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// stream.go owns lightweight stream-frame hashing used by ad-learning sample
// evaluation.
//
// Ownership summary:
// 1) compute lightweight perceptual hashes for stream frames
// 2) support ad-learning stream evaluation without broader pipeline concerns
// 3) keep stream-frame hashing separate from batch evaluation orchestration
//
// File map for maintainers:
// 1) stream-frame hash entrypoint
// 2) ffmpeg command and timeout setup
// 3) raw frame output to perceptual-bit conversion handoff

func computeStreamFrameHash(command string, streamURL string, second int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	args := []string{
		"-v", "error",
		"-ss", fmt.Sprintf("%d", second),
		"-i", streamURL,
		"-frames:v", "1",
		"-vf", "scale=8:8,format=gray",
		"-f", "rawvideo",
		"-",
	}

	cmd := exec.CommandContext(ctx, command, args...)
	output, err := cmd.CombinedOutput()
	if err != nil && len(output) == 0 {
		return "", err
	}
	return toPerceptualBits(output), nil
}

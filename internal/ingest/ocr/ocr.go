// Package ocr turns a photographed recipe page into text by running the
// tesseract binary as an external process (ADR-00006, ADR-00002: the Go build
// needs no cgo). The image goes to tesseract on stdin and is never written to
// disk; the text it prints is the acquire step's output.
package ocr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	// DefaultBin is the binary looked up on PATH.
	DefaultBin = "tesseract"
	// DefaultTimeout bounds one OCR run.
	DefaultTimeout = 60 * time.Second
)

var (
	// ErrUnavailable means the tesseract binary could not be started, for
	// example because it is not installed.
	ErrUnavailable = errors.New("tesseract is not available")
	// ErrFailed means tesseract ran and failed, for example on an image it
	// cannot decode, or it ran past the timeout.
	ErrFailed = errors.New("tesseract failed")
	// ErrNoText means tesseract ran and found no text in the image. It is an
	// answer about the photo, not a pipeline failure.
	ErrNoText = errors.New("no text found in the image")
)

// Runner runs tesseract. The zero value uses the defaults.
type Runner struct {
	// Bin is the binary to run; empty means DefaultBin, found on PATH.
	Bin string
	// Timeout bounds one run; zero means DefaultTimeout.
	Timeout time.Duration
}

// Text returns the text tesseract reads from image, which it receives on
// stdin. A missing binary wraps ErrUnavailable, any other failure wraps
// ErrFailed, and an image without text wraps ErrNoText; none of them is
// returned as empty text.
func (r Runner) Text(ctx context.Context, image []byte) (string, error) {
	bin := r.Bin
	if bin == "" {
		bin = DefaultBin
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, "stdin", "stdout")
	cmd.Stdin = bytes.NewReader(image)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		switch {
		case errors.Is(err, exec.ErrNotFound), errors.Is(err, os.ErrNotExist), errors.Is(err, os.ErrPermission):
			return "", fmt.Errorf("%w: run %q: %w", ErrUnavailable, bin, err)
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return "", fmt.Errorf("%w: %q ran past %s: %w", ErrFailed, bin, timeout, ctx.Err())
		case ctx.Err() != nil:
			return "", fmt.Errorf("%w: %q stopped: %w", ErrFailed, bin, ctx.Err())
		}
		return "", fmt.Errorf("%w: %s: %w", ErrFailed, strings.TrimSpace(stderr.String()), err)
	}
	text := stdout.String()
	if strings.TrimSpace(text) == "" {
		return "", ErrNoText
	}
	return text, nil
}

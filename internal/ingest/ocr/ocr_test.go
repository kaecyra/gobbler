package ocr

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stub writes an executable shell script standing in for tesseract.
func stub(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "tesseract")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTextReturnsStdoutAndFeedsImageOnStdin(t *testing.T) {
	// The stub echoes its arguments and the bytes it was given.
	bin := stub(t, `echo "args: $*"; cat`)
	got, err := Runner{Bin: bin}.Text(context.Background(), []byte("IMAGEBYTES"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "args: stdin stdout") || !strings.Contains(got, "IMAGEBYTES") {
		t.Errorf("Text = %q, want the stdin/stdout arguments and the image on stdin", got)
	}
}

func TestTextMissingBinaryIsUnavailable(t *testing.T) {
	_, err := Runner{Bin: filepath.Join(t.TempDir(), "absent")}.Text(context.Background(), []byte("x"))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestTextFailureIsNotEmptyText(t *testing.T) {
	bin := stub(t, `echo "Error: cannot open image" >&2; exit 1`)
	text, err := Runner{Bin: bin}.Text(context.Background(), []byte("x"))
	if !errors.Is(err, ErrFailed) || text != "" {
		t.Fatalf("Text = %q, %v; want ErrFailed and no text", text, err)
	}
	if !strings.Contains(err.Error(), "cannot open image") {
		t.Errorf("err = %q, want tesseract's own message", err)
	}
}

func TestTextBlankOutputIsNoText(t *testing.T) {
	bin := stub(t, `printf '  \n\f'`)
	if _, err := (Runner{Bin: bin}).Text(context.Background(), []byte("x")); !errors.Is(err, ErrNoText) {
		t.Fatalf("err = %v, want ErrNoText", err)
	}
}

func TestTextTimeoutIsFailure(t *testing.T) {
	bin := stub(t, `exec sleep 5`)
	start := time.Now()
	_, err := Runner{Bin: bin, Timeout: 100 * time.Millisecond}.Text(context.Background(), []byte("x"))
	if !errors.Is(err, ErrFailed) {
		t.Fatalf("err = %v, want ErrFailed", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("took %s, the timeout did not stop the process", time.Since(start))
	}
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "ran past") {
		t.Errorf("err = %q, want it to say the run ran past the timeout", err)
	}
}

func TestTextCallerCancellationIsNotReportedAsATimeout(t *testing.T) {
	bin := stub(t, `exec sleep 5`)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	_, err := Runner{Bin: bin, Timeout: time.Minute}.Text(ctx, []byte("x"))
	if !errors.Is(err, ErrFailed) || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want ErrFailed wrapping context.Canceled", err)
	}
	if strings.Contains(err.Error(), "ran past") {
		t.Errorf("err = %q, a cancellation is not a timeout", err)
	}
}

func TestTextFindsTesseractOnPath(t *testing.T) {
	dir := filepath.Dir(stub(t, `echo from-path`))
	t.Setenv("PATH", dir)
	got, err := Runner{}.Text(context.Background(), []byte("x"))
	if err != nil || strings.TrimSpace(got) != "from-path" {
		t.Fatalf("Text = %q, %v; want the PATH binary's output", got, err)
	}
}

// A real tesseract run needs a binary and a language pack; without them the
// test is skipped, with the reason, rather than passing without checking.
func TestRealTesseractRejectsNonImage(t *testing.T) {
	if _, err := exec.LookPath(DefaultBin); err != nil {
		t.Skipf("tesseract is not installed (%v); install tesseract-ocr to run the OCR integration test", err)
	}
	_, err := Runner{}.Text(context.Background(), []byte("this is not an image"))
	if !errors.Is(err, ErrFailed) {
		t.Fatalf("err = %v, want ErrFailed for bytes tesseract cannot decode", err)
	}
}

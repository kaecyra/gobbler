package ingest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

// ErrFetch is returned (wrapped) when a page cannot be acquired: a bad URL,
// a transport failure or timeout, a non-success status, or a response that is
// not a page. It is never turned into a low-confidence draft.
var ErrFetch = errors.New("fetch failed")

// maxPageBytes bounds what one fetch reads, so a hostile or mistaken URL
// cannot fill memory. Recipe pages are far below it.
const maxPageBytes = 10 << 20

// HTTPClient is what the pipeline needs from an HTTP client; *http.Client
// satisfies it and tests substitute their own.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// fetch returns the body of rawURL and the URL it was finally served from.
func (p *Pipeline) fetch(ctx context.Context, rawURL string) (body []byte, finalURL string, err error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, "", fmt.Errorf("%w: %q is not an http or https URL", ErrFetch, rawURL)
	}
	ctx, cancel := context.WithTimeout(ctx, p.cfg.FetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("%w: build request for %s: %w", ErrFetch, u, err)
	}
	req.Header.Set("User-Agent", p.cfg.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.1")

	client := p.deps.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("%w: get %s: %w", ErrFetch, u, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, "", fmt.Errorf("%w: %s answered %d %s", ErrFetch, u, resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !isPage(ct) {
		return nil, "", fmt.Errorf("%w: %s is %q, not a web page", ErrFetch, u, ct)
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("%w: read %s: %w", ErrFetch, u, err)
	}
	if len(body) > maxPageBytes {
		return nil, "", fmt.Errorf("%w: %s is larger than %d bytes", ErrFetch, u, maxPageBytes)
	}
	final := u
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL
	}
	return body, final.String(), nil
}

// isPage reports whether a Content-Type is HTML or plain text.
func isPage(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return mt == "application/xhtml+xml" || strings.HasPrefix(mt, "text/")
}

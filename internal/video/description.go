package video

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Integration and FieldAPIKey address the YouTube Data API key in the
// integration settings store (ADR-00054, ADR-00057). The key is a secret.
const (
	Integration = "youtube"
	FieldAPIKey = "api_key"
)

// DefaultAPIHost is the origin of the YouTube Data API. The endpoint
// (GET /youtube/v3/videos with part=snippet and id=<video ID>), the snippet's
// title and description fields, the 1-unit quota cost and the error reasons
// below were checked against developers.google.com/youtube/v3/docs on
// 2026-11-11. That page does not document the `key` query parameter; it is
// Google's standard API-key parameter and could not be confirmed there.
const DefaultAPIHost = "https://www.googleapis.com"

// maxAPIResponseBytes bounds a videos.list answer; a snippet is a few
// kilobytes (a description is at most 5000 bytes).
const maxAPIResponseBytes = 1 << 20

// Errors a description lookup returns, wrapped, for errors.Is. None carries
// the API key.
var (
	// ErrNoKey means no API key is saved or it cannot be read: the lookup is
	// off. It is the normal state of a deployment that has not set one up.
	ErrNoKey = errors.New("no YouTube Data API key is saved")
	// ErrQuota means YouTube refused the call because the key's daily quota
	// is used up.
	ErrQuota = errors.New("the YouTube Data API quota is used up")
	// ErrKeyRejected means YouTube did not accept the key, or the key's
	// project does not have the YouTube Data API enabled.
	ErrKeyRejected = errors.New("the YouTube Data API did not accept the saved key")
	// ErrVideoUnavailable means the video does not exist, is private or has
	// been removed.
	ErrVideoUnavailable = errors.New("the video is private or has been removed")
	// ErrLookup is any other failure: a transport error, a timeout or an
	// answer that is not a videos.list response.
	ErrLookup = errors.New("the YouTube Data API lookup failed")
)

// KeyReader supplies the saved API key on every call, so a change on the
// Deployment page is in force on the next one. It returns an error wrapping
// ErrNoKey when there is none.
type KeyReader interface {
	APIKey(ctx context.Context) (string, error)
}

// Description is the text of a video's page that a recipe can be read from.
type Description struct {
	Title string
	Text  string
}

// Descriptions reads a video's title and description from the YouTube Data
// API.
type Descriptions struct {
	// Keys supplies the API key.
	Keys KeyReader
	// Client sends the request; nil means a client with a short timeout that
	// does not follow redirects.
	Client *http.Client
	// BaseURL replaces the API origin, with no trailing slash; empty means
	// DefaultAPIHost. It exists so a test or the e2e suite can point the
	// lookup at a local server, the HTTP boundary.
	BaseURL string
}

// Fetch returns the title and description of video id. It returns ErrNoKey
// without a request when no key is saved.
func (d *Descriptions) Fetch(ctx context.Context, id string) (Description, error) {
	if !IsID(id) {
		return Description{}, fmt.Errorf("%w: %q", ErrInvalidID, id)
	}
	key, err := d.Keys.APIKey(ctx)
	if err != nil {
		return Description{}, err
	}
	base := strings.TrimRight(d.BaseURL, "/")
	if base == "" {
		base = DefaultAPIHost
	}
	client := d.Client
	if client == nil {
		client = &http.Client{
			Timeout:       fetchTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	q := url.Values{"part": {"snippet"}, "id": {id}, "key": {key}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/youtube/v3/videos?"+q.Encode(), nil)
	if err != nil {
		return Description{}, fmt.Errorf("%w: build request for %s", ErrLookup, id)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		// A *url.Error prints the request URL, which carries the key. Only
		// the cause is kept.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return Description{}, fmt.Errorf("%w: ask for %s: %s", ErrLookup, id, redact(err.Error(), key))
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponseBytes+1))
	if err != nil {
		return Description{}, fmt.Errorf("%w: read the answer for %s: %s", ErrLookup, id, redact(err.Error(), key))
	}
	if len(body) > maxAPIResponseBytes {
		return Description{}, fmt.Errorf("%w: the answer for %s is larger than %d bytes", ErrLookup, id, maxAPIResponseBytes)
	}
	if resp.StatusCode != http.StatusOK {
		return Description{}, apiError(resp.StatusCode, body, id)
	}
	var out struct {
		Items []struct {
			Snippet struct {
				Title       string `json:"title"`
				Description string `json:"description"`
			} `json:"snippet"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return Description{}, fmt.Errorf("%w: the answer for %s is not a video list", ErrLookup, id)
	}
	if len(out.Items) == 0 {
		return Description{}, fmt.Errorf("%w: %s", ErrVideoUnavailable, id)
	}
	s := out.Items[0].Snippet
	return Description{Title: strings.TrimSpace(s.Title), Text: strings.TrimSpace(s.Description)}, nil
}

func redact(s, key string) string {
	if key == "" {
		return s
	}
	return strings.ReplaceAll(s, key, "[key]")
}

// apiError turns a non-200 answer into one of the package's errors, by the
// reasons in the body: the classic error.errors[].reason, and the reason of a
// google.rpc.ErrorInfo in error.details[] that current Google APIs add (an
// invalid key is 400 with details reason API_KEY_INVALID; a key restricted
// away from the API API_KEY_SERVICE_BLOCKED; a disabled API SERVICE_DISABLED).
//
// Checked 2026-11-11: the ErrorInfo shape (@type, reason, domain, metadata) in
// error.details[] is as google.aip.dev/193 gives it, and the classic reasons
// quotaExceeded, keyInvalid, accessNotConfigured and videoNotFound are on
// developers.google.com/youtube/v3/docs/core_errors. The three UPPER_SNAKE
// reasons are in neither page and have not been checked against a live
// answer; an unlisted reason falls to ErrLookup, never to a silent success.
// rateLimitExceeded and userRateLimitExceeded are short-window limits, not the
// day's quota, so they are ErrLookup and the person is not told to wait a day.
func apiError(status int, body []byte, id string) error {
	var e struct {
		Error struct {
			Errors []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
			Details []struct {
				Reason string `json:"reason"`
			} `json:"details"`
		} `json:"error"`
	}
	var reasons []string
	if json.Unmarshal(body, &e) == nil {
		for _, r := range e.Error.Errors {
			reasons = append(reasons, r.Reason)
		}
		for _, d := range e.Error.Details {
			reasons = append(reasons, d.Reason)
		}
	}
	for _, reason := range reasons {
		switch reason {
		case "quotaExceeded", "dailyLimitExceeded":
			return fmt.Errorf("%w (status %d, %s)", ErrQuota, status, reason)
		case "keyInvalid", "keyExpired", "accessNotConfigured", "ipRefererBlocked", "referrerBlocked",
			"API_KEY_INVALID", "API_KEY_SERVICE_BLOCKED", "SERVICE_DISABLED":
			return fmt.Errorf("%w (status %d, %s)", ErrKeyRejected, status, reason)
		case "videoNotFound":
			return fmt.Errorf("%w: %s", ErrVideoUnavailable, id)
		}
	}
	reason := ""
	if len(reasons) > 0 {
		reason = strings.Join(reasons, ", ")
	}
	if reason == "" {
		return fmt.Errorf("%w: the answer for %s was status %d", ErrLookup, id, status)
	}
	return fmt.Errorf("%w: the answer for %s was status %d (%s)", ErrLookup, id, status, reason)
}

// IsYouTubeHost reports whether host is one of YouTube's own: a link to it
// is never a recipe blog.
func IsYouTubeHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, h := range []string{"youtube.com", "youtu.be", "youtube-nocookie.com"} {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

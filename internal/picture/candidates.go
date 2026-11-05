package picture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/ingest/extract"
	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/netguard"
	"github.com/kaecyra/gobbler/internal/picture/picturedb"
	"github.com/kaecyra/gobbler/internal/store"
)

// Candidate limits (ADR-00039, Choosing).
const (
	// MaxCandidates is the most candidates a draft keeps.
	MaxCandidates = 8
	// MinWidth and MinHeight are the smallest source picture that fills the
	// widest card of the approved mockup (#435): 533 by 150 CSS pixels, so
	// 1066 by 300 at 2x. A smaller picture would be upscaled and soft.
	MinWidth  = 1066
	MinHeight = 300
	// FetchTimeout bounds one image download.
	FetchTimeout = 15 * time.Second
)

// Fetcher is what the candidate step needs to download an image; *http.Client
// satisfies it. Production code passes NewFetcher, which cannot reach a
// private address (ADR-00039, Fetching).
type Fetcher interface {
	Do(*http.Request) (*http.Response, error)
}

// NewFetcher returns the guarded client candidate downloads go through.
// allowLoopback is the dev auth bypass switch, as for page fetches.
func NewFetcher(allowLoopback bool) *http.Client {
	return netguard.NewClient(netguard.Options{Timeout: FetchTimeout, Redirects: netguard.MaxRedirects, AllowLoopback: allowLoopback})
}

// Found is a candidate that downloaded, decoded and passed the filters, not yet
// stored anywhere.
type Found struct {
	SourceURL     string
	WebP          []byte
	Width, Height int
}

// Candidate is a stored draft candidate.
type Candidate struct {
	Position      int
	SourceURL     string
	Key           string
	Width, Height int
}

// find downloads urls in order, processes each and applies the filters. A
// candidate that fails is logged and dropped, never an error: it is data about
// the page, not a broken pipeline (error-handling rule).
func (s *Service) find(ctx context.Context, urls []string) []Found {
	var out []Found
	seen := map[[sha256.Size]byte]bool{}
	for _, u := range urls {
		if len(out) >= MaxCandidates {
			break
		}
		f, sum, err := s.process(ctx, u)
		if err != nil {
			s.log.InfoContext(ctx, "picture candidate dropped", "url", u, "reason", err.Error())
			continue
		}
		if seen[sum] {
			s.log.InfoContext(ctx, "picture candidate dropped", "url", u, "reason", "same pixels as an earlier candidate")
			continue
		}
		seen[sum] = true
		out = append(out, f)
	}
	return out
}

func (s *Service) process(ctx context.Context, rawURL string) (Found, [sha256.Size]byte, error) {
	var none [sha256.Size]byte
	data, err := s.download(ctx, rawURL)
	if err != nil {
		return Found{}, none, err
	}
	d, err := media.Decode(data)
	if err != nil {
		return Found{}, none, fmt.Errorf("decode: %w", err)
	}
	b := d.Image.Bounds()
	if b.Dx() < MinWidth || b.Dy() < MinHeight {
		return Found{}, none, fmt.Errorf("%dx%d is smaller than %dx%d", b.Dx(), b.Dy(), MinWidth, MinHeight)
	}
	webp, err := media.Picture(d.Image)
	if err != nil {
		return Found{}, none, err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(webp))
	if err != nil {
		return Found{}, none, fmt.Errorf("read size of processed picture: %w", err)
	}
	return Found{SourceURL: rawURL, WebP: webp, Width: cfg.Width, Height: cfg.Height}, pixelHash(d.Image), nil
}

// pixelHash identifies a picture by its decoded pixels, so the same picture
// served in two encodings or at two addresses collapses to one candidate.
func pixelHash(img image.Image) [sha256.Size]byte {
	b := img.Bounds()
	n := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(n, n.Bounds(), img, b.Min, draw.Src)
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%dx%d:", b.Dx(), b.Dy())
	_, _ = h.Write(n.Pix) // hash.Hash.Write never returns an error
	var sum [sha256.Size]byte
	h.Sum(sum[:0])
	return sum
}

func (s *Service) download(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%q is not an http or https URL", rawURL)
	}
	ctx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "image/*")
	resp, err := s.fetch.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("answered %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, media.MaxUploadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	return data, nil
}

// FetchCandidates downloads urls (in order, from the extractor) for the draft
// proposal and keeps the survivors as its candidates under
// drafts/<proposal_id>/<n>.webp. Failures, pictures below the minimum size and
// repeated pixels are dropped, and at most MaxCandidates remain. Candidates
// the draft already had are replaced.
func (s *Service) FetchCandidates(ctx context.Context, proposalID int64, urls []string) ([]Candidate, error) {
	hid, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.DeleteCandidates(ctx, proposalID); err != nil {
		return nil, err
	}
	found := s.find(ctx, urls)
	var out []Candidate
	for i, f := range found {
		c := Candidate{Position: i + 1, SourceURL: f.SourceURL, Key: candidateKey(proposalID, i+1), Width: f.Width, Height: f.Height}
		if err := s.media.Put(ctx, c.Key, f.WebP, "image/webp"); err != nil {
			s.removeObjects(ctx, out)
			return nil, fmt.Errorf("store candidate %d of proposal %d: %w", c.Position, proposalID, err)
		}
		out = append(out, c)
	}
	err = db.WithTx(ctx, s.db, s.q, func(q *picturedb.Queries, _ *sql.Tx) error {
		for _, c := range out {
			if err := q.InsertCandidate(ctx, picturedb.InsertCandidateParams{
				HouseholdID: hid, ProposalID: proposalID, Position: int64(c.Position), SourceUrl: c.SourceURL,
				Key: c.Key, Width: int64(c.Width), Height: int64(c.Height),
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.removeObjects(ctx, out)
		return nil, fmt.Errorf("record candidates of proposal %d: %w", proposalID, err)
	}
	return out, nil
}

func (s *Service) removeObjects(ctx context.Context, cs []Candidate) {
	for _, c := range cs {
		s.deleteObject(ctx, c.Key, "draft candidate")
	}
}

// Candidates returns the stored candidates of the draft proposal, in order.
func (s *Service) Candidates(ctx context.Context, proposalID int64) ([]Candidate, error) {
	hid, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListCandidates(ctx, picturedb.ListCandidatesParams{ProposalID: proposalID, HouseholdID: hid})
	if err != nil {
		return nil, fmt.Errorf("list candidates of proposal %d: %w", proposalID, err)
	}
	out := make([]Candidate, len(rows))
	for i, r := range rows {
		out[i] = Candidate{Position: int(r.Position), SourceURL: r.SourceUrl, Key: r.Key, Width: int(r.Width), Height: int(r.Height)}
	}
	return out, nil
}

// DeleteCandidates deletes the draft's candidate rows and objects. Run it when
// a draft is confirmed, discarded, superseded or expired. A failed object
// delete is logged, not returned.
func (s *Service) DeleteCandidates(ctx context.Context, proposalID int64) error {
	hid, err := scope(ctx)
	if err != nil {
		return err
	}
	old, err := s.Candidates(ctx, proposalID)
	if err != nil {
		return err
	}
	if len(old) == 0 {
		return nil
	}
	err = db.WithTx(ctx, s.db, s.q, func(q *picturedb.Queries, _ *sql.Tx) error {
		return q.DeleteCandidates(ctx, picturedb.DeleteCandidatesParams{ProposalID: proposalID, HouseholdID: hid})
	})
	if err != nil {
		return fmt.Errorf("delete candidates of proposal %d: %w", proposalID, err)
	}
	s.removeObjects(ctx, old)
	return nil
}

// Promote makes candidate position of the draft proposal the picture of
// recipe id, copying it to recipes/<recipe_id>/<version>.webp, and then deletes
// all of the draft's candidates. A position the draft has no candidate for is
// store.ErrNotFound.
func (s *Service) Promote(ctx context.Context, proposalID int64, position int, id int64) (string, error) {
	cs, err := s.Candidates(ctx, proposalID)
	if err != nil {
		return "", err
	}
	var chosen *Candidate
	for i := range cs {
		if cs[i].Position == position {
			chosen = &cs[i]
		}
	}
	if chosen == nil {
		return "", fmt.Errorf("%w: candidate %d of proposal %d", store.ErrNotFound, position, proposalID)
	}
	data, err := s.media.Get(ctx, chosen.Key)
	if err != nil {
		return "", fmt.Errorf("read candidate %d of proposal %d: %w", position, proposalID, err)
	}
	key, err := s.setProcessed(ctx, id, data)
	if err != nil {
		return "", err
	}
	if err := s.DeleteCandidates(ctx, proposalID); err != nil {
		// The picture is set; the leftovers go when the proposal is pruned.
		s.log.WarnContext(ctx, "delete promoted draft's candidates", "proposal_id", proposalID, "error", err.Error())
	}
	return key, nil
}

// Refind runs the candidate step against recipe id's saved html source snapshot
// and returns what survives, for a choice (Choose). A recipe with no html
// snapshot, or a page naming no usable image, has no candidates: that is an
// answer, not an error. Nothing is stored.
func (s *Service) Refind(ctx context.Context, id int64) ([]Found, error) {
	hid, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	page, err := s.source(ctx, hid, id)
	if err != nil {
		return nil, err
	}
	snap, err := s.snapshots.GetSnapshot(ctx, id, store.SnapshotHTML)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read html snapshot of recipe %d: %w", id, err)
	}
	res, err := extract.Extract(snap.Content, page)
	if err != nil || res.Recipe == nil {
		if err != nil {
			s.log.InfoContext(ctx, "re-find found no recipe in the saved page", "recipe_id", id, "error", err.Error())
		}
		return nil, nil
	}
	return s.find(ctx, res.Recipe.ImageURLs), nil
}

// Choose sets a candidate returned by Refind as recipe id's picture, replacing
// the previous one. It returns the new key.
func (s *Service) Choose(ctx context.Context, id int64, f Found) (string, error) {
	return s.setProcessed(ctx, id, f.WebP)
}

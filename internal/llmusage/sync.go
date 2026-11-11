package llmusage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/llm"
	"github.com/kaecyra/gobbler/internal/llmusage/llmusagedb"
)

// ErrSyncFailed is returned (wrapped) by Sync when the sync failed and the
// failure was recorded, so the page shows it as the last failure. Any other
// error from Sync means even that could not be recorded.
var ErrSyncFailed = errors.New("price sync failed")

// DefaultOpenRouterURL is the origin of OpenRouter's public API.
const DefaultOpenRouterURL = "https://openrouter.ai"

// modelsPath is the public model listing, fetched without credentials.
const modelsPath = "/api/v1/models"

const (
	fetchTimeout = 30 * time.Second
	// maxListingBytes bounds the listing read; the real one is a few hundred
	// kilobytes.
	maxListingBytes = 16 << 20
	// SyncEvery is how long a successful sync stays current.
	SyncEvery = 24 * time.Hour
)

// openRouterPrefix is what OpenRouter puts before the provider's own model id.
// A model matches only the entry whose id is exactly this prefix followed by the
// id gobbler uses; there is no other normalisation (ADR-00058).
var openRouterPrefix = map[string]string{
	llm.ProviderClaude: "anthropic/",
	llm.ProviderGemini: "google/",
}

// Syncer sets prices from OpenRouter's model listing. Create it with NewSyncer.
// It is safe for concurrent use: a sync in progress makes another wait.
type Syncer struct {
	store  *Store
	client *http.Client
	url    string
	log    *slog.Logger
	mu     sync.Mutex
}

// NewSyncer returns a Syncer for store that reads the listing from the
// OpenRouter at baseURL ("" is the real one). A nil log means slog.Default().
func NewSyncer(store *Store, baseURL string, log *slog.Logger) *Syncer {
	if baseURL == "" {
		baseURL = DefaultOpenRouterURL
	}
	if log == nil {
		log = slog.Default()
	}
	return &Syncer{store: store, client: &http.Client{Timeout: fetchTimeout}, url: strings.TrimRight(baseURL, "/") + modelsPath, log: log}
}

// listing is the part of OpenRouter's /api/v1/models answer that is read. The
// prices are US dollars per token, as decimal strings.
type listing struct {
	Data []struct {
		ID      string `json:"id"`
		Pricing struct {
			Prompt     string `json:"prompt"`
			Completion string `json:"completion"`
		} `json:"pricing"`
	} `json:"data"`
}

// Sync fetches the listing and updates the price of every model a call has used
// or a task is set to use. A model with no entry of exactly the provider's
// prefix and its id is left as it is, and so is a price the operator set. A
// sync that fails changes no price: the failure is logged and recorded for the
// page, and returned wrapping ErrSyncFailed.
func (s *Syncer) Sync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.sync(ctx)
	if err == nil {
		return nil
	}
	s.log.WarnContext(ctx, "llm price sync failed; prices left as they were", "error", err.Error())
	// Recording the failure must outlive a cancelled request.
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if rerr := s.store.recordFailure(rctx, err); rerr != nil {
		return errors.Join(err, rerr)
	}
	return fmt.Errorf("%w: %w", ErrSyncFailed, err)
}

func (s *Syncer) sync(ctx context.Context) error {
	wanted, err := s.store.wanted(ctx)
	if err != nil {
		return err
	}
	l, err := s.fetch(ctx)
	if err != nil {
		return err
	}
	byID := make(map[string]int, len(l.Data))
	for i, e := range l.Data {
		byID[e.ID] = i
	}
	type update struct {
		ModelRef
		in, out int64
	}
	var updates []update
	for _, m := range wanted {
		prefix, ok := openRouterPrefix[m.Provider]
		if !ok {
			continue
		}
		i, ok := byID[prefix+m.Model]
		if !ok {
			continue
		}
		e := l.Data[i]
		in, inErr := parseUSDPerToken(e.Pricing.Prompt)
		out, outErr := parseUSDPerToken(e.Pricing.Completion)
		if err := errors.Join(inErr, outErr); err != nil {
			// OpenRouter lists a variable-price model with a negative price.
			s.log.WarnContext(ctx, "llm price sync: listed price not usable; model left as it was",
				"provider", m.Provider, "model", m.Model, "error", err.Error())
			continue
		}
		updates = append(updates, update{m, in, out})
	}
	now := s.store.now()
	err = db.WithTx(ctx, s.store.db, s.store.q, func(q *llmusagedb.Queries, _ *sql.Tx) error {
		for _, u := range updates {
			_, err := q.UpsertSyncedPrice(ctx, llmusagedb.UpsertSyncedPriceParams{
				Provider: u.Provider, Model: u.Model, InputMicros: u.in, OutputMicros: u.out,
				SyncedAt: sql.NullString{String: stamp(now), Valid: true}, UpdatedAt: stamp(now),
			})
			if err != nil {
				return fmt.Errorf("set price of %s %s: %w", u.Provider, u.Model, err)
			}
		}
		return q.RecordSyncSuccess(ctx, sql.NullString{String: stamp(now), Valid: true})
	})
	if err != nil {
		return fmt.Errorf("store synced prices: %w", err)
	}
	s.log.InfoContext(ctx, "llm price sync", "listed", len(l.Data), "wanted", len(wanted), "priced", len(updates))
	return nil
}

func (s *Syncer) fetch(ctx context.Context) (listing, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return listing{}, fmt.Errorf("build request for OpenRouter models: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return listing{}, fmt.Errorf("fetch OpenRouter models: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			s.log.WarnContext(ctx, "close OpenRouter response", "error", err.Error())
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return listing{}, fmt.Errorf("fetch OpenRouter models: status %d", resp.StatusCode)
	}
	var l listing
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxListingBytes)).Decode(&l); err != nil {
		return listing{}, fmt.Errorf("read OpenRouter models: %w", err)
	}
	if len(l.Data) == 0 {
		return listing{}, errors.New("read OpenRouter models: the listing has no models")
	}
	return l, nil
}

// wanted is every model a call has used or a task is set to use (ADR-00058).
func (s *Store) wanted(ctx context.Context) ([]ModelRef, error) {
	var refs []ModelRef
	if s.configured != nil {
		var err error
		if refs, err = s.configured.Configured(ctx); err != nil {
			return nil, fmt.Errorf("list configured models: %w", err)
		}
	}
	used, err := s.q.UsedModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("list used models: %w", err)
	}
	for _, u := range used {
		if m := (ModelRef{u.Provider, u.Model}); !slices.Contains(refs, m) {
			refs = append(refs, m)
		}
	}
	return refs, nil
}

func (s *Store) recordFailure(ctx context.Context, cause error) error {
	err := db.WithTx(ctx, s.db, s.q, func(q *llmusagedb.Queries, _ *sql.Tx) error {
		return q.RecordSyncFailure(ctx, llmusagedb.RecordSyncFailureParams{
			LastFailureAt: sql.NullString{String: stamp(s.now()), Valid: true},
			LastError:     sql.NullString{String: truncate(cause.Error(), MaxErrorBytes), Valid: true},
		})
	})
	if err != nil {
		return fmt.Errorf("record price sync failure: %w", err)
	}
	return nil
}

// Due reports whether the daily sync should run now: nothing has succeeded in
// the last SyncEvery.
func (s *Store) Due(ctx context.Context) (bool, error) {
	st, err := s.SyncStatus(ctx)
	if err != nil {
		return false, err
	}
	return s.now().Sub(st.LastSuccess) >= SyncEvery, nil
}

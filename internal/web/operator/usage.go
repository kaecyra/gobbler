package operator

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/llm"
	"github.com/kaecyra/gobbler/internal/llmusage"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// UsagePath is the LLM usage page (ADR-00058).
const UsagePath = Pattern + "/usage"

// Usage is what the usage page reads and sets: the report over a window, the
// price list and the sync status, and the operator's prices. *llmusage.Store
// satisfies it.
type Usage interface {
	Report(ctx context.Context, w llmusage.Window) (llmusage.Report, error)
	Prices(ctx context.Context) ([]llmusage.PriceRow, error)
	SyncStatus(ctx context.Context) (llmusage.SyncStatus, error)
	SetPrice(ctx context.Context, provider, model, input, output string) error
	ClearPrice(ctx context.Context, provider, model string) error
}

// PriceSyncer fetches model prices now. *llmusage.Syncer satisfies it.
type PriceSyncer interface {
	Sync(ctx context.Context) error
}

// usageRoutes adds the usage page to r. Without Usage or PriceSync they answer
// 500 with the reason, as the other operator pages do for a missing dependency,
// so a router built without them fails loud instead of dereferencing nil.
func (h *handler) usageRoutes(r chi.Router) {
	if h.Usage == nil || h.PriceSync == nil {
		unavailable := func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "the usage page is not available: the LLM usage store is required", http.StatusInternalServerError)
		}
		r.Get("/usage", unavailable)
		r.Post("/usage/sync", unavailable)
		r.Post("/usage/prices", unavailable)
		r.Post("/usage/prices/clear", unavailable)
		return
	}
	r.Get("/usage", h.usage)
	r.Post("/usage/sync", h.syncPrices)
	r.Post("/usage/prices", h.setPrice)
	r.Post("/usage/prices/clear", h.clearPrice)
}

// windowOf is the window a request names, the default when it names none. An
// unknown name is a bad request.
func windowOf(raw string) (llmusage.Window, bool) {
	if raw == "" {
		return llmusage.Windows[0], true
	}
	return llmusage.WindowByKey(raw)
}

func (h *handler) usage(w http.ResponseWriter, r *http.Request) {
	if _, ok := operatorEmail(w, r); !ok {
		return
	}
	win, ok := windowOf(r.URL.Query().Get("window"))
	if !ok {
		layout.Render(w, r, http.StatusBadRequest, layout.ErrorPage(http.StatusBadRequest, "That is not a window the usage page offers."))
		return
	}
	h.renderUsage(w, r, http.StatusOK, win, "")
}

// renderUsage draws the page over win with status; problem is a page-level
// message about what the operator just tried.
func (h *handler) renderUsage(w http.ResponseWriter, r *http.Request, status int, win llmusage.Window, problem string) {
	ctx := r.Context()
	rep, err := h.Usage.Report(ctx, win)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	prices, err := h.Usage.Prices(ctx)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	st, err := h.Usage.SyncStatus(ctx)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	layout.Render(w, r, status, usagePage(newUsageView(rep, prices, st, problem)))
}

// backToUsage sends the browser to the page over the window the form was on.
func backToUsage(w http.ResponseWriter, r *http.Request, windowKey string) {
	target := UsagePath
	if windowKey != "" && windowKey != llmusage.Windows[0].Key {
		target += "?" + url.Values{"window": {windowKey}}.Encode()
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// formWindow is the window a posted form was on, the default when it names
// none or one that does not exist.
func formWindow(r *http.Request) llmusage.Window {
	win, ok := windowOf(r.PostFormValue("window"))
	if !ok {
		return llmusage.Windows[0]
	}
	return win
}

func (h *handler) syncPrices(w http.ResponseWriter, r *http.Request) {
	if _, ok := operatorEmail(w, r); !ok {
		return
	}
	win := formWindow(r)
	// A failed sync is logged and recorded by the syncer, and the page shows it
	// as the last failure; the redirect lands on it. Anything else, such as a
	// failure that could not be recorded, is an error.
	if err := h.PriceSync.Sync(r.Context()); err != nil && !errors.Is(err, llmusage.ErrSyncFailed) {
		layout.Fail(w, r, err)
		return
	}
	backToUsage(w, r, win.Key)
}

func (h *handler) setPrice(w http.ResponseWriter, r *http.Request) {
	if _, ok := operatorEmail(w, r); !ok {
		return
	}
	win := formWindow(r)
	err := h.Usage.SetPrice(r.Context(), r.PostFormValue("provider"), r.PostFormValue("model"), r.PostFormValue("input"), r.PostFormValue("output"))
	if errors.Is(err, llmusage.ErrInvalid) {
		h.renderUsage(w, r, http.StatusUnprocessableEntity, win, err.Error())
		return
	}
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	backToUsage(w, r, win.Key)
}

// isManual reports whether the model's price is one the operator set.
func (h *handler) isManual(ctx context.Context, provider, model string) (bool, error) {
	rows, err := h.Usage.Prices(ctx)
	if err != nil {
		return false, err
	}
	for _, r := range rows {
		if r.Provider == provider && r.Model == model {
			return r.Price != nil && r.Price.Source == llmusage.SourceManual, nil
		}
	}
	return false, nil
}

func (h *handler) clearPrice(w http.ResponseWriter, r *http.Request) {
	if _, ok := operatorEmail(w, r); !ok {
		return
	}
	win := formWindow(r)
	provider, model := r.PostFormValue("provider"), r.PostFormValue("model")
	wasManual, err := h.isManual(r.Context(), provider, model)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	err = h.Usage.ClearPrice(r.Context(), provider, model)
	if errors.Is(err, llmusage.ErrInvalid) {
		h.renderUsage(w, r, http.StatusUnprocessableEntity, win, err.Error())
		return
	}
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	// "Use synced price" gives the price back at once: left to the daily sync,
	// the model would stay unpriced for up to a day, and every call in that
	// gap would be recorded with no cost for good (ADR-00058). A failed sync is
	// shown on the page as the last failure, as for Sync now.
	if wasManual {
		if err := h.PriceSync.Sync(r.Context()); err != nil && !errors.Is(err, llmusage.ErrSyncFailed) {
			layout.Fail(w, r, err)
			return
		}
	}
	backToUsage(w, r, win.Key)
}

// usageView is what the usage page draws.
type usageView struct {
	Problem string
	Windows []windowLink
	Window  string
	// Stats are the headline numbers over the window.
	Stats       []stat
	ByModel     []usageRow
	ByHousehold []usageRow
	Failures    []failureRow
	Sync        syncView
	Prices      []priceRowView
}

type windowLink struct {
	Key, Label, Href string
	Current          bool
}

// stat is one headline number: a label, its value and, when something about it
// would mislead, a note.
type stat struct {
	TestID, Label, Value, Note string
}

// usageRow is one line of a breakdown table.
type usageRow struct {
	// Name, Provider and Task identify the row: a model row names all three, a
	// household row only Name.
	Task, Provider, Name string
	Calls, In, Out       string
	Failed               string
	Cost                 string
	Unpriced             string
}

type failureRow struct {
	At, Task, Model, Household, Error string
}

type syncView struct {
	Success, Failure, Error string
	// FailureIsLatest marks the failure as newer than the last success, so the
	// page raises it.
	FailureIsLatest bool
}

type priceRowView struct {
	Provider, Model     string
	Priced              bool
	Input, Output       string
	InputRaw, OutputRaw string
	Source, Synced      string
	Manual              bool
}

func number(n int64) string { return strconv.FormatInt(n, 10) }

func usageTime(t time.Time) string { return t.UTC().Format(timeLayout) }

// costCell is a cost, or "Unpriced" when none of the calls had a price: a call
// with no price adds nothing to the cost, and the cell must not read as free.
func costCell(t llmusage.Totals) string {
	if t.Calls > 0 && t.Unpriced == t.Calls {
		return "Unpriced"
	}
	return llmusage.FormatCost(t.CostMicros)
}

func rowOf(t llmusage.Totals) usageRow {
	return usageRow{
		Calls: number(t.Calls), In: number(t.InputTokens), Out: number(t.OutputTokens),
		Failed: number(t.Failed), Cost: costCell(t), Unpriced: number(t.Unpriced),
	}
}

func newUsageView(rep llmusage.Report, prices []llmusage.PriceRow, st llmusage.SyncStatus, problem string) usageView {
	v := usageView{Problem: problem, Window: rep.Window.Key}
	for _, w := range llmusage.Windows {
		href := UsagePath
		if w.Key != llmusage.Windows[0].Key {
			href += "?" + url.Values{"window": {w.Key}}.Encode()
		}
		v.Windows = append(v.Windows, windowLink{Key: w.Key, Label: w.Label, Href: href, Current: w.Key == rep.Window.Key})
	}

	t := rep.Totals
	cost := stat{TestID: "usage-cost", Label: "Cost", Value: costCell(t)}
	if t.Unpriced > 0 && t.Unpriced < t.Calls {
		cost.Note = number(t.Unpriced) + " unpriced, not in this total"
	}
	failed := stat{TestID: "usage-failed", Label: "Failed", Value: number(t.Failed)}
	if t.Calls > 0 {
		failed.Note = strconv.FormatFloat(t.FailedShare()*100, 'f', 1, 64) + "% of calls"
	}
	v.Stats = []stat{
		cost,
		{TestID: "usage-calls", Label: "Calls", Value: number(t.Calls)},
		{TestID: "usage-in", Label: "Tokens in", Value: number(t.InputTokens)},
		{TestID: "usage-out", Label: "Tokens out", Value: number(t.OutputTokens)},
		failed,
	}

	for _, m := range rep.ByModel {
		r := rowOf(m.Totals)
		r.Task, r.Provider, r.Name = m.Task.String(), m.Provider, m.Model
		v.ByModel = append(v.ByModel, r)
	}
	for _, h := range rep.ByHousehold {
		r := rowOf(h.Totals)
		r.Name = h.Name
		if h.HouseholdID == 0 {
			r.Name = "No household"
		}
		v.ByHousehold = append(v.ByHousehold, r)
	}
	for _, f := range rep.Failures {
		household := f.Household
		if household == "" {
			household = "No household"
		}
		v.Failures = append(v.Failures, failureRow{
			At: usageTime(f.At), Task: f.Task.String(), Model: f.Provider + " " + f.Model, Household: household, Error: f.Error,
		})
	}

	if !st.LastSuccess.IsZero() {
		v.Sync.Success = usageTime(st.LastSuccess)
	}
	if !st.LastFailure.IsZero() {
		v.Sync.Failure, v.Sync.Error = usageTime(st.LastFailure), st.LastError
		v.Sync.FailureIsLatest = !st.LastFailure.Before(st.LastSuccess)
	}

	for _, p := range prices {
		pv := priceRowView{Provider: p.Provider, Model: p.Model}
		if p.Price != nil {
			pv.Priced = true
			pv.Input, pv.Output = llmusage.FormatPrice(p.Price.InputMicros), llmusage.FormatPrice(p.Price.OutputMicros)
			pv.InputRaw, pv.OutputRaw = rawPrice(p.Price.InputMicros), rawPrice(p.Price.OutputMicros)
			pv.Manual = p.Price.Source == llmusage.SourceManual
			pv.Source = "OpenRouter"
			if pv.Manual {
				pv.Source = "Set by you"
			} else {
				pv.Synced = usageTime(p.Price.SyncedAt)
			}
		}
		v.Prices = append(v.Prices, pv)
	}
	return v
}

// rawPrice is a price as the form takes it back: dollars per million tokens,
// without the sign.
func rawPrice(micros int64) string { return llmusage.FormatPrice(micros)[1:] }

// providerLabel is a provider as the price list names it.
func providerLabel(p string) string {
	switch p {
	case llm.ProviderClaude:
		return "Claude"
	case llm.ProviderGemini:
		return "Gemini"
	}
	return p
}

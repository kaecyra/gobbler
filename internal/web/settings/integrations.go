package settings

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"

	store "github.com/kaecyra/gobbler/internal/settings"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// secretsPath is where a secret's Clear posts, and valuesPath where an
// integration's one form posts, below Pattern.
const (
	secretsPath = Pattern + "/secrets"
	valuesPath  = Pattern + "/values"
)

// SecretStore is what the page uses from the integration settings store
// (ADR-00057). *settings.Store satisfies it. Nothing it offers returns a
// secret's value, so the page cannot show one.
type SecretStore interface {
	SecretState(ctx context.Context, f store.Field) (store.State, error)
	SetSecret(ctx context.Context, f store.Field, value string) error
	Clear(ctx context.Context, f store.Field) error
}

// SecretField is one secret an integration keeps.
type SecretField struct {
	// Name is the field name in the store, such as "password".
	Name string
	// Label is what the page calls it.
	Label string
	// After is the name of the plain field it follows in the section's form;
	// empty puts it after them all.
	After string
}

// ValueStore is what the page uses for an integration's plain settings.
// *settings.Store satisfies it.
type ValueStore interface {
	Value(ctx context.Context, f store.Field) (string, error)
	SetValue(ctx context.Context, f store.Field, value string) error
	Clear(ctx context.Context, f store.Field) error
}

// IntegrationStore is the integration settings store as the page uses it.
type IntegrationStore interface {
	SecretStore
	ValueStore
}

// PlainField is one plain, non-secret setting an integration keeps. The page
// shows its saved value; a blank value clears it.
type PlainField struct {
	// Name is the field name in the store, such as "host".
	Name string
	// Label is what the page calls it.
	Label string
	// Hint is a short line under the input, optional.
	Hint string
	// Placeholder is shown while the input is empty, optional.
	Placeholder string
	// Select makes the field a select instead of a text input (the LLM's
	// provider and model); its options come from the section's Choices.
	Select bool
}

// Choice is one option of a select field.
type Choice struct {
	// Value is what is saved; empty is the option that clears the field.
	Value string
	// Label is what the page shows.
	Label string
}

// Choices are the options of a select field.
type Choices struct {
	// Options are what may be chosen, including the empty one.
	Options []Choice
	// Problem, when set, is why the options could not be had. The page then
	// shows the saved value as text with the problem, offers no input, and a
	// save leaves the field as it is.
	Problem string
}

// Section is one integration's settings on the page. An integration registers
// its section when it is wired in (mail, LLM); the page renders whatever is
// registered and nothing else.
type Section struct {
	// Integration is the name in the store, such as "mail".
	Integration string
	// Title is the section heading.
	Title string
	// Fields are the plain settings. They and Secrets are saved together by
	// the section's one form.
	Fields []PlainField
	// Choices, when set, is called once for each render of the section and
	// returns the options of its Select fields by field name. It reads what is
	// current, and it is where a section does slow work (listing a provider's
	// models) once, and concurrently, instead of once per field.
	Choices func(ctx context.Context) map[string]Choices
	Secrets []SecretField
	// Validate checks the plain values as submitted (keyed by field name,
	// trimmed, blank when absent) before any is saved, given which of the
	// section's secrets are set: saved and readable, or typed in the same
	// submit. It returns the problem to show, or "" when they can be saved.
	// Nil accepts anything.
	//
	// A select field the page offered no input for (Choices.Problem) is not in
	// values and is not saved. Validate may change values to say what is saved
	// instead: setting a field to "" clears it, so a section can drop a field
	// that no longer applies, as the LLM does with a model when its provider
	// changes. It runs only when nothing is refused.
	Validate func(ctx context.Context, values map[string]string, secretSet map[string]bool) string
}

// Integrations is the integration settings the page shows and edits. With no
// Sections the page shows nothing about integrations.
type Integrations struct {
	Store    IntegrationStore
	Sections []Section
}

func (i Integrations) section(integration string) (Section, bool) {
	for _, s := range i.Sections {
		if s.Integration == integration {
			return s, true
		}
	}
	return Section{}, false
}

func (i Integrations) secret(integration, name string) (store.Field, bool) {
	sec, ok := i.section(integration)
	if !ok {
		return store.Field{}, false
	}
	for _, f := range sec.Secrets {
		if f.Name == name {
			return store.Field{Integration: integration, Name: name}, true
		}
	}
	return store.Field{}, false
}

// maxFormBytes bounds one posted field: the longest value, with room for the
// field name and encoding.
const maxFormBytes = store.MaxValueBytes*3 + 1024

func (h *handler) clearSecret(w http.ResponseWriter, r *http.Request) {
	f, ok := h.Integrations.secret(chi.URLParam(r, "integration"), chi.URLParam(r, "field"))
	if !ok {
		layout.WriteError(w, r, layout.ErrNotFound)
		return
	}
	if err := h.Integrations.Store.Clear(r.Context(), f); err != nil {
		layout.WriteError(w, r, err)
		return
	}
	h.savedOutcome(w, r)
}

// savedOutcome answers a save or a clear that succeeded: a redirect back to
// the page for a browser without script, the integrations section for HTMX.
func (h *handler) savedOutcome(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") != "true" {
		http.Redirect(w, r, Pattern, http.StatusSeeOther)
		return
	}
	h.renderIntegrations(w, r, http.StatusOK, "", entered{})
}

// renderIntegrations answers with the integrations section for HTMX and with
// the whole page otherwise. A refusal is a 200 for HTMX, which does not swap a
// 4xx. entered carries the values the person typed, which a refusal keeps.
func (h *handler) renderIntegrations(w http.ResponseWriter, r *http.Request, status int, problem string, in entered) {
	htmx := r.Header.Get("HX-Request") == "true"
	v, err := h.integrationsView(r.Context(), problem, in)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	if htmx {
		layout.Render(w, r, http.StatusOK, integrationsSection(v))
		return
	}
	layout.Render(w, r, status, settingsPage(h.view(outcomeNone), v))
}

// Secret states as the page words them.
const (
	stateSet        = "Saved"
	stateUnset      = "Not set"
	stateUnreadable = "Unreadable. Enter it again."
)

// entered is the values the person submitted for one integration's plain
// fields, shown again when the save is refused. A typed secret is never among
// them: it is not sent back to the browser (ADR-00057).
type entered struct {
	Integration string
	Values      map[string]string
}

type integrationsView struct {
	// Problem is a refusal to show above the sections.
	Problem  string
	Sections []sectionView
}

type sectionView struct {
	Integration string
	Title       string
	// Action is where the section's one form posts.
	Action string
	// Rows are the form's fields in page order: each is a plain field or a
	// secret.
	Rows []rowView
}

// rowView is one field of a section's form; exactly one of Field and Secret
// is set.
type rowView struct {
	Field  *fieldView
	Secret *secretView
}

type fieldView struct {
	Name, Label, Hint, Placeholder string
	Value                          string
	// Select is true for a field with Choices; Options are its options, with
	// the one matching Value selected.
	Select  bool
	Options []optionView
	// Problem is why a select has no options to offer; the field then shows
	// Value as text and posts nothing.
	Problem string
}

type optionView struct {
	Value, Label string
	Selected     bool
}

// noLongerOffered marks a saved choice the list no longer has, so it can be
// replaced.
const noLongerOffered = " (no longer offered)"

// selectView fills fv for a select field with the options c offers. A saved
// value among them is selected; one that is not is added, marked, and
// selected, so it is neither hidden nor lost.
func selectView(fv fieldView, c Choices) fieldView {
	fv.Select = true
	if c.Problem != "" {
		fv.Problem = c.Problem
		return fv
	}
	found := false
	for _, o := range c.Options {
		sel := o.Value == fv.Value
		found = found || sel
		fv.Options = append(fv.Options, optionView{Value: o.Value, Label: o.Label, Selected: sel})
	}
	if !found && fv.Value != "" {
		fv.Options = append(fv.Options, optionView{Value: fv.Value, Label: fv.Value + noLongerOffered, Selected: true})
	}
	return fv
}

type secretView struct {
	Name  string
	Label string
	// State is the tag beside the label and Tag its tint: "ok", "unset" or
	// "unreadable".
	State, Tag string
	// Saved is true when a readable value is stored; the input then shows
	// dots as its placeholder.
	Saved bool
	// Stored is true when there is something to clear: saved or unreadable.
	Stored bool
	// ClearAction is where clearing posts, from the form ClearForm names,
	// which sits outside the section's form.
	ClearAction, ClearForm string
}

// savedPlaceholder stands in a saved secret's empty input. It is not the
// secret, nor its length.
const savedPlaceholder = "••••••••••••"

func (h *handler) integrationsView(ctx context.Context, problem string, in entered) (integrationsView, error) {
	v := integrationsView{Problem: problem}
	if h.Integrations.Store == nil {
		return v, nil
	}
	for _, s := range h.Integrations.Sections {
		sv := sectionView{Integration: s.Integration, Title: s.Title, Action: valuesPath + "/" + s.Integration}
		var choices map[string]Choices
		if s.Choices != nil {
			choices = s.Choices(ctx)
		}
		// secrets are the section's secret rows keyed by the field each
		// follows, "" for after them all.
		secrets := make(map[string][]rowView, len(s.Secrets))
		for _, f := range s.Secrets {
			if f.After != "" && !slices.ContainsFunc(s.Fields, func(p PlainField) bool { return p.Name == f.After }) {
				return integrationsView{}, fmt.Errorf("%s secret %s follows %q, which is not one of its fields", s.Integration, f.Name, f.After)
			}
			sec, err := h.secretView(ctx, s.Integration, f)
			if err != nil {
				return integrationsView{}, err
			}
			secrets[f.After] = append(secrets[f.After], rowView{Secret: &sec})
		}
		for _, f := range s.Fields {
			fv := fieldView{Name: f.Name, Label: f.Label, Hint: f.Hint, Placeholder: f.Placeholder}
			typed, wasTyped := in.Values[f.Name]
			if in.Integration == s.Integration && wasTyped {
				fv.Value = typed
			} else {
				val, err := h.Integrations.Store.Value(ctx, store.Field{Integration: s.Integration, Name: f.Name})
				if err != nil && !errors.Is(err, store.ErrUnset) {
					// As for a secret: a failed read must not look like an empty field.
					return integrationsView{}, fmt.Errorf("read %s %s: %w", s.Integration, f.Name, err)
				}
				fv.Value = val
			}
			if f.Select {
				fv = selectView(fv, choices[f.Name])
			}
			sv.Rows = append(sv.Rows, rowView{Field: &fv})
			sv.Rows = append(sv.Rows, secrets[f.Name]...)
		}
		sv.Rows = append(sv.Rows, secrets[""]...)
		v.Sections = append(v.Sections, sv)
	}
	return v, nil
}

func (h *handler) secretView(ctx context.Context, integration string, f SecretField) (secretView, error) {
	st, err := h.Integrations.Store.SecretState(ctx, store.Field{Integration: integration, Name: f.Name})
	if err != nil {
		// A failed read is not an unset secret: showing "Not set" would
		// invite overwriting one that may be stored.
		return secretView{}, fmt.Errorf("read state of %s %s: %w", integration, f.Name, err)
	}
	sec := secretView{
		Name: f.Name, Label: f.Label, State: stateUnset, Tag: "unset",
		ClearAction: secretsPath + "/" + integration + "/" + f.Name + "/clear",
		ClearForm:   "clear-" + integration + "-" + f.Name,
	}
	switch st {
	case store.StateSet:
		sec.State, sec.Tag, sec.Saved, sec.Stored = stateSet, "ok", true, true
	case store.StateUnreadable:
		sec.State, sec.Tag, sec.Stored = stateUnreadable, "unreadable", true
	}
	return sec, nil
}

// saveValues saves an integration's form: its plain fields and its secrets,
// together. A blank plain field is cleared; a blank secret keeps what is
// stored, and a typed one replaces it. The section's Validate runs first,
// counting a typed secret as set, and a refusal saves nothing.
func (h *handler) saveValues(w http.ResponseWriter, r *http.Request) {
	integration := chi.URLParam(r, "integration")
	sec, ok := h.Integrations.section(integration)
	if !ok {
		layout.WriteError(w, r, layout.ErrNotFound)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, int64(len(sec.Fields)+len(sec.Secrets))*maxFormBytes)
	if err := r.ParseForm(); err != nil {
		h.renderIntegrations(w, r, http.StatusBadRequest, "That form could not be read.", entered{})
		return
	}
	values := make(map[string]string, len(sec.Fields))
	for _, f := range sec.Fields {
		if _, posted := r.PostForm[f.Name]; !posted && f.Select {
			// The page offered no input for it (its options could not be had).
			continue
		}
		values[f.Name] = strings.TrimSpace(r.PostForm.Get(f.Name))
	}
	// typed are the secrets given a value, in the section's order. A blank
	// one is not among them and keeps what is stored.
	var typed []SecretField
	secrets := make(map[string]string, len(sec.Secrets))
	for _, f := range sec.Secrets {
		if v := r.PostForm.Get(f.Name); v != "" {
			typed = append(typed, f)
			secrets[f.Name] = v
		}
	}
	refuse := func(msg string) {
		h.renderIntegrations(w, r, http.StatusUnprocessableEntity, msg, entered{Integration: integration, Values: values})
	}
	for _, f := range sec.Fields {
		if len(values[f.Name]) > store.MaxValueBytes {
			refuse(f.Label + " is too long.")
			return
		}
	}
	for _, f := range typed {
		if len(secrets[f.Name]) > store.MaxValueBytes {
			refuse(f.Label + " is too long.")
			return
		}
	}
	if sec.Validate != nil {
		set := make(map[string]bool, len(sec.Secrets))
		for _, sf := range sec.Secrets {
			if _, ok := secrets[sf.Name]; ok {
				set[sf.Name] = true
				continue
			}
			st, err := h.Integrations.Store.SecretState(r.Context(), store.Field{Integration: integration, Name: sf.Name})
			if err != nil {
				layout.WriteError(w, r, fmt.Errorf("read state of %s %s: %w", integration, sf.Name, err))
				return
			}
			set[sf.Name] = st == store.StateSet
		}
		if msg := sec.Validate(r.Context(), values, set); msg != "" {
			refuse(msg)
			return
		}
	}
	// The secrets are written before any plain field. The first plain field
	// is the one that turns the integration on (mail's host): it is set last,
	// so a failure part way never leaves it on with the rest missing. The
	// encryption key is the same for every secret, so a missing one refuses
	// the first and nothing has been written.
	for _, f := range typed {
		err := h.Integrations.Store.SetSecret(r.Context(), store.Field{Integration: integration, Name: f.Name}, secrets[f.Name])
		switch {
		case errors.Is(err, store.ErrNoKey):
			refuse(f.Label + " was not saved: the encryption key is missing. Set the encryption key in the environment and restart.")
			return
		case err != nil:
			layout.WriteError(w, r, fmt.Errorf("save %s %s: %w", integration, f.Name, err))
			return
		}
	}
	// A blank first field is cleared first, turning the integration off
	// before the rest change.
	order := slices.Clone(sec.Fields)
	if len(order) > 0 && values[order[0].Name] != "" {
		order = append(order[1:], order[0])
	}
	for _, f := range order {
		v, present := values[f.Name]
		if !present {
			continue
		}
		field := store.Field{Integration: integration, Name: f.Name}
		var err error
		if v == "" {
			err = h.Integrations.Store.Clear(r.Context(), field)
		} else {
			err = h.Integrations.Store.SetValue(r.Context(), field, v)
		}
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("save %s %s: %w", integration, f.Name, err))
			return
		}
	}
	h.savedOutcome(w, r)
}

package settings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	store "github.com/kaecyra/gobbler/internal/settings"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// secretsPath is where the secret controls post, below Pattern.
const secretsPath = Pattern + "/secrets"

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
}

// Section is one integration's secrets on the page. An integration registers
// its section when it is wired in (mail, LLM); the page renders whatever is
// registered and nothing else.
type Section struct {
	// Integration is the name in the store, such as "mail".
	Integration string
	// Title is the section heading.
	Title   string
	Secrets []SecretField
}

// Integrations is the integration settings the page shows and edits. With no
// Sections the page shows nothing about integrations.
type Integrations struct {
	Store    SecretStore
	Sections []Section
}

func (i Integrations) field(integration, name string) (store.Field, SecretField, bool) {
	for _, s := range i.Sections {
		if s.Integration != integration {
			continue
		}
		for _, f := range s.Secrets {
			if f.Name == name {
				return store.Field{Integration: integration, Name: name}, f, true
			}
		}
	}
	return store.Field{}, SecretField{}, false
}

// maxFormBytes bounds a posted secret form: the longest value, with room for
// the field name and encoding.
const maxFormBytes = store.MaxValueBytes*3 + 1024

func (h *handler) saveSecret(w http.ResponseWriter, r *http.Request) {
	f, label, ok := h.Integrations.field(chi.URLParam(r, "integration"), chi.URLParam(r, "field"))
	if !ok {
		layout.WriteError(w, r, layout.ErrNotFound)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		h.secretOutcome(w, r, http.StatusBadRequest, f, label, "That value could not be read.")
		return
	}
	value := r.PostForm.Get("value")
	err := h.Integrations.Store.SetSecret(r.Context(), f, value)
	switch {
	case err == nil:
		h.secretOutcome(w, r, http.StatusOK, f, label, "")
	case errors.Is(err, store.ErrNoKey):
		h.secretOutcome(w, r, http.StatusUnprocessableEntity, f, label,
			label.Label+" was not saved: the encryption key is missing. Set the encryption key in the environment and restart.")
	case errors.Is(err, store.ErrInvalid):
		msg := "Enter a value first."
		if value != "" {
			msg = "That value is too long."
		}
		h.secretOutcome(w, r, http.StatusUnprocessableEntity, f, label, msg)
	default:
		layout.WriteError(w, r, fmt.Errorf("save %s %s: %w", f.Integration, f.Name, err))
	}
}

func (h *handler) clearSecret(w http.ResponseWriter, r *http.Request) {
	f, label, ok := h.Integrations.field(chi.URLParam(r, "integration"), chi.URLParam(r, "field"))
	if !ok {
		layout.WriteError(w, r, layout.ErrNotFound)
		return
	}
	if err := h.Integrations.Store.Clear(r.Context(), f); err != nil {
		layout.WriteError(w, r, err)
		return
	}
	h.secretOutcome(w, r, http.StatusOK, f, label, "")
}

// secretOutcome answers a secret request. A browser without script gets the
// whole page back (or a redirect after a success); HTMX gets the integrations
// section. A refusal is a 200 for HTMX, which does not swap a 4xx, and carries
// the message in the section.
func (h *handler) secretOutcome(w http.ResponseWriter, r *http.Request, status int, _ store.Field, _ SecretField, problem string) {
	htmx := r.Header.Get("HX-Request") == "true"
	if problem == "" && !htmx {
		http.Redirect(w, r, Pattern, http.StatusSeeOther)
		return
	}
	v := h.integrationsView(r.Context(), problem)
	if htmx {
		layout.Render(w, r, http.StatusOK, integrationsSection(v))
		return
	}
	layout.Render(w, r, status, settingsPage(h.view(outcomeNone), v))
}

// Secret states as the page words them.
const (
	stateSet        = "Set"
	stateUnset      = "Not set"
	stateUnreadable = "Unreadable. Enter it again."
)

type integrationsView struct {
	// Problem is a refusal to show above the sections.
	Problem  string
	Sections []sectionView
}

type sectionView struct {
	Integration string
	Title       string
	Secrets     []secretView
}

type secretView struct {
	Name  string
	Label string
	State string
	// Stored is true when there is something to clear: set or unreadable.
	Stored bool
	// Action is where the value posts; ClearAction where clearing posts.
	Action, ClearAction string
}

func (h *handler) integrationsView(ctx context.Context, problem string) integrationsView {
	v := integrationsView{Problem: problem}
	if h.Integrations.Store == nil {
		return v
	}
	for _, s := range h.Integrations.Sections {
		sv := sectionView{Integration: s.Integration, Title: s.Title}
		for _, f := range s.Secrets {
			st, err := h.Integrations.Store.SecretState(ctx, store.Field{Integration: s.Integration, Name: f.Name})
			if err != nil {
				// The page still renders; the field shows as not set and
				// the failure is in the log.
				slog.ErrorContext(ctx, "read integration secret state", "integration", s.Integration, "field", f.Name, "error", err.Error())
			}
			sec := secretView{
				Name: f.Name, Label: f.Label, State: stateUnset,
				Action:      secretsPath + "/" + s.Integration + "/" + f.Name,
				ClearAction: secretsPath + "/" + s.Integration + "/" + f.Name + "/clear",
			}
			switch st {
			case store.StateSet:
				sec.State, sec.Stored = stateSet, true
			case store.StateUnreadable:
				sec.State, sec.Stored = stateUnreadable, true
			}
			sv.Secrets = append(sv.Secrets, sec)
		}
		v.Sections = append(v.Sections, sv)
	}
	return v
}

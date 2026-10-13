package nonmember

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Pattern is where the router mounts Routes.
const Pattern = "/not-a-member"

// Routes returns the page's route, relative to Pattern. A household member who
// opens it has no use for it and is sent home.
func Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := household.FromContext(r.Context()); ok {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		render(w, r, http.StatusOK)
	})
	return r
}

// Refuse answers a request from a verified non-member with the page, in place
// and with status 403, so the response says nothing about whether the path
// exists.
func Refuse(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusForbidden)
}

func render(w http.ResponseWriter, r *http.Request, status int) {
	email, _ := auth.EmailFromContext(r.Context())
	layout.Render(w, r, status, page(email))
}

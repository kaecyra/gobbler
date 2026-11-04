package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/oauth"
	"github.com/kaecyra/gobbler/internal/ratelimit"
)

// The paths of the endpoints. The public ones are also listed in package
// layout (layout.IsPublic), which this package's tests check.
const (
	// Pattern is where the OAuth endpoints are mounted.
	Pattern = "/oauth"
	// WellKnownPattern is where the discovery documents are mounted.
	WellKnownPattern = "/.well-known"

	RegisterPath  = Pattern + "/register"
	AuthorizePath = Pattern + "/authorize"
	TokenPath     = Pattern + "/token"

	// ResourceMetadataPath is the Protected Resource Metadata of the /mcp
	// endpoint (RFC 9728 section 3.1: the well-known path with the resource's
	// path appended). The 401 from /mcp points a client here.
	ResourceMetadataPath = WellKnownPattern + "/oauth-protected-resource" + oauth.ResourcePath
	// ServerMetadataPath is the Authorization Server Metadata (RFC 8414).
	ServerMetadataPath = WellKnownPattern + "/oauth-authorization-server"
)

// maxBody bounds a registration or token request body.
const maxBody = 16 << 10

// MetadataSource loads a Client ID Metadata Document. *oauth.MetadataFetcher
// satisfies it; tests supply their own, since the real one refuses to reach a
// test server.
type MetadataSource interface {
	Fetch(ctx context.Context, clientID string) (oauth.Client, error)
}

// Deps is what the endpoints need.
type Deps struct {
	OAuth    *oauth.Store
	Metadata MetadataSource
	// PublicBaseURL is the origin gobbler is reached at, with no trailing slash
	// (config.OIDC.PublicBaseURL). It is the issuer and the root of the
	// resource URL.
	PublicBaseURL string
	// DevBypass lets the base URL come from the request when PublicBaseURL is
	// empty. Only the development bypass, which is loopback only, leaves it
	// empty (ADR-00003).
	DevBypass bool
	Logger    *slog.Logger
	// Register and Token hold each client address to a leaky bucket on client
	// registration and the token endpoint (ADR-00036). Both are required:
	// Routes refuses to build without them, so a public endpoint is never
	// left unlimited by an omission.
	Register, Token *ratelimit.Limiter
}

type handler struct {
	Deps
	log *slog.Logger
}

func newHandler(d Deps) *handler {
	h := &handler{Deps: d, log: d.Logger}
	if h.log == nil {
		h.log = slog.Default()
	}
	return h
}

// Routes returns the endpoints under Pattern.
func Routes(d Deps) (chi.Router, error) {
	if d.Register == nil || d.Token == nil {
		return nil, errors.New("oauth: the registration and token rate limiters are required")
	}
	h := newHandler(d)
	r := chi.NewRouter()
	r.With(perAddress(d.Register, h.log)).Post("/register", h.register)
	r.Get("/authorize", h.authorizeGet)
	r.Post("/authorize", h.authorizePost)
	r.With(perAddress(d.Token, h.log)).Post("/token", h.token)
	return r, nil
}

// perAddress holds each client address to l's bucket (ADR-00036). A refused
// request gets 429 with an OAuth error body (RFC 6749 section 5.2) and the
// Retry-After the limiter set.
func perAddress(l *ratelimit.Limiter, log *slog.Logger) func(http.Handler) http.Handler {
	return l.Middleware(ratelimit.ClientKey, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusTooManyRequests)
		body := errorBody{Error: oauth.CodeTemporarilyBusy, Description: "too many requests from this address; retry later"}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			// The status is sent already, so all that is left is to say so.
			log.WarnContext(r.Context(), "write oauth response", "path", r.URL.Path, "error", err.Error())
		}
	})
}

// WellKnownRoutes returns the discovery documents under WellKnownPattern.
func WellKnownRoutes(d Deps) chi.Router {
	h := newHandler(d)
	r := chi.NewRouter()
	r.Get("/oauth-protected-resource", h.resourceMetadata)
	r.Get("/oauth-protected-resource"+oauth.ResourcePath, h.resourceMetadata)
	r.Get("/oauth-authorization-server", h.serverMetadata)
	return r
}

// baseURL is the origin gobbler is reached at.
func (h *handler) baseURL(r *http.Request) (string, error) {
	return Origin(h.PublicBaseURL, h.DevBypass, r)
}

// ErrNoOrigin is returned by Origin when the origin is not known.
var ErrNoOrigin = errors.New("the public base URL is not configured")

// Origin is the origin gobbler is reached at, with no trailing slash: the
// configured public base URL, or with none, and only under the development
// bypass (loopback only, ADR-00003), the one the request arrived at. The MCP
// endpoint uses it too, so a token is checked against the origin it was issued
// for.
func Origin(publicBaseURL string, devBypass bool, r *http.Request) (string, error) {
	if publicBaseURL != "" {
		return strings.TrimRight(publicBaseURL, "/"), nil
	}
	if devBypass {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		return scheme + "://" + r.Host, nil
	}
	return "", ErrNoOrigin
}

// writeJSON writes v as the response.
func (h *handler) writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status is sent already, so all that is left is to say so.
		h.log.WarnContext(r.Context(), "write oauth response", "path", r.URL.Path, "error", err.Error())
	}
}

type errorBody struct {
	Error       string `json:"error"`
	Description string `json:"error_description,omitempty"`
}

// writeOAuthError answers a JSON endpoint with err as the protocol names it. An
// error that is not an *oauth.Error is a failure of gobbler's own: it is logged
// in full and the client gets nothing but "server_error".
func (h *handler) writeOAuthError(w http.ResponseWriter, r *http.Request, err error) {
	var oe *oauth.Error
	if !errors.As(err, &oe) {
		h.log.ErrorContext(r.Context(), "oauth request failed", "path", r.URL.Path, "error", err.Error())
		h.writeJSON(w, r, http.StatusInternalServerError, errorBody{Error: "server_error", Description: "gobbler could not complete the request"})
		return
	}
	status := http.StatusBadRequest
	switch oe.Code {
	case oauth.CodeInvalidClient:
		status = http.StatusUnauthorized
	case oauth.CodeTemporarilyBusy:
		status = http.StatusServiceUnavailable
	}
	h.log.InfoContext(r.Context(), "oauth request refused", "path", r.URL.Path, "error_code", oe.Code, "reason", oe.Description)
	h.writeJSON(w, r, status, errorBody{Error: oe.Code, Description: oe.Description})
}

// discovery adds the headers of a public, cacheable document that any client,
// a browser-based one included, may read.
func discovery(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Access-Control-Allow-Origin", "*")
}

// resourceMetadata is the Protected Resource Metadata (RFC 9728).
func (h *handler) resourceMetadata(w http.ResponseWriter, r *http.Request) {
	base, err := h.baseURL(r)
	if err != nil {
		h.writeOAuthError(w, r, err)
		return
	}
	discovery(w)
	h.writeJSON(w, r, http.StatusOK, map[string]any{
		"resource":                 oauth.ResourceURL(base),
		"authorization_servers":    []string{base},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "gobbler",
	})
}

// serverMetadata is the Authorization Server Metadata (RFC 8414).
func (h *handler) serverMetadata(w http.ResponseWriter, r *http.Request) {
	base, err := h.baseURL(r)
	if err != nil {
		h.writeOAuthError(w, r, err)
		return
	}
	discovery(w)
	h.writeJSON(w, r, http.StatusOK, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + AuthorizePath,
		"token_endpoint":                        base + TokenPath,
		"registration_endpoint":                 base + RegisterPath,
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"client_id_metadata_document_supported": true,
		// RFC 9207: the redirect carries iss, so a client can tell servers apart.
		"authorization_response_iss_parameter_supported": true,
	})
}

// requireMediaType refuses a request whose body is not of the type.
func requireMediaType(r *http.Request, want string) error {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != want {
		return &oauth.Error{Code: oauth.CodeInvalidRequest, Description: "the request body must be " + want}
	}
	return nil
}

type registration struct {
	ClientName    string   `json:"client_name"`
	RedirectURIs  []string `json:"redirect_uris"`
	AuthMethod    string   `json:"token_endpoint_auth_method"`
	GrantTypes    []string `json:"grant_types"`
	ResponseTypes []string `json:"response_types"`
}

// register is Dynamic Client Registration (RFC 7591). Clients are public: no
// secret is issued.
func (h *handler) register(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if err := requireMediaType(r, "application/json"); err != nil {
		h.writeOAuthError(w, r, err)
		return
	}
	var req registration
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
		h.writeOAuthError(w, r, &oauth.Error{Code: oauth.CodeInvalidClientMetadata, Description: "the body is not a valid client registration"})
		return
	}
	c, err := h.OAuth.RegisterClient(r.Context(), oauth.ClientMetadata{
		Name: req.ClientName, RedirectURIs: req.RedirectURIs, AuthMethod: req.AuthMethod,
		GrantTypes: req.GrantTypes, ResponseTypes: req.ResponseTypes,
	})
	if err != nil {
		h.writeOAuthError(w, r, err)
		return
	}
	h.log.InfoContext(r.Context(), "oauth client registered", "client_id", c.ID)
	h.writeJSON(w, r, http.StatusCreated, map[string]any{
		"client_id":                  c.ID,
		"client_id_issued_at":        c.CreatedAt.Unix(),
		"client_name":                c.Name,
		"redirect_uris":              c.RedirectURIs,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	})
}

// token is the token endpoint: it trades a code, or a refresh token, for
// tokens. The parameters are read from the body alone, never the query string.
func (h *handler) token(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	if err := requireMediaType(r, "application/x-www-form-urlencoded"); err != nil {
		h.writeOAuthError(w, r, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseForm(); err != nil {
		h.writeOAuthError(w, r, &oauth.Error{Code: oauth.CodeInvalidRequest, Description: "the body is not a valid form"})
		return
	}
	form := r.PostForm
	var toks oauth.Tokens
	var err error
	switch gt := form.Get("grant_type"); gt {
	case "authorization_code":
		toks, err = h.OAuth.ExchangeCode(r.Context(), oauth.ExchangeRequest{
			Code: form.Get("code"), ClientID: form.Get("client_id"), RedirectURI: form.Get("redirect_uri"),
			Verifier: form.Get("code_verifier"), Resource: form.Get("resource"),
		})
	case "refresh_token":
		toks, err = h.OAuth.Refresh(r.Context(), oauth.RefreshRequest{
			Token: form.Get("refresh_token"), ClientID: form.Get("client_id"), Resource: form.Get("resource"),
		})
	default:
		err = &oauth.Error{Code: oauth.CodeUnsupportedGrantType, Description: "grant_type must be authorization_code or refresh_token"}
	}
	if err != nil {
		h.writeOAuthError(w, r, err)
		return
	}
	h.log.InfoContext(r.Context(), "oauth tokens issued", "grant_type", form.Get("grant_type"), "client_id", oauth.Truncate(form.Get("client_id"), 120))
	h.writeJSON(w, r, http.StatusOK, map[string]any{
		"access_token":  toks.Access,
		"token_type":    "Bearer",
		"expires_in":    int(toks.ExpiresIn.Seconds()),
		"refresh_token": toks.Refresh,
	})
}

// redirectWith sends the browser back to the client with the parameters added.
func redirectWith(w http.ResponseWriter, r *http.Request, redirectURI string, params url.Values) error {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return fmt.Errorf("parse redirect URI: %w", err)
	}
	q := u.Query()
	for k, v := range params {
		q[k] = v
	}
	u.RawQuery = q.Encode()
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
	return nil
}

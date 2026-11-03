package oauth

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/oauth"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// authRequest is an authorization request that passed every check that can be
// answered without the person.
type authRequest struct {
	Client        oauth.Client
	RedirectURI   string
	CodeChallenge string
	State         string
	Resource      string
	Base          string
}

// pageError is a request that must not be redirected back to the client,
// because the client or the redirect URI is not trustworthy: it is shown to the
// person instead (RFC 6749 section 4.1.2.1).
type pageError struct {
	status  int
	message string
	cause   error
}

func (e *pageError) Error() string { return e.message }
func (e *pageError) Unwrap() error { return e.cause }

// redirectError is a request that can be refused to the client, at its
// redirect URI.
type redirectError struct {
	code, description string
}

func (e *redirectError) Error() string { return e.code + ": " + e.description }

// parseAuthRequest checks an authorization request, from the query on GET and
// the form on POST. The client and the redirect URI come first: until both are
// known to be genuine the response goes to the person, not to a URI the request
// names.
func (h *handler) parseAuthRequest(r *http.Request, v url.Values) (authRequest, error) {
	base, err := h.baseURL(r)
	if err != nil {
		return authRequest{}, err
	}
	clientID := v.Get("client_id")
	if clientID == "" {
		return authRequest{}, &pageError{status: http.StatusBadRequest, message: "This link has no client_id, so gobbler cannot tell which application is asking."}
	}
	client, err := h.client(r, clientID)
	if err != nil {
		return authRequest{}, err
	}
	redirectURI := v.Get("redirect_uri")
	if redirectURI == "" || !client.AllowsRedirect(redirectURI) {
		return authRequest{}, &pageError{status: http.StatusBadRequest, message: "This link asks to return to an address the application did not register, so gobbler stopped."}
	}
	req := authRequest{
		Client: client, RedirectURI: redirectURI, CodeChallenge: v.Get("code_challenge"),
		State: v.Get("state"), Resource: v.Get("resource"), Base: base,
	}
	switch {
	case v.Get("response_type") != "code":
		return req, &redirectError{"unsupported_response_type", "response_type must be code"}
	case req.CodeChallenge == "":
		return req, &redirectError{oauth.CodeInvalidRequest, "code_challenge is required (PKCE)"}
	case v.Get("code_challenge_method") != "S256":
		return req, &redirectError{oauth.CodeInvalidRequest, "code_challenge_method must be S256"}
	case !oauth.ValidChallenge(req.CodeChallenge):
		return req, &redirectError{oauth.CodeInvalidRequest, "code_challenge is not an S256 challenge"}
	case req.Resource == "":
		return req, &redirectError{oauth.CodeInvalidRequest, "resource is required"}
	case req.Resource != oauth.ResourceURL(base):
		return req, &redirectError{oauth.CodeInvalidTarget, "resource must be " + oauth.ResourceURL(base)}
	}
	return req, nil
}

// client resolves a client_id: a Client ID Metadata Document URL is fetched, and
// anything else must be a client that registered.
func (h *handler) client(r *http.Request, clientID string) (oauth.Client, error) {
	if oauth.IsMetadataURL(clientID) {
		c, err := h.Metadata.Fetch(r.Context(), clientID)
		if err != nil {
			// The detail is logged and kept from the page: it can say what
			// gobbler could or could not reach.
			h.log.WarnContext(r.Context(), "oauth client metadata refused", "client_id", truncate(clientID), "error", err.Error())
			return oauth.Client{}, &pageError{status: http.StatusBadRequest, message: "gobbler could not load this application's details, so it stopped.", cause: err}
		}
		return c, nil
	}
	c, err := h.OAuth.Client(r.Context(), clientID)
	switch {
	case errors.Is(err, oauth.ErrUnknownClient):
		return oauth.Client{}, &pageError{status: http.StatusBadRequest, message: "gobbler does not know the application that sent you here.", cause: err}
	case err != nil:
		return oauth.Client{}, err
	}
	return c, nil
}

// fail answers a request that cannot continue.
func (h *handler) fail(w http.ResponseWriter, r *http.Request, req authRequest, err error) {
	var pe *pageError
	var re *redirectError
	switch {
	case errors.As(err, &pe):
		layout.Render(w, r, pe.status, layout.ErrorPage(pe.status, pe.message))
	case errors.As(err, &re):
		params := url.Values{"error": {re.code}, "error_description": {re.description}, "iss": {req.Base}}
		if req.State != "" {
			params.Set("state", req.State)
		}
		if err := redirectWith(w, r, req.RedirectURI, params); err != nil {
			layout.WriteError(w, r, err)
		}
	default:
		layout.WriteError(w, r, err)
	}
}

// noFraming keeps the consent page out of frames, where a page could cover
// the buttons.
func noFraming(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
}

// authorizeGet checks the request and shows the consent page.
func (h *handler) authorizeGet(w http.ResponseWriter, r *http.Request) {
	noFraming(w)
	email, ok := auth.EmailFromContext(r.Context())
	if !ok {
		http.Error(w, "sign-in required", http.StatusUnauthorized)
		return
	}
	v := r.URL.Query()
	req, err := h.parseAuthRequest(r, v)
	if err != nil {
		h.fail(w, r, req, err)
		return
	}
	layout.Render(w, r, http.StatusOK, consentPage(consentView{
		Client: req.Client.Name, ClientID: req.Client.ID, ReturnHost: oauth.ReturnHost(req.RedirectURI), Email: email,
		Fields: map[string]string{
			"response_type": "code", "client_id": req.Client.ID, "redirect_uri": req.RedirectURI,
			"code_challenge": req.CodeChallenge, "code_challenge_method": "S256", "state": req.State, "resource": req.Resource,
		},
		Action:  AuthorizePath,
		Profile: layout.ProfilePath,
	}))
}

// authorizePost takes the person's decision. The form carries the request
// again, and every check runs again on it: nothing about the earlier page is
// trusted.
func (h *handler) authorizePost(w http.ResponseWriter, r *http.Request) {
	noFraming(w)
	email, ok := auth.EmailFromContext(r.Context())
	if !ok {
		http.Error(w, "sign-in required", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseForm(); err != nil {
		layout.Render(w, r, http.StatusBadRequest, layout.ErrorPage(http.StatusBadRequest, "That form could not be read."))
		return
	}
	req, err := h.parseAuthRequest(r, r.PostForm)
	if err != nil {
		h.fail(w, r, req, err)
		return
	}
	if r.PostForm.Get("decision") != "allow" {
		h.fail(w, r, req, &redirectError{"access_denied", "the person did not approve the request"})
		return
	}
	code, err := h.OAuth.IssueCode(r.Context(), oauth.AuthRequest{
		Owner: email, ClientID: req.Client.ID, ClientName: req.Client.Name, RedirectURI: req.RedirectURI,
		CodeChallenge: req.CodeChallenge, Resource: req.Resource,
	})
	if err != nil {
		layout.WriteError(w, r, fmt.Errorf("issue authorization code: %w", err))
		return
	}
	h.log.InfoContext(r.Context(), "oauth request approved", "client_id", truncate(req.Client.ID), "return_host", oauth.ReturnHost(req.RedirectURI))
	params := url.Values{"code": {code}, "iss": {req.Base}}
	if req.State != "" {
		params.Set("state", req.State)
	}
	if err := redirectWith(w, r, req.RedirectURI, params); err != nil {
		layout.WriteError(w, r, err)
	}
}

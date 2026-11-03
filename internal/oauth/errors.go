package oauth

import "errors"

// OAuth error codes (RFC 6749 section 5.2, RFC 7591, RFC 8707).
const (
	CodeInvalidRequest        = "invalid_request"
	CodeInvalidClient         = "invalid_client"
	CodeInvalidGrant          = "invalid_grant"
	CodeInvalidTarget         = "invalid_target"
	CodeInvalidClientMetadata = "invalid_client_metadata"
	CodeInvalidRedirectURI    = "invalid_redirect_uri"
	CodeUnsupportedGrantType  = "unsupported_grant_type"
	CodeTemporarilyBusy       = "temporarily_unavailable"
)

// Error is a refusal that the protocol names. Its Description is written to be
// sent to the client, so it never holds a secret or an internal detail. Two
// Errors are the same for errors.Is when their codes are.
type Error struct {
	Code        string
	Description string
}

func (e *Error) Error() string { return e.Code + ": " + e.Description }

// Is matches on the code alone, so a test can ask for ErrInvalidGrant whatever
// the description.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code
}

// Sentinels for errors.Is.
var (
	ErrInvalidRequest        = &Error{Code: CodeInvalidRequest}
	ErrInvalidClient         = &Error{Code: CodeInvalidClient}
	ErrInvalidGrant          = &Error{Code: CodeInvalidGrant}
	ErrInvalidTarget         = &Error{Code: CodeInvalidTarget}
	ErrInvalidClientMetadata = &Error{Code: CodeInvalidClientMetadata}
	ErrInvalidRedirectURI    = &Error{Code: CodeInvalidRedirectURI}
	ErrUnsupportedGrantType  = &Error{Code: CodeUnsupportedGrantType}
	ErrTemporarilyBusy       = &Error{Code: CodeTemporarilyBusy}
)

// ErrInvalidToken is returned (wrapped) by Verify for an access token that is
// missing, unknown, expired, revoked or issued for another audience. The cases
// are one error, so a probe learns nothing.
var ErrInvalidToken = errors.New("invalid access token")

// ErrNotFound is returned (wrapped) when no grant matches for the caller: it
// does not exist or belongs to someone else.
var ErrNotFound = errors.New("grant not found")

func refuse(code, description string) *Error { return &Error{Code: code, Description: description} }

package layout

import (
	"context"
	"path"
	"slices"
	"strings"

	apihouseholds "github.com/kaecyra/gobbler/internal/api/households"
	"github.com/kaecyra/gobbler/internal/auth"
)

// OperatorPaths are the roots of the operator's pages (ADR-00018,
// Invitations). Admits opens a path under one of them to an operator alone.
// Every operator page sits under /operator (docs/ui/design.md, Operator
// area), so a new one needs no entry here, only one in operatorSections.
// /settings is the deployment page's old address, now only a redirect; it stays
// an operator path so an operator who belongs to no household can follow it.
var OperatorPaths = []string{OperatorPath, "/settings"}

// OperatorPath is the root of the operator area and its overview page.
const OperatorPath = "/operator"

// NotAMemberPath is the page a verified non-member may always open. Package
// nonmember mounts it here.
const NotAMemberPath = "/not-a-member"

// CanadaPath is the public page about where gobbler runs and why (the
// footer's "Canada" links here).
const CanadaPath = "/canada"

// SignInPath starts a sign-in, and CallbackPath is where the issuer sends the
// browser back (ADR-00030). Both are public. Package signin serves them.
const (
	SignInPath   = "/login"
	CallbackPath = "/auth/callback"
)

// SignOutPath ends the session. It is not public, since signing out needs a
// session to end, but every identity may open it, member or not.
const SignOutPath = "/logout"

// ProfilePath is the root of the profile pages (ADR-00032). Every signed-in
// person may open their own profile, member or not, so Admits admits a
// non-member to it and to everything under it. Package profile mounts it here.
// Whose profile a path under it shows is decided by that package, not by
// admission.
const ProfilePath = "/profile"

// publicPages are the exact paths served to a request without an identity
// (ADR-00029): the health check, /canada, and sign-in with its callback
// (ADR-00030); the OAuth endpoints (ADR-00034) are appended by the init below.
// Paths are written out, not imported, for the same reason
// navItems' are (ADR-00012); package web checks that its health route is
// listed.
var publicPages = []string{"/healthz", CanadaPath, SignInPath, CallbackPath}

// staticPrefix is where the embedded assets are served. Every page, the public
// ones and the not-a-member page included, is drawn with them, so they are
// public too, and the only public prefix.
const staticPrefix = "/static/"

// IsPublic reports whether p is a public path (ADR-00029): one of publicPages
// exactly, or under staticPrefix. It matches p as written. The identity
// middleware calls it on the raw path, before any dot-segment check, so on its
// own it is not a lock: "/static/../recipes" is public here. The membership
// middleware is the lock. It refuses dot segments first, and admits a request
// with no identity only when both the decoded and the escaped path are public.
func IsPublic(p string) bool {
	return slices.Contains(publicPages, p) || strings.HasPrefix(p, staticPrefix)
}

// Admits reports whether a request for p may be served, given whether its
// verified identity is a member of a household and whether it is an
// operator's (ADR-00018). It is the one admission rule: package web's
// membership middleware enforces it, and the nav shows only the links it
// admits, so the two cannot disagree.
//
//   - A public path (IsPublic) admits everyone.
//   - An operator page admits an operator, member or not, and nobody else.
//   - Every other page admits a member.
//   - A non-member may also open the not-a-member page, the invitation
//     routes, sign-out and the profile pages.
//
// The caller has refused dot segments, so the prefixes mean what they say.
func Admits(p string, member, operator bool) bool {
	switch {
	case IsPublic(p):
		return true
	case isOperatorPath(p):
		return operator
	case member:
		return true
	default:
		return p == NotAMemberPath || p == SignOutPath || p == ProfilePath ||
			strings.HasPrefix(p, ProfilePath+"/") || strings.HasPrefix(p, InvitationsPath+"/")
	}
}

// isOperatorPath reports whether p, cleaned, is an operator root or lies under
// one. Cleaning only normalises repeated slashes on a path with no dot
// segments.
func isOperatorPath(p string) bool {
	p = path.Clean("/" + p)
	for _, op := range OperatorPaths {
		if p == op || strings.HasPrefix(p, op+"/") {
			return true
		}
	}
	return false
}

type operatorKey struct{}

// WithOperator marks ctx as the request of an operator email, member or not.
// It only decides what the nav shows; the lock is the membership middleware.
func WithOperator(ctx context.Context) context.Context {
	return context.WithValue(ctx, operatorKey{}, true)
}

// IsOperator reports whether ctx was marked by WithOperator.
func IsOperator(ctx context.Context) bool {
	v, _ := ctx.Value(operatorKey{}).(bool)
	return v
}

type signInKey struct{}

// WithSignIn marks ctx as served by a deployment where people sign in to the
// app itself (ADR-00030), so the account menu offers "Sign in" and "Sign out".
// Without it, identity comes from in front of the app and neither is shown.
func WithSignIn(ctx context.Context) context.Context {
	return context.WithValue(ctx, signInKey{}, true)
}

func signInOn(ctx context.Context) bool {
	v, _ := ctx.Value(signInKey{}).(bool)
	return v
}

// offerSignIn reports whether the account menu shows "Sign in": sign-in is on and
// the request carries no identity, which only a public page is served without.
func offerSignIn(ctx context.Context) bool {
	_, signedIn := auth.EmailFromContext(ctx)
	return signInOn(ctx) && !signedIn
}

// offerSignOut reports whether the account menu shows "Sign out": sign-in is on
// and the request carries an identity.
func offerSignOut(ctx context.Context) bool {
	_, signedIn := auth.EmailFromContext(ctx)
	return signInOn(ctx) && signedIn
}

// visible reports whether the nav shows item to the request in ctx: exactly
// when Admits would let that request open it.
func visible(ctx context.Context, item navItem) bool {
	_, member := apihouseholds.FromContext(ctx)
	return Admits(item.Path, member, IsOperator(ctx))
}

// BearerPath is the machine-facing endpoint (ADR-00031, ADR-00034). It is not
// public: it reads household data, so it admits only a member (Admits). It skips
// the session, because a browser cookie never authenticates it, and
// authenticates by bearer credential instead; package web wires that, and
// package mcp serves it.
const BearerPath = "/mcp"

// IsBearerPath reports whether p is the bearer-authenticated endpoint or lies
// under it. It matches p as written, like IsPublic.
func IsBearerPath(p string) bool {
	return p == BearerPath || strings.HasPrefix(p, BearerPath+"/")
}

// oauthPublicPages are the OAuth server's endpoints that are served without an
// identity (ADR-00034, amending ADR-00029): the discovery documents, client
// registration and the token endpoint. A client calls them before any person has
// signed in, and the token endpoint authenticates by the code or refresh token
// it is given. The authorization endpoint, /oauth/authorize, is not here: it
// needs a session. Package web/oauth serves them, and its tests check that each
// of its routes but that one is public.
var oauthPublicPages = []string{
	"/.well-known/oauth-protected-resource",
	"/.well-known/oauth-protected-resource/mcp",
	"/.well-known/oauth-authorization-server",
	"/oauth/register",
	"/oauth/token",
}

// init adds the OAuth endpoints to publicPages. The wave-11 nodes each add
// their own paths in their own init, so none edits another's line.
func init() { publicPages = append(publicPages, oauthPublicPages...) }

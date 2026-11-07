package layout

import (
	"context"

	apihouseholds "github.com/kaecyra/gobbler/internal/api/households"
)

// profileAdmitted reports whether the request may open the profile pages: the
// same Admits rule that shows a nav link, so the account menu offers the link
// exactly when the server would serve it.
func profileAdmitted(ctx context.Context) bool {
	_, member := apihouseholds.FromContext(ctx)
	return Admits(ProfilePath, member, IsOperator(ctx))
}

type avatarSourceKey struct{}

// AvatarSource finds the address of the signed-in person's uploaded avatar, or
// returns "" when they have none, so the header draws the default one.
type AvatarSource func(ctx context.Context) string

// WithAvatarSource tells the header where to find the viewer's avatar. The
// header asks only when it renders, so a request that renders no header, and a
// visitor who is signed out, cost no lookup.
func WithAvatarSource(ctx context.Context, f AvatarSource) context.Context {
	return context.WithValue(ctx, avatarSourceKey{}, f)
}

func avatarSrc(ctx context.Context) string {
	if f, ok := ctx.Value(avatarSourceKey{}).(AvatarSource); ok && f != nil {
		return f(ctx)
	}
	return ""
}

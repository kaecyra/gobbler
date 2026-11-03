package layout

import (
	"context"

	"github.com/kaecyra/gobbler/internal/household"
)

// profileAdmitted reports whether the request may open the profile pages: the
// same Admits rule that shows a nav link, so the account menu offers the link
// exactly when the server would serve it.
func profileAdmitted(ctx context.Context) bool {
	_, member := household.FromContext(ctx)
	return Admits(ProfilePath, member, IsOperator(ctx))
}

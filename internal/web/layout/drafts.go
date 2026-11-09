package layout

import (
	"context"
	"strconv"
)

type waitingDraftsKey struct{}

// WaitingDrafts counts the signed-in member's household's drafts waiting for
// review, for the badge on the import entry of the nav (ADR-00049). It returns
// 0 when there are none, or when the count cannot be read.
type WaitingDrafts func(ctx context.Context) int

// WithWaitingDrafts tells the nav how to count the drafts waiting for review.
// The nav counts only when it renders an entry that shows the count, so a
// fragment, a redirect and a page served to a non-member cost no query.
func WithWaitingDrafts(ctx context.Context, f WaitingDrafts) context.Context {
	return context.WithValue(ctx, waitingDraftsKey{}, f)
}

// waitingCount is the badge count for item: the drafts waiting for review on
// the entry that shows them, and 0 on every other entry.
func waitingCount(ctx context.Context, item navItem) int {
	if !item.Waiting {
		return 0
	}
	if f, ok := ctx.Value(waitingDraftsKey{}).(WaitingDrafts); ok && f != nil {
		return f(ctx)
	}
	return 0
}

// navLabel is item's accessible name, with the count of drafts waiting when
// its badge shows one.
func navLabel(item navItem, waiting int) string {
	switch {
	case waiting == 1:
		return item.Label + ", 1 draft waiting for review"
	case waiting > 1:
		return item.Label + ", " + strconv.Itoa(waiting) + " drafts waiting for review"
	}
	return item.Label
}

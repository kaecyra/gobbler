package household

import (
	"context"
	"errors"
	"fmt"
)

// Seed makes email a member of a household called name, creating the
// household when the email has none, and does nothing when it already belongs
// to one. It exists so the dev bypass and the end-to-end suite have members to
// act as; production households start from an operator's invitation.
func (s *Store) Seed(ctx context.Context, name, email string) error {
	_, err := s.ForEmail(ctx, email)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	if _, err := s.Create(ctx, name, email); err != nil {
		return fmt.Errorf("seed household %q: %w", name, err)
	}
	return nil
}

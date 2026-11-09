package household

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/kaecyra/gobbler/internal/household/householddb"
)

// MaxHouseStyleBytes is the longest house style, in bytes of UTF-8 (ADR-00048).
const MaxHouseStyleBytes = 16 << 10

// HouseStyle returns the household's house style (ADR-00048): plain text that
// tells an agent how the household shapes its recipes, empty when none has
// been written. A household that does not exist is ErrNotFound.
func (s *Store) HouseStyle(ctx context.Context, householdID int64) (string, error) {
	style, err := s.q.GetHouseStyle(ctx, householdID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: household %d", ErrNotFound, householdID)
	}
	if err != nil {
		return "", fmt.Errorf("read the house style of household %d: %w", householdID, err)
	}
	return style, nil
}

// SetHouseStyle replaces the household's house style and changes nothing else
// about it. A browser sends a textarea's line breaks as CRLF, so they are
// stored as LF, and the bound is measured after that. A style longer than
// MaxHouseStyleBytes is ErrInvalid, with a message for the person who typed it.
func (s *Store) SetHouseStyle(ctx context.Context, householdID int64, style string) error {
	style = strings.ReplaceAll(style, "\r\n", "\n")
	if len(style) > MaxHouseStyleBytes {
		return fmt.Errorf("%w: the house style can be at most %d KiB, and this one is %d bytes over",
			ErrInvalid, MaxHouseStyleBytes>>10, len(style)-MaxHouseStyleBytes)
	}
	n, err := s.q.SetHouseStyle(ctx, householddb.SetHouseStyleParams{HouseStyle: style, ID: householdID})
	if err != nil {
		return fmt.Errorf("save the house style of household %d: %w", householdID, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: household %d", ErrNotFound, householdID)
	}
	return nil
}

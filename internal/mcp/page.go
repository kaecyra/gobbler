package mcp

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// Paging (ADR-00034): a tool that returns a list returns one page and a cursor
// for the next. The cursor is opaque to the client.
const (
	// DefaultPageSize is used when the client asks for no size.
	DefaultPageSize = 25
	// MaxPageSize is the most any page holds, whatever the client asks.
	MaxPageSize = 50
)

// Page is one page of a list.
type Page[T any] struct {
	Items []T `json:"items"`
	// NextCursor is empty on the last page; pass it as cursor for the next.
	NextCursor string `json:"next_cursor,omitempty"`
}

// paginate returns the page of all that cursor names, at most limit items
// (clamped to MaxPageSize). A cursor that is not one this package issued is an
// error the client can fix.
func paginate[T any](all []T, cursor string, limit int) (Page[T], error) {
	size := limit
	if size <= 0 {
		size = DefaultPageSize
	}
	size = min(size, MaxPageSize)
	start := 0
	if cursor != "" {
		var err error
		if start, err = decodeCursor(cursor); err != nil {
			return Page[T]{}, err
		}
	}
	if start > len(all) {
		start = len(all)
	}
	end := min(start+size, len(all))
	p := Page[T]{Items: append([]T{}, all[start:end]...)}
	if end < len(all) {
		p.NextCursor = base64.RawURLEncoding.EncodeToString([]byte("o:" + strconv.Itoa(end)))
	}
	return p, nil
}

// decodeCursor returns the offset a cursor from paginate names.
func decodeCursor(cursor string) (int, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	n, ok := strings.CutPrefix(string(raw), "o:")
	if err == nil && ok {
		if v, convErr := strconv.Atoi(n); convErr == nil && v >= 0 {
			return v, nil
		}
	}
	return 0, fmt.Errorf("invalid cursor %q: pass the next_cursor of the previous page", cursor)
}

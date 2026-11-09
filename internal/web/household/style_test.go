package household

import (
	"context"
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// The house style field on the household page (ADR-00048).

func TestAMemberSavesTheHouseStyleOnTheHouseholdPage(t *testing.T) {
	s := newSite(t)
	s.household(t, "Alpha", "ann@example.com", "abe@example.com")
	s.household(t, "Beta", "bo@example.com")

	// The second member, not only the founder, may save it.
	const style = "Weights in grams.\r\nOne step per action & no \"to taste\"."
	rec := s.do(http.MethodPost, "/household/style", "abe@example.com", url.Values{"style": {style}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != Pattern {
		t.Fatalf("save = %d to %q, want 303 to %s:\n%s", rec.Code, rec.Header().Get("Location"), Pattern, rec.Body)
	}

	stored := strings.ReplaceAll(style, "\r\n", "\n")
	h, err := s.hs.ForEmail(context.Background(), "ann@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.hs.HouseStyle(context.Background(), h.ID); err != nil || got != stored {
		t.Fatalf("stored style = %q, %v, want %q", got, err, stored)
	}

	page := s.do(http.MethodGet, "/household", "ann@example.com", nil)
	if page.Code != http.StatusOK || !strings.Contains(html.UnescapeString(page.Body.String()), stored) {
		t.Fatalf("page does not show the saved style (%d):\n%s", page.Code, page.Body)
	}
	other := s.do(http.MethodGet, "/household", "bo@example.com", nil)
	if strings.Contains(html.UnescapeString(other.Body.String()), "Weights in grams.") {
		t.Fatal("another household's page shows the style")
	}
}

// textareaValue is the value a browser gives the style field of page: the
// HTML parser drops a line break straight after <textarea>.
func textareaValue(t *testing.T, page string) string {
	t.Helper()
	doc, err := xhtml.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	var find func(*xhtml.Node) (string, bool)
	find = func(n *xhtml.Node) (string, bool) {
		if n.Type == xhtml.ElementNode && n.Data == "textarea" {
			for _, a := range n.Attr {
				if a.Key == "name" && a.Val == "style" {
					var b strings.Builder
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						b.WriteString(c.Data)
					}
					return b.String(), true
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if v, ok := find(c); ok {
				return v, true
			}
		}
		return "", false
	}
	v, ok := find(doc)
	if !ok {
		t.Fatalf("no style field on the page:\n%s", page)
	}
	return v
}

func TestAHouseStyleThatStartsWithABlankLineKeepsItWhenSavedAgain(t *testing.T) {
	s := newSite(t)
	s.household(t, "Alpha", "ann@example.com")
	const style = "\nWeights in grams."
	// Each round saves what the field showed after the last, as a person who
	// presses Save without editing does.
	value := style
	for round := range 2 {
		if rec := s.do(http.MethodPost, "/household/style", "ann@example.com", url.Values{"style": {value}}); rec.Code != http.StatusSeeOther {
			t.Fatalf("save %d = %d", round, rec.Code)
		}
		page := s.do(http.MethodGet, "/household", "ann@example.com", nil)
		if value = textareaValue(t, page.Body.String()); value != style {
			t.Fatalf("round %d: field shows %q, want %q", round, value, style)
		}
	}
}

func TestAHouseStyleOver16KiBIsRefusedOnTheHouseholdPage(t *testing.T) {
	s := newSite(t)
	s.household(t, "Alpha", "ann@example.com")
	if rec := s.do(http.MethodPost, "/household/style", "ann@example.com", url.Values{"style": {"Keep this."}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("first save = %d", rec.Code)
	}
	rec := s.do(http.MethodPost, "/household/style", "ann@example.com", url.Values{"style": {strings.Repeat("a", 16385)}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "at most 16 KiB") {
		t.Fatalf("over-limit save = %d, want 400 with the reason:\n%s", rec.Code, rec.Body)
	}
	h, err := s.hs.ForEmail(context.Background(), "ann@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.hs.HouseStyle(context.Background(), h.ID); err != nil || got != "Keep this." {
		t.Fatalf("style after a refused save = %q, %v", got, err)
	}
}

func TestANonMemberCannotSaveAHouseStyle(t *testing.T) {
	s := newSite(t)
	s.household(t, "Alpha", "ann@example.com")
	rec := s.do(http.MethodPost, "/household/style", "stranger@example.com", url.Values{"style": {"Mine now."}})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-member save = %d, want 403", rec.Code)
	}
	h, err := s.hs.ForEmail(context.Background(), "ann@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.hs.HouseStyle(context.Background(), h.ID); err != nil || got != "" {
		t.Fatalf("style after a non-member's save = %q, %v", got, err)
	}
}

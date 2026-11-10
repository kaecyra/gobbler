package layout

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apihouseholds "github.com/kaecyra/gobbler/internal/api/households"
	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
)

// #613: an error page carries its own heading and mascot, never the bare
// status line or a sentinel's raw text.
func TestWriteErrorShowsEachErrorsOwnPage(t *testing.T) {
	cases := []struct {
		name, path string
		err        error
		status     int
		want       []string
		notWant    []string
	}{
		{"generic not found", "/shopping/9", fmt.Errorf("list 9: %w", ErrNotFound), 404,
			[]string{"We couldn't find that page", "Error 404", MascotTasting, `href="/recipes"`, ">Home</a>"}, []string{">not found<", "Not Found"}},
		{"recipe not found", "/recipes/9", fmt.Errorf("recipe 9: %w", apirecipes.ErrNotFound), 404,
			[]string{"That recipe isn't in your household", MascotMixingBowl}, nil},
		{"recipe sentinel off the recipe pages", "/ingest/abc", apirecipes.ErrNotFound, 404,
			[]string{"We couldn't find that page"}, []string{"That recipe"}},
		{"expired invitation", "/invitations/x", apihouseholds.ErrExpired, 410,
			[]string{"This invitation has expired", "Invitation", MascotHerbs, "send a new link"}, []string{"Error 410"}},
		{"used invitation", "/invitations/x", apihouseholds.ErrUsed, 410, []string{"This invitation has already been used"}, nil},
		{"withdrawn invitation", "/invitations/x", apihouseholds.ErrRevoked, 410, []string{"This invitation was withdrawn"}, nil},
		{"conflict keeps its reason as a sentence", "/household/leave", apihouseholds.ErrAlreadyMember, 409,
			[]string{"We couldn't do that", "Already a member of a household; leave the current household first."}, nil},
		{"unmapped is a 500 that leaks nothing", "/recipes", errors.New("database is locked"), 500,
			[]string{"Something went wrong on our side", "It's been logged", MascotWink}, []string{"database is locked"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			WriteError(rec, httptest.NewRequest(http.MethodGet, c.path, nil), c.err)
			if rec.Code != c.status {
				t.Fatalf("status = %d, want %d", rec.Code, c.status)
			}
			body := strings.ReplaceAll(rec.Body.String(), "&#39;", "'")
			for _, w := range c.want {
				if !strings.Contains(body, w) {
					t.Errorf("page lacks %q", w)
				}
			}
			for _, w := range c.notWant {
				if strings.Contains(body, w) {
					t.Errorf("page shows %q", w)
				}
			}
		})
	}
}

func TestErrorPageKeepsACallersMessage(t *testing.T) {
	var buf bytes.Buffer
	if err := ErrorPage(http.StatusForbidden, "This page is for the operator.").Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	body := strings.ReplaceAll(buf.String(), "&#39;", "'")
	for _, w := range []string{"This page isn't open to you", "This page is for the operator.", "Error 403"} {
		if !strings.Contains(body, w) {
			t.Errorf("page lacks %q", w)
		}
	}
}

func TestErrorPageSignOutIsAForm(t *testing.T) {
	var buf bytes.Buffer
	v := ErrorView{Status: 404, Mascot: MascotWink, Heading: "h", Actions: []ErrorAction{{Label: "Sign out", Primary: true, SignOut: true}}}
	if err := ErrorPageView(v).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `<form method="post" action="`+SignOutPath+`"><button type="submit" class="btn primary">Sign out</button>`) {
		t.Errorf("sign-out action is not a post form:\n%s", buf.String())
	}
}

func TestSentence(t *testing.T) {
	for in, want := range map[string]string{
		"invitation has expired": "Invitation has expired.",
		"Already done.":          "Already done.",
		"really?":                "Really?",
		"":                       "",
		"élan":                   "Élan.",
	} {
		if got := sentence(in); got != want {
			t.Errorf("sentence(%q) = %q, want %q", in, got, want)
		}
	}
}

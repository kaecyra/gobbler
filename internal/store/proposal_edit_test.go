package store

import (
	"testing"

	"github.com/kaecyra/gobbler/internal/credential"
)

func TestDescribeNamesAnAPIKeyByItsOwner(t *testing.T) {
	p := Provenance{Person: "ann@example.com", Credential: &Credential{Kind: credential.KindAPIKey, Ref: "gobk_ab12"}}
	if got, want := p.Describe(), "API key gobk_ab12 (ann@example.com)"; got != want {
		t.Errorf("Describe() = %q, want %q", got, want)
	}
	// A household key acts for no one person: no owner is shown.
	p.Person = "household:3"
	if got, want := p.Describe(), "API key gobk_ab12"; got != want {
		t.Errorf("household key: Describe() = %q, want %q", got, want)
	}
}

func TestDescribeNamesAnOAuthGrantByItsClient(t *testing.T) {
	p := Provenance{Person: "ann@example.com", Credential: &Credential{Kind: credential.KindOAuth, Ref: "client-1"}}
	if got, want := p.Describe(), "OAuth client client-1 (ann@example.com)"; got != want {
		t.Errorf("Describe() = %q, want %q", got, want)
	}
}

package store

import (
	"errors"
	"testing"

	"github.com/kaecyra/gobbler/internal/recipe"
)

// ADR-00035 obligation 10: accepting a proposal writes a revision with change
// kind "proposal" that refers to the proposal, names the accepting person, and
// leaves the proposal accepted.
func TestAcceptEditWritesAProposalRevision(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id := newRecipe(ctx, t, s, ids.salt)
	before, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	edit := editOf(ctx, t, s, id, func(r *recipe.Recipe) { r.Name = "Less salty" })
	token, err := s.ProposeEdit(ctx, id, 1, edit, agent)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AcceptEdit(ctx, token, 1, edit); err != nil {
		t.Fatal(err)
	}
	var kind string
	var proposal int64
	if err := d.QueryRow(`SELECT change_kind, proposal_id FROM revisions WHERE recipe_id = ? AND version = 1`, id).Scan(&kind, &proposal); err != nil {
		t.Fatal(err)
	}
	if kind != "proposal" {
		t.Errorf("change kind = %q, want proposal", kind)
	}
	var tok string
	if err := d.QueryRow(`SELECT token FROM proposals WHERE id = ?`, proposal).Scan(&tok); err != nil || tok != token {
		t.Errorf("revision refers to proposal %q (%v), want %q", tok, err, token)
	}
	revs, err := s.ListRevisions(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 1 || revs[0].Author != "dev@localhost" || revs[0].Proposal == nil {
		t.Fatalf("revisions = %+v, want one by the accepting person with its proposal", revs)
	}
	if p := revs[0].Proposal; p.Reason != "less salt" || p.Credential == nil || p.Credential.Ref != "gobk_ab12" || p.Token != token {
		t.Errorf("revision proposal = %+v, want the reason and credential of the proposal", p)
	}
	got, err := s.GetProposal(ctx, token)
	if err != nil || got.State != ProposalAccepted {
		t.Errorf("proposal = %+v (%v), want accepted", got, err)
	}
	if cur, _ := s.Get(ctx, id); cur.Name != "Less salty" {
		t.Errorf("recipe name = %q, want the accepted edit", cur.Name)
	}
	// The revision holds the version that was replaced.
	old, err := s.GetVersion(ctx, id, 1)
	if err != nil || old.Name != before.Name {
		t.Errorf("version 1 = %q (%v), want %q", old.Name, err, before.Name)
	}
}

// ADR-00035 obligation 11: restoring the revision a proposal wrote round-trips
// the full recipe.
func TestRestoreOfAProposalRevisionRoundTrips(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id := newRecipe(ctx, t, s, ids.salt)
	original, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	edit := editOf(ctx, t, s, id, func(r *recipe.Recipe) {
		r.Name = "Other"
		r.Components[0].Steps = append(r.Components[0].Steps, recipe.Step{ID: "n", Text: "Bake."})
	})
	token, err := s.ProposeEdit(ctx, id, 1, edit, agent)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AcceptEdit(ctx, token, 1, edit); err != nil {
		t.Fatal(err)
	}
	if err := s.Restore(ctx, id, 1); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	sameRecipe(t, original, got)
}

// ADR-00035 obligation 9, store side: a proposal made against a version the
// recipe has left cannot be accepted, and a review save that races an ordinary
// save is refused with the recipe unchanged.
func TestAcceptEditRefusesAStaleProposal(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id := newRecipe(ctx, t, s, ids.salt)
	edit := editOf(ctx, t, s, id, func(r *recipe.Recipe) { r.Name = "Agent's name" })
	token, err := s.ProposeEdit(ctx, id, 1, edit, agent)
	if err != nil {
		t.Fatal(err)
	}
	person := editOf(ctx, t, s, id, func(r *recipe.Recipe) { r.Name = "Person's name" })
	if err := s.UpdateIfVersion(ctx, id, 1, person); err != nil {
		t.Fatal(err)
	}
	p, err := s.GetProposal(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if info, _ := s.Info(ctx, id); !p.StaleAt(info.Version) {
		t.Errorf("proposal at base %d is not stale at version %d", p.BaseVersion, info.Version)
	}
	err = s.AcceptEdit(ctx, token, 2, edit)
	if !errors.Is(err, ErrStaleProposal) || !errors.Is(err, ErrStaleVersion) {
		t.Fatalf("AcceptEdit on a stale proposal = %v, want ErrStaleProposal (an ErrStaleVersion)", err)
	}
	if cur, _ := s.Get(ctx, id); cur.Name != "Person's name" {
		t.Errorf("recipe name = %q, want the person's edit untouched", cur.Name)
	}
	if got, _ := s.GetProposal(ctx, token); got.State != ProposalPending {
		t.Errorf("state = %q, want pending: a refused accept changes nothing", got.State)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM revisions WHERE change_kind = 'proposal'`); n != 0 {
		t.Errorf("%d proposal revisions written by a refused accept", n)
	}
}

func TestAcceptEditRefusesAFormLoadedAtAnotherVersionAndAFinishedProposal(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id := newRecipe(ctx, t, s, ids.salt)
	edit := editOf(ctx, t, s, id, func(r *recipe.Recipe) { r.Name = "N" })
	token, err := s.ProposeEdit(ctx, id, 1, edit, agent)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AcceptEdit(ctx, token, 5, edit); !errors.Is(err, ErrStaleVersion) {
		t.Errorf("AcceptEdit with a form at version 5 = %v, want ErrStaleVersion", err)
	}
	if err := s.AcceptEdit(otherCtx(t), token, 1, edit); !errors.Is(err, ErrNotFound) {
		t.Errorf("AcceptEdit from another household = %v, want ErrNotFound", err)
	}
	if err := s.RejectProposal(ctx, token); err != nil {
		t.Fatal(err)
	}
	if err := s.AcceptEdit(ctx, token, 1, edit); !errors.Is(err, ErrNotPending) {
		t.Errorf("AcceptEdit of a rejected proposal = %v, want ErrNotPending", err)
	}
}

// Deleting a recipe removes its accepted proposals and the revisions that refer
// to them in one statement (ADR-00035, History).
func TestDeleteRecipeWithAnAcceptedProposal(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id := newRecipe(ctx, t, s, ids.salt)
	edit := editOf(ctx, t, s, id, func(r *recipe.Recipe) { r.Name = "N" })
	token, err := s.ProposeEdit(ctx, id, 1, edit, agent)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AcceptEdit(ctx, token, 1, edit); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM proposals`) + count(t, d, `SELECT COUNT(*) FROM revisions`); n != 0 {
		t.Errorf("%d proposal or revision rows left after deleting the recipe", n)
	}
}

func TestProvenanceDescribe(t *testing.T) {
	key := &Credential{Kind: "api_key", Ref: "gobk_ab12"}
	for name, c := range map[string]struct {
		p    Provenance
		want string
	}{
		"person":    {Provenance{Person: "ann@example.com"}, "ann@example.com"},
		"key":       {Provenance{Person: "ann@example.com", Credential: key}, "API key gobk_ab12 (ann@example.com)"},
		"household": {Provenance{Person: "household:3", Credential: key}, "API key gobk_ab12"},
		"oauth":     {Provenance{Person: "ann@example.com", Credential: &Credential{Kind: "oauth_grant", Ref: "client-1"}}, "OAuth client client-1 (ann@example.com)"},
	} {
		if got := c.p.Describe(); got != c.want {
			t.Errorf("%s: Describe() = %q, want %q", name, got, c.want)
		}
	}
}

// A credential recorded without a creation time must not make its proposal
// unreadable: it was stored as empty text, and reading it back parsed the empty
// text as a time.
func TestProposalByACredentialWithNoCreationTimeLoads(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id := newRecipe(ctx, t, s, ids.salt)
	by := Provenance{Person: "ann@example.com", Reason: "r", Credential: &Credential{Kind: "api_key", ID: 1, Ref: "gobk_x"}}
	edit := editOf(ctx, t, s, id, func(r *recipe.Recipe) { r.Name = "N" })
	token, err := s.ProposeEdit(ctx, id, 1, edit, by)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.GetProposal(ctx, token)
	if err != nil || p.Credential == nil || !p.Credential.CreatedAt.IsZero() {
		t.Fatalf("GetProposal = %+v (%v), want the credential with no creation time", p.Credential, err)
	}
	if err := s.AcceptEdit(ctx, token, 1, edit); err != nil {
		t.Fatal(err)
	}
	revs, err := s.ListRevisions(ctx, id)
	if err != nil || len(revs) != 1 || revs[0].Proposal == nil || revs[0].Proposal.Credential == nil {
		t.Fatalf("ListRevisions = %+v (%v), want the revision to carry the credential", revs, err)
	}
}

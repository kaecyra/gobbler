package recipeview

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/recipe"
)

// doughAndSauce is the canonical meanwhile case: while the dough rises, the
// sauce can be started. Step numbers: 1 mix, 2 rise, 3 bake, 4 chop <tomatoes>,
// 5 simmer, 6 serve.
func doughAndSauce() recipe.Recipe {
	return recipe.Recipe{
		Name: "Pizza",
		Components: []recipe.Component{
			{Name: "Dough", Steps: []recipe.Step{
				{ID: "mix", Text: "Mix the dough.", Duration: 10 * time.Minute},
				{ID: "rise", Text: "Let it rise.", Duration: time.Hour, Passive: true, DependsOn: []recipe.StepID{"mix"}},
				{ID: "bake", Text: "Bake.", Duration: 15 * time.Minute, DependsOn: []recipe.StepID{"rise"}},
			}},
			{Name: "Sauce", Steps: []recipe.Step{
				{ID: "chop", Text: "Chop <tomatoes>.", Duration: 5 * time.Minute},
				{ID: "simmer", Text: "Simmer the sauce.", Duration: 20 * time.Minute, DependsOn: []recipe.StepID{"chop"}},
			}},
			{Name: "Finish", Steps: []recipe.Step{
				{ID: "serve", Text: "Top and serve.", Duration: time.Minute, DependsOn: []recipe.StepID{"bake", "simmer"}},
			}},
		},
	}
}

func TestViewShowsMeanwhileNextToPassiveStep(t *testing.T) {
	code, body := serve(t, fakeRecipes{3: doughAndSauce()}, "/recipes/3")
	if code != http.StatusOK {
		t.Fatalf("status = %d\n%s", code, body)
	}
	mustContain(t, body,
		`data-testid="meanwhile"`,
		"Meanwhile",
		"step 4",
		"Chop &lt;tomatoes&gt;.", // escaped like all recipe text
	)
	// Only the passive step carries guidance, and only ready steps are offered.
	if n := strings.Count(body, `data-testid="meanwhile"`); n != 1 {
		t.Errorf("guidance blocks = %d, want 1\n%s", n, body)
	}
	i := strings.Index(body, `data-testid="meanwhile"`)
	if i < 0 {
		t.FailNow()
	}
	if before := body[:i]; !strings.Contains(before[strings.LastIndex(before, "<li"):], "Let it rise.") {
		t.Errorf("guidance is not inside the passive step's item\n%s", body)
	}
	block := body[i:]
	block = block[:strings.Index(block, "</ul>")]
	if strings.Contains(block, "Simmer the sauce.") {
		t.Errorf("simmer waits for chop and must not be offered\n%s", block)
	}
}

func TestViewWithoutGuidanceIsUnchanged(t *testing.T) {
	// testRecipe has a passive step but nothing that can be done during it.
	code, body := serve(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7")
	if code != http.StatusOK {
		t.Fatalf("status = %d\n%s", code, body)
	}
	mustNotContain(t, body, "Meanwhile", "meanwhile")
	mustContain(t, body, "Bake until golden.", "Timer · 45 min")
}

func TestViewMeanwhileSurvivesScaling(t *testing.T) {
	_, body := serve(t, fakeRecipes{3: doughAndSauce()}, "/recipes/3?scale=2")
	mustContain(t, body, `data-testid="meanwhile"`, "step 4")
}

func TestViewWithBrokenStepGraphFailsLoudly(t *testing.T) {
	r := doughAndSauce()
	r.Times.Total = time.Hour // a stored total keeps TotalTime from touching the graph
	r.Components[1].Steps[0].DependsOn = []recipe.StepID{"simmer"}
	code, body := serve(t, fakeRecipes{3: r}, "/recipes/3")
	if code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500\n%s", code, body)
	}
}

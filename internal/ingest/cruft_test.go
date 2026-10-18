package ingest

import "testing"

func TestCleanDescription(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"empty":              {"", ""},
		"plain":              {"A crisp cutlet. Serve hot.", "A crisp cutlet. Serve hot."},
		"video call":         {"Crispy chicken. (Watch the video)", "Crispy chicken."},
		"video fused":        {"Crispy. (Watch the video)Cook Time assumes two batches.", "Crispy. Cook Time assumes two batches."},
		"video call 2":       {"Great soup (see my how-to video) for winter.", "Great soup for winter."},
		"cooking info stays": {"Cook Time assumes chicken is cooked in two batches.", "Cook Time assumes chicken is cooked in two batches."},
		"fused spacing":      {"Fry in batches.Drain well!Serve now?Yes.", "Fry in batches. Drain well! Serve now? Yes."},
		"abbreviation":       {"Use 2 tbsp.Salt is optional.", "Use 2 tbsp.Salt is optional."},
		"decimal and url":    {"Bake at 1.5 hours; see example.com.Fine", "Bake at 1.5 hours; see example.com.Fine"},
		"initials":           {"By J.R.Smith, with love.", "By J.R.Smith, with love."},
		"section pointer": {
			"Tasty. Don't forget to see the section 'MEAL IDEAS' below the recipe card! It gives you a list of dishes. Enjoy with rice.",
			"Tasty. Enjoy with rice."},
		"curly apostrophe":                       {"Tasty. Don’t forget to see the section below! Done.", "Tasty. Done."},
		"scroll down":                            {"Easy. Scroll down for the recipe. Quick.", "Easy. Quick."},
		"jump":                                   {"Easy. Jump to recipe. Quick.", "Easy. Quick."},
		"pin":                                    {"Easy. Pin this for later! Quick.", "Easy. Quick."},
		"subscribe":                              {"Easy. Subscribe to my channel. Quick.", "Easy. Quick."},
		"newsletter":                             {"Easy. Sign up for my newsletter! Quick.", "Easy. Quick."},
		"sign-off":                               {"Easy. I hope it is of help to you.", "Easy."},
		"sign-off enjoy":                         {"Easy. I hope you enjoy this recipe!", "Easy."},
		"it after non-pointer stays":             {"Easy. I hope you enjoy it. It keeps for days.", "Easy. It keeps for days."},
		"dont forget cooking":                    {"Easy. Don't forget to salt the water. Quick.", "Easy. Don't forget to salt the water. Quick."},
		"see keeps cooking":                      {"Easy. See the steam rise from the pot. Quick.", "Easy. See the steam rise from the pot. Quick."},
		"pin as cooking":                         {"Roll the chicken and pin it closed with a toothpick.", "Roll the chicken and pin it closed with a toothpick."},
		"pin in place":                           {"Pin it in place with a skewer. Fry.", "Pin it in place with a skewer. Fry."},
		"pin this to the pan":                    {"Pin this flat against the pan.", "Pin this flat against the pan."},
		"pin for later":                          {"Easy. Pin it for later! Quick.", "Easy. Quick."},
		"pin recipe":                             {"Easy. Pin this recipe to your dinner board. Quick.", "Easy. Quick."},
		"pinterest":                              {"Easy. Find me on Pinterest. Quick.", "Easy. Quick."},
		"above as cooking":                       {"Check the oil is above 350F. Fry.", "Check the oil is above 350F. Fry."},
		"below as cooking":                       {"See the thermometer read below 165F. Rest.", "See the thermometer read below 165F. Rest."},
		"see below":                              {"Easy. See below for the recipe. Quick.", "Easy. Quick."},
		"curly apostrophe kept":                  {"It\u2019s a cook\u2019s favourite. Don\u2019t overcook.", "It\u2019s a cook\u2019s favourite. Don\u2019t overcook."},
		"it after section pointer keeps cooking": {"Easy. See the section below. It keeps 3 days.", "Easy. It keeps 3 days."},
		"it after scroll keeps cooking":          {"Easy. Scroll down for the recipe. It keeps 3 days. This freezes well.", "Easy. It keeps 3 days. This freezes well."},
		"it describing the page is dropped":      {"Easy. Scroll down for the recipe. It has a list of dishes for a full meal. Quick.", "Easy. Quick."},
		"pin it to cooking":                      {"Pin it to the roast with a skewer.", "Pin it to the roast with a skewer."},
		"pin to board":                           {"Easy. Pin it to your dinner board! Quick.", "Easy. Quick."},
		"all cruft":                              {"(Watch the video) Scroll down for the recipe.", ""},
		"recipetineats": {
			"This is how Japanese people make a Chicken Cutlet. Pour over fruity tonkatsu sauce to eat it. (Watch the video)Cook Time assumes chicken is cooked in two batches.Don't forget to see the section 'MEAL IDEAS' below the recipe card! It gives you a list of dishes that I have already posted and this recipe that can make up a complete meal. I hope it is of help to you.",
			"This is how Japanese people make a Chicken Cutlet. Pour over fruity tonkatsu sauce to eat it. Cook Time assumes chicken is cooked in two batches."},
	} {
		t.Run(name, func(t *testing.T) {
			if got := cleanDescription(tc.in); got != tc.want {
				t.Errorf("cleanDescription(%q)\n got %q\nwant %q", tc.in, got, tc.want)
			}
		})
	}
}

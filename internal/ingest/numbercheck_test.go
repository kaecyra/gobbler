package ingest

import "testing"

// The number check (ADR-00053) lists the numbers a transcribed line states that
// the tesseract text never read. Numbers are compared by value, so the same
// quantity written another way still agrees.
func TestMissingNumbers(t *testing.T) {
	cases := []struct {
		name      string
		line      string
		tesseract string
		want      []string
	}{
		{"same text", "2 cups flour", "2 cups flour", nil},
		{"unicode fraction against slash", "½ cup sugar", "1/2 cup sugar", nil},
		{"slash against unicode fraction", "1/2 cup sugar", "½ cup sugar", nil},
		{"fraction against decimal", "½ cup sugar", "0.5 cup sugar", nil},
		{"fraction misread as a digit", "½ cup sugar", "4 cup sugar", []string{"1/2"}},
		{"mixed number joined", "1½ cups milk", "1 1/2 cups milk", nil},
		{"mixed number misjoined", "1 1/2 cups milk", "11/2 cups milk", []string{"1", "1/2"}},
		{"range both ends", "2-3 cloves garlic", "2-3 cloves garlic", nil},
		{"range with an en dash", "2–3 cloves garlic", "2 to 3 cloves garlic", nil},
		{"range end misread", "2-3 cloves garlic", "2-8 cloves garlic", []string{"3"}},
		{"temperature", "Bake at 350°F", "Bake at 350 F", nil},
		{"temperature misread", "Bake at 350°F", "Bake at 360 F", []string{"350"}},
		{"thousands separator", "1,000 g flour", "1000 g flour", nil},
		{"number elsewhere in the text counts", "1 tsp salt", "salt\n1 tsp pepper", nil},
		{"no numbers", "salt to taste", "pepper", nil},
		{"order and repeats", "3 eggs, 3 yolks, ¼ tsp", "9 eggs 9 yolks", []string{"3", "1/4"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := missingNumbers(c.line, numbersIn(c.tesseract))
			if len(got) != len(c.want) {
				t.Fatalf("missingNumbers(%q, %q) = %v, want %v", c.line, c.tesseract, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("missingNumbers(%q, %q) = %v, want %v", c.line, c.tesseract, got, c.want)
				}
			}
		})
	}
}

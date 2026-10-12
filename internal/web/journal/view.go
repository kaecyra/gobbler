package journal

import (
	"sort"
	"strconv"
	"time"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

// view is everything the journal templates show: the stored journal, newest
// first, and the state of the forms (the values typed and the message of a
// rejected one).
type view struct {
	RecipeID int64
	Name     string
	Rating   *int
	Notes    []store.Note
	Made     []store.MadeIt

	NoteBody        string
	NoteError       string
	RatingError     string
	MadeOn          string
	MadeRating      string
	MadeComment     string
	DateError       string
	MadeRatingError string
}

// newView orders the store's lists newest first and prefills the made-it date
// with today (UTC, the zone the store uses for its own default).
func newView(id int64, rec recipe.Recipe, notes []store.Note, made []store.MadeIt, now time.Time) view {
	v := view{
		RecipeID: id, Name: rec.Name, Rating: rec.Rating,
		Notes:  append([]store.Note(nil), notes...),
		Made:   append([]store.MadeIt(nil), made...),
		MadeOn: now.UTC().Format(dateLayout),
	}
	sort.SliceStable(v.Notes, func(i, j int) bool {
		a, b := v.Notes[i], v.Notes[j]
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.After(b.CreatedAt)
		}
		return a.ID > b.ID
	})
	sort.SliceStable(v.Made, func(i, j int) bool {
		a, b := v.Made[i], v.Made[j]
		if !a.On.Equal(b.On) {
			return a.On.After(b.On)
		}
		return a.ID > b.ID
	})
	return v
}

func (v view) base() string { return journalPath(v.RecipeID) }

func (v view) ratingIs(n int) bool { return v.Rating != nil && *v.Rating == n }

func stars() []int {
	out := make([]int, 0, maxRating)
	for n := minRating; n <= maxRating; n++ {
		out = append(out, n)
	}
	return out
}

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02 15:04") + " UTC" }

func starText(n int) string { return strconv.Itoa(n) + " of " + strconv.Itoa(maxRating) }

package journal

import (
	"sort"
	"strconv"
	"time"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/web/layout"
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

	NoteBody   string
	NotePinned bool
	NoteError  string
	// EditingID is the note shown as an edit form, with EditBody in it and
	// EditError above it; DeletingID is the note asking to confirm its delete.
	// Zero is none.
	EditingID       int64
	EditBody        string
	EditError       string
	DeletingID      int64
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

// ratings lists the ratings a form offers, 1 to 5 spoons.
func ratings() []int {
	out := make([]int, 0, layout.MaxSpoons)
	for n := minRating; n <= layout.MaxSpoons; n++ {
		out = append(out, n)
	}
	return out
}

// spoonOn says whether spoon n of the rating picker is filled: every spoon up
// to the rating is.
func (v view) spoonOn(n int) bool { return v.Rating != nil && n <= *v.Rating }

// madeRatingIs says whether n is the rating in the made-it form; 0 is none.
func (v view) madeRatingIs(n int) bool {
	if n == 0 {
		return v.MadeRating == ""
	}
	return v.MadeRating == strconv.Itoa(n)
}

// verdict is the word of a rating, "Not rated" for none.
func verdict(r *int) string {
	if r == nil {
		return layout.Verdict(0)
	}
	return layout.Verdict(*r)
}

// day is a calendar date as the journal shows it, "9 Mar 2024".
func day(t time.Time) string { return t.Format("2 Jan 2006") }

// madeTimes counts the made-it log: "1 time", "3 times".
func madeTimes(n int) string {
	if n == 1 {
		return "1 time"
	}
	return strconv.Itoa(n) + " times"
}

// note finds note id among the journal's notes.
func (v view) note(id int64) (store.Note, bool) {
	for _, n := range v.Notes {
		if n.ID == id {
			return n, true
		}
	}
	return store.Note{}, false
}

// notePath is note n's own URL: the edit form posts its body there, and
// /edit and /delete below it are the edit form and the delete confirmation.
func (v view) notePath(n store.Note) string {
	return v.base() + "/notes/" + strconv.FormatInt(n.ID, 10)
}

// pinPath is where the pin toggle of note n posts.
func (v view) pinPath(n store.Note) string {
	return v.notePath(n) + "/pin"
}

// pinValue is what the toggle of n posts: the opposite of its state.
func pinValue(n store.Note) string {
	if n.Pinned {
		return "0"
	}
	return "1"
}

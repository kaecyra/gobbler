# Frontend

> **On demand.** Read before changing a `.templ` file, a stylesheet or an
> embedded script under `internal/web/`. The specification is
> [docs/ui/design.md](../../docs/ui/design.md); this file is only what goes
> wrong without it.

- **Read `docs/ui/design.md` and open `docs/ui/mockup.html` before styling a
  page.** The app shipped through every functional wave nearly unstyled,
  because no plan node owned the look and the mockup sat outside the
  repository (ADR-00022).
- **Never key the theme off `prefers-color-scheme`.** The first mockup did,
  rendered dark on the owner's machine, and was rejected: "This isn't a dev
  tool, its a recipe site."
- **No thick coloured left-border accents.** The owner rejected them on sight
  in the mockup review.
- **Change only the element and property named in design feedback.** A
  request to drop one box's border once came back with the mascot gone and the
  layout redone, which cost a review round and trust.
- **Colours, radii and fonts come from the tokens.** A literal colour in a
  page stylesheet is a second palette waiting to drift.
- **A message about the page is a page-level callout, not a line inside a
  content box.** It shows only when there is something to tell. The avatar's
  upload status first sat under the email in the profile box; the owner moved
  it out: "it belongs as a page-level warning/message, not crammed inside the
  profile box."
- **No retry button where doing the thing again is the retry.** Say what
  failed and leave the control that does it. The picture dialog's Try again
  was removed: "They can try again by actually... trying again." Make sure
  repeating works: choosing the same file again fired no `change` until the
  input was cleared first.
- **Line icons come from `layout.Icon`.** Add a drawing to the set in
  `internal/web/layout/icon.go`; never inline an `<svg>` or write a
  package-local `iconX` templ. Before the set, the clock was drawn twice
  (`layout` and `ribbon`), and a name with no drawing rendered nothing, which
  left the Profile link without its picture. Pictures with a meaning of their
  own (meal type, equipment, the recipe scales) stay with their owners.

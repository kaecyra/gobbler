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

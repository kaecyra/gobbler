# UI design

The visual specification for every page ([ADR-00022](../adr/00022-adopt-the-mockup-design.md)).
[`mockup.html`](mockup.html) is the reviewed mockup. Open it in a browser to
see the five drawn screens. Where the mockup and this file disagree, this file
wins.

## Principles

- **Bright and light, for an iPad on a kitchen counter.** Warm, sunny muted
  tones, never grey or overcast. Tablet first, then phone, then laptop. Touch
  targets are at least `--tap` (44px). Text is larger on the recipe and cook
  views, so it reads at arm's length.
- **Light always.** Nothing keys off `prefers-color-scheme`. Dark is opt-in
  from the header toggle, which sets `data-mode="dark"` on the root element
  and remembers the choice in `localStorage`. The page renders correctly when
  storage is unavailable.
- **Nothing remote.** Fonts are system stacks. Icons and mascots are SVG
  embedded in the binary. Scripts are embedded too
  ([ADR-00002](../adr/00002-single-binary-go-stack.md)).
- **No thick coloured left-border accents** on callouts, notes or cards. The
  owner rejected them. Use a quiet tint, an icon with a label, or a hairline.
- **No mockup or watermark labels** anywhere on a page.

## Tokens

Copy these from the `:root` block of [`mockup.html`](mockup.html), including its
`:root[data-mode="dark"]` overrides. The one exception is the corner radius:

| Token | Value | Use |
|---|---|---|
| `--paper` `--card` `--sunk` | `#fcfaf5` `#ffffff` `#f4f1e9` | page, card and inset backgrounds |
| `--ink` `--ink-2` `--ink-3` | `#2b3634` `#525d5a` `#636c69` | text, secondary text, tertiary text |
| `--line` | `#ebe6dc` | hairlines and borders |
| `--wattle` `--wattle-ink` `--wattle-soft` | `#e8836d` `#a64835` `#fdeae4` | the coral accent: primary action, tips, hands-on time |
| `--grain` `--grain-ink` `--grain-soft` | `#f2b85c` `#875a10` `#fef2d8` | timers, "needs a look" marks |
| `--slate` | `#a3adaa` | waiting time |
| `--ok` | `#3f8a57` | done, high confidence |
| `--f-display` | `ui-rounded`, then system stack | headings |
| `--f-body` | system stack | body text, 17px with 1.55 line height |
| `--r` | **6px** (the mockup's 12px is not carried over) | cards and inputs; never above 6px, never pill-shaped |

## Layout and chrome

- **Header.** The mascot stands free with no circle. His feet sit on the
  header's bottom line, with space before the wordmark "gobbler" and the line
  "Find it. Cook it. Gobble it." The nav sits on the right with line icons:
  Library, Shopping list, and Add recipe as the coral action. The other
  sections (catalog, shared, household, invitations, settings) sit behind one
  menu button at the end of the nav. Below 560px the nav becomes a fixed bottom
  bar. The dark-mode toggle sits in the header.
- **Footer.** It links the repository and says "Made by kaecyra in Canada"
  with a red maple leaf.
- **Mascots.** Each drawing is its own embedded SVG file, served cached:
  `gob` in the header, plus `gob-full-rolling-pin`, `gob-full-mixing-bowl`,
  `gob-whisk`, `gob-herbs`, `gob-full-pie` and `gob-tasting` for the panels,
  timers and empty states. A mascot is never clipped. In a box he is shown
  whole, standing on a baseline.

## Components

- **Time ribbon.** Pinned to the bottom of a recipe card as card chrome, and
  shown large on the recipe page as "Ready in about…".
  - Each segment shows only an icon and its time: a hand for active, a clock
    for waiting. There is no legend and no words. The stage kinds go in the
    tooltip and the screen-reader label.
  - Segments are soft and semi-transparent, with a 1px border in the same hue.
    They are never flat opaque blocks.
  - The server computes the segments. An embedded script only fits them to
    the card width: segment width grows with the square root of the duration,
    and neighbours of the same kind merge until the ribbon fits.
  - Hands-on is always coral. Each stage kind maps to a colour family:

    | Stage kind | Family |
    |---|---|
    | `prep`, or none on an active step | `active` (coral) |
    | `rise`, `rest` | `sky` |
    | `steep`, `marinate` | `mint` |
    | `chill` | `ice` |
    | `bake`, `roast` | `apricot` |
    | `simmer`, `braise`, `cook` | `butter` |
    | none on a passive step | `--slate` |

- **Meal type.** An icon with a label, centred on each other. The library
  groups its cards by meal.
- **Spice.** Zero to three chilis. Nothing is shown when the spice level is
  unset.
- **Rating.** Wooden spoons, never stars. An unrated recipe says "New".
- **Tips.** They float free, with no box or tint, but stand out: a coral icon
  and label, with the text at full recipe size.
- **Cook mode.** The entry is labelled "Cook mode", never "Start cooking". It
  sits in the recipe view's controls row, not as a big button under the title.
- **Low confidence** (review screen). A grain-coloured mark on the source line
  and a dot on the parsed line, with a hint. No left border.

## Screens

| Screen | Route | Mockup section |
|---|---|---|
| Home | `/` | `#library` (hero and side panels) |
| Library and search results | `/recipes`, `/recipes?q=` | `#library` |
| Recipe, journal and history | `/recipes/{id}` and its subpages | `#recipe` |
| Cook mode | `/recipes/{id}/cook` | `#cook` |
| Shopping list | `/shopping` and its subpages | `#shop` |
| Add and review | `/ingest`, `/ingest/{token}`, `/ingest/photo` | `#add` |

- **Search** is Google-style: a big centred box on home with a `/` focus key.
  Typing moves to `/recipes?q=`, the library's existing search, through HTMX
  `hx-push-url`, so all of the state is in the URL and Back returns home.
- **Library side panels:** a suggestion with "Another idea", recently made,
  and haven't made in a while. They are read from the made-it log.
- There are **no tag filters**.
- **Equipment** on the recipe page and in cook mode is a row of chips, each
  an icon and the name, with the size hint where one applies
  ([ADR-00024](../adr/00024-equipment-chips-icons-and-catalog.md)).
- **Cuisine** is plain text: in the recipe page's meta row and under a library
  card's source line, and absent when unset. The editor sets it from a select
  of the fixed list ([ADR-00025](../adr/00025-one-cuisine-per-recipe.md)).
- **Shopping list grouping:** a By aisle / By recipe toggle, two links to the
  same page with the choice in `?group=recipe`. By aisle is the default. By
  recipe shows each recipe's own share of a merged line; hand-added items sit
  in a last "Added by hand" group.
- **Editor save bar:** Save and Cancel sit in a bar stuck to the bottom of the
  viewport while the form scrolls, above the bottom tab bar on a phone.
- **Pages with no mockup** take the same tokens and components: the editor,
  settings, household and invitations, sharing, catalog, pantry, shopping
  build and export, and the operator pages. They are reviewed from screenshots
  in their pull requests.

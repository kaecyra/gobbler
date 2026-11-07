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
  from the toggle in the account menu, which sets `data-mode="dark"` on the root element
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
| `--maple` `--maple-ink` `--maple-soft` | `#dd2e44` `#a51d30` `#fde8ea` | Canadian red: the footer leaf and the Canada marks on `/canada` |
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
  bar. The account button ends the header (below).
- **Header spacing.** Every item in the header's right-hand group, the nav
  entries, the coral action and the account button, is separated by the same
  gap, the `--nav-gap` token (8px), at every width. No item adds a margin of
  its own to stand apart.
- **Account button and menu.** The person's avatar sits at the end of the
  header, where the light and dark button used to be. It is a `--tap` square
  with the `--r` corner, never a circle. Pressing it opens a menu, in this order:
  - the light and dark toggle, labelled with the mode it switches to ("Dark
    mode" or "Light mode");
  - **Profile**, the link to the person's own profile;
  - **Sign out**, shown where people sign in to the app
    ([ADR-00030](../adr/00030-google-sign-in.md)). It is not in the "More
    sections" menu.

  The avatar is the one the person uploaded or, until then, a default drawn in
  the app: a mirrored five by five pattern from a hash of the email, coloured by
  a pair of tokens (`av-bg-N`, `av-fg-N`), with nothing fetched from outside
  ([ADR-00032](../adr/00032-profiles.md)). The same avatar heads the profile
  page at 96px.
- **Signed-out figure.** A visitor with no identity, such as one on a public
  page ([ADR-00029](../adr/00029-public-paths.md)), gets a neutral figure in
  the account button's place: the same size and menu, holding the light and dark
  toggle and, where people sign in to the app, **Sign in**. The toggle works from
  both, remembers its choice in `localStorage` and never follows the system
  setting (see Principles).
- **Footer.** It links the repository, with the running version beside the
  link in muted text, and says "Made by kaecyra in Canada" with a red maple
  leaf.
- **Mascots.** Each drawing is its own embedded SVG file, served cached:
  `gob` in the header, plus `gob-full-rolling-pin`, `gob-full-mixing-bowl`,
  `gob-whisk`, `gob-herbs`, `gob-full-pie` and `gob-tasting` for the panels,
  timers and empty states. A mascot is never clipped. In a box he is shown
  whole, standing on a baseline.

## Components

- **Time ribbon.** Shown large on the recipe page as "Ready in about…". A
  recipe card carries no ribbon: it shows only the recipe's total time, a small
  clock icon and the time in the card's meta line, the figure the library's
  time grouping uses, so a card never contradicts the group it sits under.
  - Each segment shows only an icon and its time: a hand for active, a clock
    for waiting. There is no legend and no words. The stage kinds go in the
    tooltip and the screen-reader label.
  - Segments are soft and semi-transparent, with a 1px border in the same hue.
    They are never flat opaque blocks.
  - The server computes the segments. An embedded script only fits them to
    the ribbon's width: segment width grows with the square root of the
    duration, and neighbours of the same kind merge until the ribbon fits.
  - Hands-on is always coral. Each stage kind maps to a colour family:

    | Stage kind | Family |
    |---|---|
    | `prep`, `mix`, `serve`, or none on an active step | `active` (coral) |
    | `rise`, `rest` | `sky` |
    | `steep`, `marinate` | `mint` |
    | `chill` | `ice` |
    | `bake`, `roast` | `apricot` |
    | `simmer`, `braise`, `cook` | `butter` |
    | none on a passive step | `--slate` |

- **Recipe picture**
  ([ADR-00039](../adr/00039-recipe-pictures.md), mockup `.rpic` and `.rhero`).
  - A library or search card shows it full-bleed at the top, 150px tall,
    fading into the card colour over its lower 60%. The recipe page shows it
    as a header 210px tall (160px under 560px wide) with the title over the
    fade, fading into the page colour.
  - A recipe with no picture, or whose picture object is missing from the
    store, shows the placeholder band at the same height. The band is drawn
    from tokens only (`.ph0` to `.ph5` in `app.css`) and carries the meal
    type's icon in a small tile. Its variant is the recipe id modulo 6, so a
    recipe keeps its band. A recipe with no meal type has the band alone.
  - The picture is decoration: `alt=""`, and the card's title link is the way
    in. The band sits under the picture while it loads. A page script removes
    a picture that fails to load, which leaves the band; the server logs the
    missing object when the picture route answers 404.
  - Cook mode, the home side panels, shopping lists, the journal and history
    show no picture.
- **Meal type.** An icon with a label, centred on each other. The library
  groups its cards by meal.
- **Spice.** Zero to three chilis. Nothing is shown when the spice level is
  unset.
- **Rating.** Wooden spoons, never stars. An unrated recipe says "New".
- **Tips.** They float free, with no box or tint, but stand out: a coral icon
  and label, with the text at full recipe size.
- **Author notes.** They follow the last step on the recipe page, apart from
  the chain (ADR-00041, ADR-00045): extra vertical space separates them from
  the steps, and no connector line joins the last step to the first note or one
  note to the next. Each sits where a step number would, behind a bulb in a soft
  wattle circle. The text is a little smaller and
  lighter than step text, a bold title leads when the note has one, and a small
  grey "From the author" closes it. Cook mode reaches them through an
  "Author's Notes" button. Variant A of `#recipe` in the mockup is the target,
  except that ADR-00045 overrides it on the connectors and the gap: the mockup
  still draws dotted lines to and between notes.
- **Markdown in steps and notes**
  ([ADR-00046](../adr/00046-markdown-in-steps.md)). Step text and author-note
  text render through `layout.Markdown` inside an element with the `md` class.
  - Every block takes the size, line height and colour of the text it sits
    in: 18.5px `--ink-2` in a step, 17px `--ink-2` in an author note, and the
    callout's own size in a meanwhile item. Blocks sit 6px apart.
  - Lists are indented 1.4em, with discs for a bulleted list and numbers for
    a numbered one. Items sit 2px apart.
  - A blockquote is a `--sunk` panel with the `--r` radius and 8px by 14px of
    padding, and no border.
  - Strong text is `--ink` at weight 650, the weight of an author note's
    title. Inline code is `--f-mono` at 0.88em on a `--sunk` chip with the
    `--r-sm` radius.
  - Links keep the page's link colour.
  - In an author note, the bold title, a lone paragraph and the "From the
    author" credit share one line, as before. When the note holds a list, the
    list starts on a line of its own and the credit follows it on another.
- **Video.** On the recipe page it sits at the top of the steps column, under
  the title and above any tips and the first step, as a 16:9 box no wider than
  the column. It plays only when its button is pressed
  ([ADR-00021](../adr/00021-youtube-video-on-a-recipe.md)), and a playing video
  stays put when the recipe is scaled. A recipe without a video shows nothing.
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
  - "A while" is more than 30 days since the recipe was last made. A recipe
    made more recently is neither suggested nor named as not made in a while.
  - The suggestion offers recipes never made first, by name, then those not
    made in a while, made longest ago first. On the library it skips the
    recipe the haven't-made-in-a-while panel names, so the two never repeat.
  - The suggestion is a library card, total time included: a small mascot
    head speaks the voice line above it.
  - Haven't made in a while shows the rating as spoons.
  - A panel with nothing to show is not drawn. With none to draw, the library
    drops its side and the results take the width, and an empty library shows
    only the empty state, on home and in the library.
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
- **Parts:** every page calls a recipe component a "part" (Add a part, Part 2
  name, Parts of this recipe). The data model, the ADRs, field names and the
  MCP tools keep "component".
- **Pages with no mockup** take the same tokens and components: the editor,
  settings, household and invitations, the profile page, sharing, catalog, pantry, shopping
  build and export, and the operator pages. They are reviewed from screenshots
  in their pull requests.

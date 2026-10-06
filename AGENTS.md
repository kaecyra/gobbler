# gobbler

gobbler is a self-hosted, single-user recipe manager: recipes that are easy to
find and remember, free of ads, and easy to turn into shopping lists. It
ingests recipes from URLs, pasted text and photos, stores them as structured
data — components, ingredients with exact quantities and qualifiers,
equipment, a step dependency graph, times — and generates merged,
aisle-grouped shopping lists from them.

It is one Go binary (chi, templ, HTMX) over a SQLite file, run with Docker
Compose on an Ubuntu VM and reached only through a Cloudflare tunnel behind
Cloudflare Access. Ingestion is rule-based first, with Claude or Gemini as a
fallback. The design and build order are in [docs/design.md](./docs/design.md).

The stakes are personal data: notes, edits and history that cannot be
recreated from the source sites. A bug that loses or silently corrupts a
recipe, or exposes the app without Access verification, is expensive. A layout
glitch is cosmetic.

## Boundaries

- **No in-app login and no password storage.** Identity comes only from a
  verified Cloudflare Access JWT ([ADR-00003](./docs/adr/00003-cloudflare-access-identity.md)).
- **The LLM never runs before the rule-based path has been tried, and its
  output is never saved without the review screen**
  ([ADR-00006](./docs/adr/00006-rule-first-ingestion.md)).
- **No third-party trackers, analytics, ads or remotely loaded scripts.**
  Frontend assets are embedded in the binary.
- **An ingested recipe's source snapshot is never modified.**
- **No secrets in the repository.** API keys, tokens and S3 credentials come
  only from the environment.

## Goals

- Prefer consistency over cleverness.
- Make the smallest correct change.
- Follow existing patterns before introducing new abstractions.
- Keep ingestion deterministic: the same input and parser version produce the
  same draft.

### Search for prior art before inventing a pattern

The last of those goals is the one most often skipped, so it gets a paragraph.

Before writing a helper, a guard, an error shape or a naming scheme, grep for
one that already exists. A sibling module has usually solved the same problem,
and the version already in the tree is the one reviewers and future readers
expect. Generalise what you find rather than adding a parallel implementation
beside it.

Two helpers doing one job is worse than one slightly awkward helper, because
the next person has to work out which is authoritative before they can do
anything at all.

## What to read, and when

Reading everything before every task is not thoroughness — it is context spent
on rules that do not apply, which is context unavailable for the work. Read the
always-on set, then route.

**Always on — read these before any task:**

- [rules/collaboration.md](./.claude/rules/collaboration.md) — when to build, when to stop and discuss, broken windows, plans
- [rules/code-style.md](./.claude/rules/code-style.md) — Go layout, interfaces, logging, never typing versions from memory
- [rules/error-handling.md](./.claude/rules/error-handling.md) — fail loud, wrapping, low confidence versus errors in ingestion
- [rules/git-workflow.md](./.claude/rules/git-workflow.md) — branches, Conventional Commits, gates, SemVer bumps

**On demand — read the one that matches what you are touching:**

| Touching | Read |
|---|---|
| Package boundaries, configuration, which wave something belongs to | [docs/design.md](./docs/design.md) |

**Where the state lives**, when you need to know rather than to read:

| Question | Answer |
|---|---|
| What was decided | [`docs/adr/`](./docs/adr/README.md) |
| How the system fits together | [`docs/design.md`](./docs/design.md) |
| Open work | GitHub issues on `kaecyra/gobbler` |

**Before proposing anything architectural**, check
[docs/adr/](./docs/adr/README.md) — numbered, immutable rulings with their
reasoning. Before *deciding* anything architectural, add one. ADRs outrank
every rule file; a rule that contradicts an ADR is a bug in the rule.

No on-demand rule files exist yet. Each one needs a `docs/` specification to
defer to; when one is written for a surface (the database, ingestion, the
frontend), add its rule file and a row to the on-demand table above.

## Writing rules, ADRs and docs

Rules, ADRs and specifications are read far more often than they are written,
usually by someone with a narrow question and no time.

- **Say the thing, then stop.** A rule that needs three paragraphs of preamble
  is not yet clear enough to write down.
- **Do not write a rule you cannot cite a reason for.** An invented rule gets
  obeyed as if it were earned and is harder to remove than to add. An honestly
  thin section beats a padded one.
- **Record what it cost.** The most useful line in a rule names what went wrong
  when it was absent; that is what keeps it alive past the next person who
  thinks it pedantic.
- **No duplication across layers.** If it belongs in `docs/`, link it. A copy
  drifts, and when two layers disagree the reader cannot tell which is stale.
- **Rule files and `docs/` name real things.** They are living documents,
  edited when the code moves, so naming real files, functions and incidents is
  what makes them followable.
- **ADRs name the architecture, not the code.** An ADR is immutable and nothing
  keeps its citations honest, so ground it in directory and package layout,
  schema objects, interfaces, a symbol that *is* the contract, or another ADR by
  number and section. A source path in an ADR is a smell; a line number is
  worse. Location-specific evidence belongs in the issue or pull request.
- **Reasoning appears once.** A point made in Context is not restated in
  Decision or Consequences. An ADR is the ruling, not the story of reaching it.
- **An ADR carries its reasoning and what was rejected.** Without the reasoning
  the next reader relitigates; without the rejected options they come back. An
  option presented only to be knocked down is a decision made for the reader.
- **Obligations must be checkable** by someone who was not in the
  conversation. "Amend `docs/api.md`'s authentication table to add a Bearer
  row" is an obligation; "update the docs" is not. Marking one done in the pass
  that performed it is a claim, not a verification.

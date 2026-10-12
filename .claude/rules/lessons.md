# Lessons

> **Always on.** Corrections from past work: the mistake, what it cost, and the
> rule that prevents it.

## Review findings are checked against the specification, not accepted on sight

**Mistake.** Review of the first `store` pull request kept asking how raw SQL
outside the API could alter history rows. Each finding was treated as a defect
and answered with another SQLite trigger.

**Cost.** Review ran four rounds past the three-round cap, and the schema filled
with defensive triggers guarding against code that did not exist. All of them
were then removed (ADR-00015).

**Rule.** Before fixing a finding, find the obligation it breaks. If no ADR or
`docs/` specification asks for the behaviour, the finding is scope. Answer the
reviewer and decline it, or raise it as an uncovered decision. Do not implement
it. Two rounds in a row on the same family of finding mean stop and ask.

## VERSION is set against main at merge, never planned ahead

**Mistake.** The dispatcher handed out VERSION numbers in the order it
planned to merge PRs. When the actual merge order differed, versions were
skipped.

**Cost.** #64 went from 0.14.0 to 0.16.0, so 0.15.0 never shipped. Review
caught the same mistake on #69, which jumped two minor versions, and
flagged it as a MUST.

**Rule.** A PR's VERSION is one bump over main's VERSION at the moment it
merges: minor for `feat`, patch for `fix` or `refactor` (git-workflow.md,
Versioning). A branch that falls behind main re-bumps after its rebase. Do
not reserve numbers for PRs that have not merged.

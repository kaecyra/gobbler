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

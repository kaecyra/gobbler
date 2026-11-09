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

## Public copy is checked against content-style before it is proposed

**Mistake.** Proposing a new headline for `/canada`, the agent suggested the
subline "answering to Canadian courts, and to no one else's" and the heading
"Sovereignty, not just residency". Both are antitheses, and the first is also a
reveal clause. The `tim:content-style` guide forbids both.

**Cost.** The owner had to point it out mid-task. On a page meant as a public
statement, copy that reads as machine-written undercuts the statement.

**Rule.** Load `tim:content-style` before drafting any user-facing words,
including suggestions in conversation, and check each draft against its list of
machine-written constructions before showing it.

## An approved mockup is the visual target, checked side by side

**Mistake.** The editor redesign was approved from a mockup. The brief to the
implementing agent turned the mockup into a requirements list and told it to
skip some of what was drawn, drag handles among them. The result was reviewed
for bugs, not against the mockup.

**Cost.** The owner had to point out that the shipped screen looked much less
polished than the mockup and lacked its drag handles. That took a second
implementation pass, which ran close to an hour.

**Rule.** Hand the implementer the rendered mockup as the target. Leave nothing
in it out unless the owner says so. Before showing the result, screenshot it at
the mockup's width and list every difference from the mockup. Close each one,
or name the owner's override or the data limit that keeps it.

## Judge the owner's work only from evidence that can show it

**Mistake.** Asked to learn the owner's editing style from four imported
recipes, the agent could not see the originals or the revision history. It
still called the recipes "lightly edited" on the strength of a few leftovers
that slipped through.

**Cost.** The owner had cleaned them a fair amount and called the comment rude.
It was wrong, and it was a judgement nobody had asked for.

**Rule.** When the evidence cannot separate what the owner did from what the
tool did, say what cannot be seen and stop there. Report leftovers as items to
fix, not as a verdict on how much work went in.

## Screens get no narration

**Mistake.** The review screen gained a subtitle restating that nothing saves
until confirm, a below-threshold banner restating the confidence chip, a
callout printing the agent's revision reason, a "N drafts are waiting, and
this is the oldest" sentence, and a sentence under every step the LLM ordered.
An agent added each one as helpful context.

**Cost.** The owner called the page "a flea market for HTML": warnings in
several styles, none telling them anything the page did not already show. All
of them were removed in #589.

**Rule.** Before adding a sentence, callout or banner to a screen, name the
decision it changes for the person. If the page's controls or marks already
say it, or it explains the system instead of the recipe, leave it out. Never
print an agent's own explanation on a person's screen.

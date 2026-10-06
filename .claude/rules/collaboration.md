# Collaboration

> **Always on.** How work gets decided, started and finished here. The
> mechanics live elsewhere and are linked from each section; this file is the
> part that governs when you move on your own and when you stop.

## Coverage decides how we work

The question is never how large the change is. It is whether the design
question in front of you has already been answered.

**Covered** — an ADR or a `docs/` specification answers it. Build it. Do not
re-open a settled decision, and do not ask permission to implement what was
already agreed: the decision *was* the permission. Move, and finish.

**Uncovered** — nothing answers it. That is trail-blazing, and trail-blazing is
collaborative. Explain the situation in plain English, argue the options
honestly including the ones you are rejecting, and stop there. Final say is the
human's. Then it becomes an ADR, and then it is covered.

So a one-line change on an unsettled boundary needs the conversation, and a
nine-hundred-line module implementing a decision that was already made does
not. Size was never the signal.

If you cannot tell which side you are on, you are on the uncovered side.

## Broken windows get fixed

Decay compounds, and a codebase learns what it is allowed to tolerate. "That
was pre-existing, it belongs in a follow-up ticket" is not an answer. If it is
broken, and you know why, and you know the fix — fix it. No permission needed,
no size ceiling.

The one boundary is file ownership. Your lane is the paths your ticket or plan-graph node names. With no ticket, it is the paths you named in the plan you posted before starting.

- **Inside your lane** — fix it. Put unrelated repairs in their own commit so
  the reviewer can read one concern at a time.
- **Outside your lane** — open an issue naming the file, the line, why it is
  wrong and the fix you already know, then reference it from your pull request.
  Do not edit the file. Someone else owns it, and a concurrent edit becomes a
  merge conflict discovered when both branches are finished and neither wants
  to rebase.

## Leave no claim you just made false

Changing behaviour is the moment something in this repository starts lying. A
constraint's comment, an ADR obligation, a rule file asserting what the code
does, a doc comment describing a guarantee, a test name claiming coverage —
each was true when written and none of them updates itself.

**Before you commit, grep for what asserted the old behaviour.** The value you
changed, the function you renamed, the constraint you widened, the bug you
fixed. Then fix or delete what is now false, in the same commit as the change
that falsified it — not in a follow-up, because a follow-up is a promise and
this is a fact.

If the false claim sits outside your lane, say so in your report and name the
file and line, the same as any other out-of-lane defect.

This is decay, and decay is caught at point of contact. Every instance is one
grep away from being caught by whoever made it false, and otherwise waits to be
found by the next person into that file.

## Plan before code, and show the plan

Covered work still gets a plan before it gets a diff: files to touch, ordered
steps, what the tests will prove, and anything still ambiguous.
Post it in the conversation before the first edit. Work dispatched from the plan graph carries it in the node's issue.

Uncovered work gets the conversation first and the plan after.

Mark items complete as you go and record what actually happened at the end. A
plan that lags the work is worse than no plan, because it is trusted.

## Corrections become rules

After a correction, write the pattern into `lessons.md` in this directory —
creating it, with an always-on scope line and a row in `AGENTS.md`, on the
first correction: the mistake, what it cost, and the rule that prevents
it. A correction about what the code must look like, rather than about how we
work, belongs in a rule file here instead.

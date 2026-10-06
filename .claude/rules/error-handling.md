# Error handling

> **Always on.** Any code that can fail, which is most of it. The rule is one
> sentence; the rest is what it looks like when applied here.

## Fail fast, fail loud

No lazy fallbacks. No mocked features. If something is broken, raise — do not
bury it behind a default value or a silent no-op. **Silent failures are bugs
that hide other bugs**, which is what makes them expensive rather than merely
untidy.

Fall back to degraded behaviour only when the degraded behaviour is genuinely
useful to the person on the other end. When you do, log what failed, what the
fallback is, and why it was chosen.

## Do not silence a type error that describes a real failure

The compiler telling you a value might be absent is the same signal as a
runtime check failing. Answer it; do not assert it away. Silencing a class of
these errors is one search-and-replace and leaves one latent crash per
occurrence, each with nothing pointing at the cause.

In Go the signal is the `error` return. Never discard one with `_ =` from a
call that can meaningfully fail, and never write `if err != nil { return nil }`
— that converts a failure into a success. `errcheck` in `golangci-lint` catches
the first; review catches the second.

## Errors carry a message the surface can show

An error thrown from a domain module ends up in front of a user, in a log
someone pages on, or in an audit record. Write the message for whoever reads it
there, not for the stack trace.

Never swallow an exception silently. Never a catch that logs nothing. If a
side-effect is genuinely allowed to fail without failing its caller, say *why*
at the catch and log the error — and if losing it silently would actually be a
bug, it was never best-effort.

## Raise at the boundary where the contract broke

Not three frames later where it becomes confusing, and not at the top where the
context is gone. Validate external input — request bodies, headers, arguments,
anything crossing a trust boundary — where it enters, and handle the shape
being wrong rather than only absent.

Distinguish "legitimately empty" from "something failed". No rows is an answer;
a query that threw is not.

## Wrap with context, branch with `errors.Is` and `errors.As`

Wrap on the way up with what the current frame was doing:
`fmt.Errorf("parse ingredient line %q: %w", line, err)`. Domain packages export
sentinel errors (`ErrNotFound`) or typed errors only where a caller branches on
them. HTTP handlers map domain errors to status codes in one place, not
per handler.

## Low confidence is data, not an error

In ingestion (ADR-00006), a line the parser cannot fully read is a
low-confidence result shown on the review screen — not an error. An error means
the pipeline itself broke: the fetch failed, OCR crashed, the provider timed
out or returned something that does not match the schema. Keep the two apart:
an error must never be turned into a quietly low-scored draft, and a hard line
must never abort the whole ingest.

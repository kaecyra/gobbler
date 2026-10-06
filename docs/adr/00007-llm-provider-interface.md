# ADR-00007: LLM provider interface with Claude and Gemini

**Status:** Accepted

**Date:** 2026-10-06

## Context

The ingestion fallback (ADR-00006) needs a language model, and the owner wants
to use several remote providers through their APIs, starting with Anthropic's
Claude and Google's Gemini. Providers change models and pricing often, so the
choice of provider and model must be cheap to change.

Options considered:

- **Hard-code one provider.** Simplest, but switching later touches the
  ingestion code. Rejected.
- **A third-party multi-provider gateway or library.** Another dependency and
  sometimes another service, for two providers. Rejected for now.
- **A small in-house interface with one implementation per provider.** Chosen.

## Decision

The ingestion package defines the interface it needs: given source text and/or
an image plus the rule-based draft, return a structured recipe draft matching
the ingestion schema. Each provider is an implementation in its own package,
using the provider's official Go SDK and its structured-output or tool-calling
feature to constrain the response to the schema.

- Providers at launch: Claude (Anthropic API) and Gemini (Google Gen AI API).
- Which provider is used, and which model for text and for vision, is
  configuration. Model identifiers are never hard-coded.
- API keys come only from the environment.
- Every call has a timeout. A failed or invalid response leaves the rule-based
  draft in place on the review screen with the error shown; it never blocks
  saving that draft.
- Responses are validated against the schema before use; an invalid response is
  an error, not a partial draft.
- Each call logs provider, model, latency and token counts; never the API key.

## Consequences

Adding a provider means one new package and configuration. Provider-specific
features beyond structured output are not used, to keep implementations
interchangeable.

**Obligations:**

1. The ingestion package depends on the interface, not on any provider SDK.
2. Both providers pass the same contract test suite against recorded
   responses.
3. Selecting provider and model needs no code change.
4. A provider timeout or invalid response is tested to leave the rule-based
   draft intact.

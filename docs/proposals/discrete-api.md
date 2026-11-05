# Open decisions: a discrete API behind the handlers

Owner request, 2026-11-05 (raised during `docs/proposals/recipe-images.md`, D7): split the work
behind the web handlers into a discrete API. The same binary can expose it for now, but it should be
discrete. Architectural; needs an ADR. To be worked through with `/overseer:decide`.

## Verified facts

- There is no service layer. The web handlers (HTMX, one subpackage per feature, ADR-00012) and the
  MCP tools (`internal/mcp/`, ADR-00034) each call the domain packages directly: `store` is imported
  by 32 files under `internal/web/`; `internal/mcp/` imports `store`, `ingest`, `shopping`,
  `recipe`, `catalog`, `household` and others.
- Two authentication routes exist: the session cookie from Google sign-in (ADR-00030) and API keys
  (`internal/apikey`), used by MCP.

## Open

1. **Boundary.** In-process Go interfaces (a service layer the handlers call), an HTTP/JSON API the
   handlers call over the network, or both.
2. **Authentication** for an API reached over the network: session cookie, API keys, or both.
3. **MCP.** Whether the MCP server becomes a client of the API.
4. **ADR-00012.** How the API's packages relate to the one-subpackage-per-feature web layout.
5. **Migration.** Whether existing handlers move all at once or feature by feature.

## Decided

(none yet)

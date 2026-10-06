# ADR-00013: End-to-end tests with Node Playwright under e2e/

**Status:** Accepted

**Date:** 2026-10-06

## Context

The gates require a Playwright end-to-end test for every route (`make e2e`).
ADR-00002 rejected a Node runtime for the application. The question is whether
that rejection extends to test tooling.

Options considered:

- **playwright-go.** Keeps tests in the application's language. It still
  downloads Playwright's Node driver and browsers at install time, so it does
  not remove Node from the machine running the tests, and its assertion and
  fixture tooling trails the upstream test runner.
- **`@playwright/test` in its own directory.** The first-class runner, with
  auto-waiting assertions, fixtures, traces and parallel workers. Node is a
  test-time dependency only, confined to one directory and absent from the
  production image. Chosen.

## Decision

- End-to-end tests use `@playwright/test`, with `package.json`, its lockfile
  and `playwright.config.ts` under `e2e/`.
- The suite runs against a running `gobbler` binary started in development-auth
  mode on a loopback address (ADR-00003), with a throwaway database.
- `make e2e` installs nothing; provisioning a checkout (`npm ci` and the
  browser install in `e2e/`) is a setup step, not part of the gate.
- Nothing under `e2e/` is copied into the container image.

## Consequences

ADR-00002's rejection of Node stands for the application and its image; this
record admits Node only as a test tool. Contributors need Node installed to run
the end-to-end gate.

**Obligations:**

1. The `Dockerfile` copies nothing from `e2e/`, and the production image contains
   no Node runtime.
2. Every route registered by `internal/web` has at least one test under `e2e/`.

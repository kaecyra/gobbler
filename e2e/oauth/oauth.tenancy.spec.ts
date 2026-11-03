import { test, expect, type APIRequestContext, type Playwright } from '@playwright/test';
import { DEV, OTHER, STRANGER, asIdentity } from '../fixtures/identity';

// Cross-household probes for the OAuth routes (ADR-00018 obligation 5,
// ADR-00034). Each route is requested by an authenticated non-member (STRANGER)
// and by a member of another household (OTHER), and the response carries
// nothing of DEV's. The route-coverage gate reads this file (see
// fixtures/route-coverage.mjs): each PROBES line is one route, written as a
// method literal followed by the path literal on the same line.
//
//   - The discovery documents, registration and the token endpoint are public
//     (ADR-00029): both identities get the same answer as anyone.
//   - The authorization endpoint needs a membership. A non-member is refused
//     (403); a member gets the answer to the request, which here names no client
//     and so is a 400.

interface Probe {
  method: 'GET' | 'POST';
  path: string;
  stranger: number;
  other: number;
}

// One probe per line: the gate pairs the method with the first path on the line.
const PROBES: Probe[] = [
  { method: 'GET', path: '/.well-known/oauth-protected-resource', stranger: 200, other: 200 },
  { method: 'GET', path: '/.well-known/oauth-protected-resource/mcp', stranger: 200, other: 200 },
  { method: 'GET', path: '/.well-known/oauth-authorization-server', stranger: 200, other: 200 },
  { method: 'POST', path: '/oauth/register', stranger: 400, other: 400 },
  { method: 'POST', path: '/oauth/token', stranger: 400, other: 400 },
  { method: 'GET', path: '/oauth/authorize', stranger: 403, other: 400 },
  { method: 'POST', path: '/oauth/authorize', stranger: 403, other: 400 },
];

function as(playwright: Playwright, baseURL: string, email: string) {
  return playwright.request.newContext({ baseURL, ...asIdentity(email) });
}

async function probeAll(ctx: APIRequestContext, expected: (p: Probe) => number): Promise<void> {
  try {
    for (const p of PROBES) {
      const res = await ctx.fetch(p.path, { method: p.method, maxRedirects: 0, failOnStatusCode: false });
      expect(res.status(), `${p.method} ${p.path}`).toBe(expected(p));
      expect(await res.text(), `${p.method} ${p.path} leaks DEV`).not.toContain(DEV);
    }
  } finally {
    await ctx.dispose();
  }
}

test("a non-member gets the OAuth routes and nothing of DEV's household", async ({ playwright, baseURL }) => {
  await probeAll(await as(playwright, baseURL!, STRANGER), (p) => p.stranger);
});

test("a member of another household gets the OAuth routes and nothing of DEV's household", async ({ playwright, baseURL }) => {
  await probeAll(await as(playwright, baseURL!, OTHER), (p) => p.other);
});

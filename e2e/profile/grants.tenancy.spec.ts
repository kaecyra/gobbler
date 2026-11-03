import { test, expect, type APIRequestContext, type Playwright } from '@playwright/test';
import { DEV, OTHER, STRANGER, asIdentity } from '../fixtures/identity';
import { profileAddress } from '../fixtures/routes/tenancy-support';

// Cross-household probes for the connected apps routes on the profile
// (ADR-00018 obligation 5, ADR-00034). The section is self-only: through DEV's
// real profile address, a non-member and a member of another household are both
// told there is nothing there, and so are they at an address that was never
// issued. The route-coverage gate reads this file (see
// fixtures/route-coverage.mjs).

interface Probe {
  method: 'GET' | 'POST';
  path: (devAddress: string) => string;
}

// A profile address is 22 characters of base64url; this one was never issued.
const GUESSED = 'AAAAAAAAAAAAAAAAAAAAAA';

// One probe per line: the gate pairs the method with the first path on the line.
const PROBES: Probe[] = [
  { method: 'GET', path: (a) => `/profile/${a}/grants` },
  { method: 'POST', path: (a) => `/profile/${a}/grants/1/revoke` },
  { method: 'GET', path: () => `/profile/${GUESSED}/grants` },
  { method: 'POST', path: () => `/profile/${GUESSED}/grants/1/revoke` },
];

function as(playwright: Playwright, baseURL: string, email: string) {
  return playwright.request.newContext({ baseURL, ...asIdentity(email) });
}

async function probeAll(playwright: Playwright, baseURL: string, ctx: APIRequestContext): Promise<void> {
  const dev = await as(playwright, baseURL, DEV);
  try {
    const address = await profileAddress(dev);
    for (const p of PROBES) {
      const res = await ctx.fetch(p.path(address), { method: p.method, maxRedirects: 0, failOnStatusCode: false });
      expect(res.status(), `${p.method} ${p.path(address)}`).toBe(404);
      expect(await res.text()).not.toContain(DEV);
    }
  } finally {
    await dev.dispose();
    await ctx.dispose();
  }
}

test("a non-member finds no connected apps on DEV's profile", async ({ playwright, baseURL }) => {
  await probeAll(playwright, baseURL!, await as(playwright, baseURL!, STRANGER));
});

test("a member of another household finds no connected apps on DEV's profile", async ({ playwright, baseURL }) => {
  await probeAll(playwright, baseURL!, await as(playwright, baseURL!, OTHER));
});

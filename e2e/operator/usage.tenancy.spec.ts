import { test, expect } from '@playwright/test';
import { OTHER, STRANGER, asIdentity } from '../fixtures/identity';

// Cross-household probes for the LLM usage routes (ADR-00018 obligation 5,
// ADR-00058). The usage page reads no household's data as a household and is an
// operator's alone, so a verified email with no household (STRANGER) and a
// member of a household (OTHER) are both refused. They run against the shared
// dev-auth server, where OPERATOR is the only operator.
//
// The route-coverage gate reads this file: each probe is one line holding the
// method and the path, and no other path literal may appear here.
const PROBES = [
  { method: 'GET', path: '/operator/usage' },
  { method: 'POST', path: '/operator/usage/sync' },
  { method: 'POST', path: '/operator/usage/prices' },
  { method: 'POST', path: '/operator/usage/prices/clear' },
];

function refusesEveryProbe() {
  for (const probe of PROBES) {
    test(`${probe.method} ${probe.path} is refused`, async ({ request }) => {
      const res = await request.fetch(probe.path, {
        method: probe.method,
        form: probe.method === 'POST' ? { provider: 'claude', model: 'm', input: '1', output: '1' } : undefined,
        headers: { 'Sec-Fetch-Site': 'same-origin' },
        maxRedirects: 0,
      });
      expect(res.status()).toBe(403);
      expect(await res.text()).not.toMatch(/Cost|Tokens in|Sync now/);
    });
  }
}

test.describe('a verified email with no household', () => {
  test.use(asIdentity(STRANGER));
  refusesEveryProbe();
});

test.describe('a member of another household', () => {
  test.use(asIdentity(OTHER));
  refusesEveryProbe();
});

import { test, expect, type APIRequestContext } from '@playwright/test';
import { DEV, OTHER, OPERATOR, STRANGER } from '../fixtures/identity';
import {
  DEV_HOUSEHOLD,
  SETUP_TIMEOUT_MS,
  as,
  expectHouseholdDeletionClean,
  expectUntouched,
  leaks,
  makeWorkdir,
  pendingShare,
  populate,
  removeWorkdir,
  send,
  startHousehold,
  startServer,
  stopServer,
  uniqueEmail,
  type Ids,
  type Server,
  type Workdir,
} from '../fixtures/routes/tenancy-support';

// Cross-household tests (ADR-00018 obligation 5). The membership check is the
// only lock between households, so every route the router registers is
// requested here by an authenticated non-member (STRANGER) and by a member of a
// household that does not hold the data (OTHER), against data that belongs to
// the dev household (DEV).
//
// The route-coverage gate (fixtures/route-coverage.mjs) reads this file: each
// PROBES line is one route, written as a method literal followed by the path
// literal on the same line, and it must name STRANGER and OTHER as the identity
// of a request context (as(...)). Every other request, to set data up or read
// it back, lives in fixtures/routes/tenancy-support.ts, so that no path literal
// outside PROBES counts as coverage and the gate rejects one.
//
// What each response must be, by kind of route:
//   - household routes: a non-member is refused (403). A member of another
//     household gets 404 for any id that is not its own, and for a page with no
//     id sees only its own data. Neither response contains anything of DEV's.
//   - operator and settings routes: both are refused (403). Neither is an
//     operator, and these pages show no household data to anyone (ADR-00018).
//   - invitation routes: an invitation names the one email it is for, so both
//     identities get 404 for DEV's invitation, as for one that does not exist.
//   - public routes (/healthz, /static): reachable by both and tenant-free, so
//     the same checks apply: no marker of DEV's data in the response.
//   - the not-a-member page: shown to the non-member, and a member is sent home.
//
// The data needs a seeded recipe, so this spec starts its own gobbler on a data
// directory it seeds through the real store (sharing/seed) in dev-auth mode,
// with OPERATOR the only operator. DEV and OTHER are the households the dev
// bypass seeds.
const routesPort = Number(process.env.GOBBLER_E2E_TENANCY_PORT ?? 38541);
const deletePort = Number(process.env.GOBBLER_E2E_TENANCY_DELETE_PORT ?? 38542);
const baselinePort = Number(process.env.GOBBLER_E2E_TENANCY_BASELINE_PORT ?? 38543);

interface Probe {
  method: 'GET' | 'POST';
  /** The path to request, built from the seeded ids. */
  path: (ids: Ids) => string;
  form?: (ids: Ids) => Record<string, string>;
  /** Status the non-member must get. */
  stranger: number;
  /** Status a member of another household must get. */
  other: number;
  /** Where a redirect must lead, when the status is one. */
  otherLocation?: string;
}

const NOT_MEMBER = 403;
const NOT_FOUND = 404;

const own = (method: 'GET' | 'POST', path: string): Probe => ({ method, path: () => path, stranger: NOT_MEMBER, other: 200 });
const hidden = (method: 'GET' | 'POST', path: (ids: Ids) => string, form?: (ids: Ids) => Record<string, string>): Probe => ({
  method,
  path,
  form,
  stranger: NOT_MEMBER,
  other: NOT_FOUND,
});
const refusedToBoth = (method: 'GET' | 'POST', path: (ids: Ids) => string): Probe => ({
  method,
  path,
  stranger: NOT_MEMBER,
  other: NOT_MEMBER,
});
const unaddressed = (method: 'GET' | 'POST', path: (ids: Ids) => string): Probe => ({
  method,
  path,
  stranger: NOT_FOUND,
  other: NOT_FOUND,
});

// One probe per line: the gate pairs the method with the first path on the line.
const PROBES: Probe[] = [
  // Public: no household data, whoever asks.
  { method: 'GET', path: () => '/healthz', stranger: 200, other: 200 },
  { method: 'GET', path: () => '/static/app.css', stranger: 200, other: 200 },
  // The not-a-member page: shown to the non-member, a member is sent home.
  { method: 'GET', path: () => '/not-a-member', stranger: 200, other: 303, otherLocation: '/' },

  // Pages with no id: a member sees only its own household.
  own('GET', '/'),
  own('GET', '/recipes'),
  // The searches use words from DEV's data that the page does not echo back as a whole marker.
  own('GET', '/recipes?text=Pie'),
  own('GET', '/catalog'),
  own('GET', '/catalog?q=fruit'),
  own('GET', '/shopping'),
  own('GET', '/shopping/pantry'),
  own('GET', '/shopping/pantry?q=fruit'),
  own('GET', '/shopping/shopping.css'),
  own('GET', '/shopping/shopping.js'),
  own('GET', '/shares'),
  own('GET', '/shares/sent'),
  own('GET', '/household'),
  own('GET', '/ingest'),
  own('GET', '/ingest/photo'),
  hidden('GET', (i) => `/catalog/merge?source=${i.ingredient}&target=1`),

  // Operator and settings pages are refused to both: neither is an operator.
  refusedToBoth('GET', () => '/settings'),
  refusedToBoth('POST', () => '/settings/backup'),
  refusedToBoth('GET', () => '/operator'),
  refusedToBoth('GET', () => '/operator/promotions'),
  refusedToBoth('GET', (i) => `/operator/promotions/${i.promotion}`),
  refusedToBoth('POST', (i) => `/operator/promotions/${i.promotion}`),
  refusedToBoth('GET', () => '/operator/equipment'),
  refusedToBoth('POST', () => '/operator/equipment/1/sized'),
  refusedToBoth('POST', () => '/operator/invitations'),
  refusedToBoth('POST', (i) => `/operator/invitations/${i.operatorInvitation}/revoke`),

  // An invitation is addressed to one email: anyone else finds nothing.
  unaddressed('GET', (i) => `/invitations/${i.joinInvitation}`),
  unaddressed('POST', (i) => `/invitations/${i.joinInvitation}/accept`),
  unaddressed('GET', (i) => `/invitations/${i.operatorInvitation}`),

  // Recipes.
  hidden('GET', (i) => `/recipes/${i.recipe}`),
  hidden('GET', (i) => `/recipes/${i.recipe}/cook`),
  hidden('GET', (i) => `/recipes/${i.recipe}/edit`),
  hidden('POST', (i) => `/recipes/${i.recipe}/edit`, () => ({ version: '1', name: 'Hijacked' })),
  hidden('POST', (i) => `/recipes/${i.recipe}/clone`),
  hidden('GET', (i) => `/recipes/${i.recipe}/history`),
  hidden('GET', (i) => `/recipes/${i.recipe}/history/1`),
  hidden('POST', (i) => `/recipes/${i.recipe}/history/1/restore`, () => ({ confirm: 'yes' })),
  hidden('GET', (i) => `/recipes/${i.recipe}/journal`),
  hidden('POST', (i) => `/recipes/${i.recipe}/journal/notes`, () => ({ body: 'hijacked note' })),
  hidden('POST', (i) => `/recipes/${i.recipe}/journal/made`, () => ({ comment: 'hijacked comment' })),
  hidden('POST', (i) => `/recipes/${i.recipe}/journal/rating`, () => ({ rating: '1' })),

  // Catalog.
  hidden('GET', (i) => `/catalog/${i.ingredient}`),
  hidden('POST', (i) => `/catalog/${i.ingredient}`, () => ({ name: 'hijacked fruit', aisle: 'produce' })),
  hidden('POST', (i) => `/catalog/${i.ingredient}/reviewed`),
  hidden('POST', (i) => `/catalog/${i.ingredient}/suggest`),
  hidden('POST', (i) => `/catalog/${i.ingredient}/unsuggest`),
  hidden('POST', () => '/catalog/merge', (i) => ({ source: String(i.ingredient), target: '1' })),

  // Shopping.
  hidden('GET', (i) => `/shopping/${i.list}`),
  hidden('GET', (i) => `/shopping/${i.list}/live`),
  hidden('GET', (i) => `/shopping/${i.list}/events`),
  hidden('GET', (i) => `/shopping/${i.list}/export.txt`),
  // The key is one DEV has already checked: a different answer for it than for an
  // unchecked key would reveal what is checked on a list this household cannot see.
  hidden('POST', (i) => `/shopping/${i.list}/check`, (i) => ({ key: `i:${i.ingredient}`, checked: '1' })),
  hidden('POST', (i) => `/shopping/${i.list}/addback`, (i) => ({ ingredient: String(i.ingredient) })),
  hidden('POST', (i) => `/shopping/${i.list}/manual`, () => ({ text: 'hijacked item' })),
  hidden('POST', (i) => `/shopping/${i.list}/manual/${i.manual}/remove`),
  hidden('POST', (i) => `/shopping/${i.list}/optional`, () => ({ optional: '1' })),
  hidden('POST', (i) => `/shopping/${i.list}/recipes`, (i) => ({ recipe: String(i.recipe), multiplier: '1' })),
  hidden('POST', (i) => `/shopping/${i.list}/recipes/${i.entry}`, () => ({ multiplier: '2' })),
  hidden('POST', (i) => `/shopping/${i.list}/recipes/${i.entry}/remove`),
  hidden('POST', (i) => `/shopping/${i.list}/regenerate`),
  // A foreign ingredient is refused as a bad reference (400): there is no list to miss.
  { ...hidden('POST', () => '/shopping/pantry/add', (i) => ({ ingredient: String(i.ingredient) })), other: 400 },
  // Removing a pantry row that is not the household's finds nothing to remove.
  { ...hidden('POST', () => '/shopping/pantry/remove', (i) => ({ ingredient: String(i.ingredient) })), other: 303, otherLocation: '/shopping/pantry' },
  { ...hidden('POST', () => '/shopping', () => ({ name: 'Other weekly list' })), other: 303 },

  // Ingestion: a draft belongs to the household that made it.
  hidden('GET', (i) => `/ingest/${i.token}`),
  hidden('POST', (i) => `/ingest/${i.token}/confirm`),
  hidden('POST', (i) => `/ingest/${i.token}/retry`),
  // Cancelling someone else's draft discards nothing of theirs (checked afterwards).
  { ...hidden('POST', (i) => `/ingest/${i.token}/cancel`), other: 303, otherLocation: '/ingest' },
  { ...hidden('POST', () => '/ingest', () => ({ text: '1 cup other flour\nStir.' })), other: 303 },
  { ...hidden('POST', () => '/ingest/photo'), other: 400 },

  // Sharing: only the sender can send its recipe, and only the recipient can act on a share.
  hidden('GET', (i) => `/shares/new/${i.recipe}`),
  hidden('POST', (i) => `/shares/new/${i.recipe}`, () => ({ email: 'nobody@e2e.example' })),
  hidden('POST', (i) => `/shares/${i.share}/accept`),
  hidden('POST', (i) => `/shares/${i.share}/decline`),

  // Household: every action is on the caller's own household.
  { ...hidden('POST', () => '/household/delete', () => ({ confirm: DEV_HOUSEHOLD })), other: 400 },
  { ...hidden('POST', () => '/household/leave'), other: 409 },
  { ...hidden('POST', () => '/household/members/remove', () => ({ email: DEV })), other: NOT_FOUND },
  { ...hidden('POST', () => '/household/invitations', () => ({ email: uniqueEmail('invitee') })), other: 303 },
  hidden('POST', (i) => `/household/invitations/${i.joinInvitation}/revoke`),
];

let work: Workdir | undefined;
let routes: Server | undefined;
let ids: Ids | undefined;
let devCtx: APIRequestContext | undefined;
let opCtx: APIRequestContext | undefined;
let thirdCtx: APIRequestContext | undefined;
let strangerCtx: APIRequestContext | undefined;
let otherCtx: APIRequestContext | undefined;

test.describe('every route, as a non-member and as a member of another household', () => {
  test.describe.configure({ mode: 'serial', timeout: SETUP_TIMEOUT_MS });

  test.beforeAll(async ({ playwright }) => {
    test.setTimeout(SETUP_TIMEOUT_MS);
    work = makeWorkdir();
    routes = await startServer(work, routesPort, true);
    devCtx = await as(playwright, routes, DEV);
    opCtx = await as(playwright, routes, OPERATOR);
    strangerCtx = await as(playwright, routes, STRANGER);
    otherCtx = await as(playwright, routes, OTHER);
    const third = await startHousehold(playwright, routes, opCtx, 'Third kitchen');
    thirdCtx = third.ctx;
    ids = await populate(devCtx, opCtx, routes.recipe, third.email);
    ids.share = await pendingShare(thirdCtx);
  });

  test.afterAll(async () => {
    await Promise.all([devCtx, opCtx, thirdCtx, strangerCtx, otherCtx].map((c) => c?.dispose()));
    await stopServer(routes);
    removeWorkdir(work);
  });

  async function run(who: string, ctx: APIRequestContext | undefined, expected: (p: Probe) => number) {
    for (const p of PROBES) {
      const path = p.path(ids!);
      const res = await send(ctx!, p.method, path, p.form?.(ids!));
      const label = `${who}: ${p.method} ${path}`;
      expect.soft(res.status(), label).toBe(expected(p));
      expect.soft(leaks(await res.text()), `${label} leaked DEV data`).toEqual([]);
      expect.soft(leaks(res.headers().location ?? ''), `${label} redirects with DEV data`).toEqual([]);
      if (who === 'other' && p.otherLocation !== undefined) {
        expect.soft(new URL(res.headers().location ?? '', 'http://x').pathname, `${label} redirects`).toBe(p.otherLocation);
      }
    }
  }

  test('an authenticated non-member reaches nothing but the health check, static assets and the not-a-member page', async () => {
    await run('stranger', strangerCtx, (p) => p.stranger);
  });

  test('a member of another household sees and changes none of the dev household data', async () => {
    await run('other', otherCtx, (p) => p.other);
  });

  test('the dev household data is exactly as it was after both identities have tried everything', async () => {
    await expectUntouched(ids!, { dev: devCtx!, op: opCtx!, third: thirdCtx!, other: otherCtx! });
  });
});

test.describe('deleting a household leaves no rows anywhere', () => {
  test.describe.configure({ mode: 'serial', timeout: SETUP_TIMEOUT_MS });

  let deleteWork: Workdir | undefined;

  test.afterAll(() => {
    removeWorkdir(deleteWork);
  });

  test('the rows of a deleted household are gone from every table', async ({ playwright }) => {
    deleteWork = makeWorkdir();
    await expectHouseholdDeletionClean(playwright, deleteWork, { rich: deletePort, baseline: baselinePort });
  });
});

import { test, expect, type APIRequestContext } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { DEV, OTHER, OPERATOR, STRANGER } from '../fixtures/identity';
import { portsFromEnv } from '../fixtures/ports.mjs';
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
  populatePersonalKey,
  profileAddress,
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
//   - public routes (/healthz, /canada, /static; ADR-00029; and sign-in with its callback,
//     ADR-00030): reachable by both and tenant-free, so the same checks apply: no marker of
//     DEV's data in the response.
//   - sign-out: any identity may sign itself out, and is sent home.
//   - the not-a-member page: shown to the non-member, and a member is sent home.
//
// The data needs a seeded recipe, so this spec starts its own gobbler on a data
// directory it seeds through the real store (sharing/seed) in dev-auth mode,
// with OPERATOR the only operator. DEV and OTHER are the households the dev
// bypass seeds.
// Four distinct free ports, taken together so the kernel cannot hand one out
// twice; an environment variable still pins its server to a chosen port.
const [routesPort, deletePort, baselinePort, videoPort] = await portsFromEnv(process.env, [
  'GOBBLER_E2E_TENANCY_PORT',
  'GOBBLER_E2E_TENANCY_DELETE_PORT',
  'GOBBLER_E2E_TENANCY_BASELINE_PORT',
  'GOBBLER_E2E_TENANCY_VIDEO_PORT',
]);
const repoRoot = fileURLToPath(new URL('../..', import.meta.url));

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
// A profile address is 22 characters of base64url; this one was never issued.
const GUESSED_PROFILE = 'AAAAAAAAAAAAAAAAAAAAAA';

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
  { method: 'GET', path: () => '/canada', stranger: 200, other: 200 },
  // Sign-in (ADR-00030): public, and it reads no household. Starting one sends
  // anyone to the issuer; a callback with no sign-in in progress is refused.
  { method: 'GET', path: () => '/login', stranger: 303, other: 303 },
  { method: 'GET', path: () => '/auth/callback', stranger: 400, other: 400 },
  // Sign-out ends the requester's own session, member or not, and goes home.
  { method: 'POST', path: () => '/logout', stranger: 303, other: 303, otherLocation: '/' },
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
  hidden('POST', (i) => `/recipes/${i.recipe}/proposal/reject`, () => ({ proposal: 'a-token-nobody-holds' })),
  // This recipe has no video, so DEV gets 404 here too; the video suite below proves it with a stored thumbnail.
  hidden('GET', (i) => `/recipes/${i.recipe}/video/thumbnail`),
  // A recipe's picture and a draft's candidate (ADR-00039): the bytes are checked in internal/web/picture; the routes here must still refuse a non-member and find nothing for another household.
  hidden('GET', (i) => `/recipes/${i.recipe}/picture`),
  hidden('GET', () => `/pictures/drafts/1/1`),
  hidden('GET', (i) => `/recipes/${i.recipe}/history`),
  hidden('GET', (i) => `/recipes/${i.recipe}/history/1`),
  hidden('POST', (i) => `/recipes/${i.recipe}/history/1/restore`, () => ({ confirm: 'yes' })),
  hidden('GET', (i) => `/recipes/${i.recipe}/journal`),
  hidden('POST', (i) => `/recipes/${i.recipe}/journal/notes`, () => ({ body: 'hijacked note' })),
  hidden('POST', (i) => `/recipes/${i.recipe}/journal/made`, () => ({ comment: 'hijacked comment' })),
  hidden('POST', (i) => `/recipes/${i.recipe}/journal/rating`, () => ({ rating: '1' })),
  hidden('POST', (i) => `/recipes/${i.recipe}/journal/notes/1/pin`, () => ({ pinned: '1' })),
  hidden('GET', (i) => `/recipes/${i.recipe}/journal/notes/1/edit`),
  hidden('POST', (i) => `/recipes/${i.recipe}/journal/notes/1`, () => ({ body: 'hijacked note' })),
  hidden('GET', (i) => `/recipes/${i.recipe}/journal/notes/1/delete`),
  hidden('POST', (i) => `/recipes/${i.recipe}/journal/notes/1/delete`),

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
  hidden('POST', () => '/shopping/add', (i) => ({ recipe: String(i.recipe), multiplier: '1' })),
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
  // API keys (ADR-00031): a household's keys are its own. Another household's member makes
  // a key of its own household and cannot revoke DEV's, which is 404 as for any unknown id.
  { ...hidden('POST', () => '/household/keys', () => ({ name: 'other bot', lifetime: '30d' })), other: 200 },
  hidden('POST', (i) => `/household/keys/${i.key}/revoke`),

  // Profiles (ADR-00032): every signed-in person has one, so a non-member is admitted
  // where it is refused the rest. /profile sends each to their own profile. A page is
  // found only by its random address, so a guessed one finds nothing for either, on a
  // read and on a change; DEV's real address is checked in e2e/profile.
  { method: 'GET', path: () => '/profile', stranger: 303, other: 303 },
  unaddressed('GET', () => `/profile/${GUESSED_PROFILE}`),
  unaddressed('POST', () => `/profile/${GUESSED_PROFILE}/name`),
  unaddressed('GET', () => `/profile/${GUESSED_PROFILE}/sessions`),
  unaddressed('POST', () => `/profile/${GUESSED_PROFILE}/sessions/end`),
  unaddressed('POST', () => `/profile/${GUESSED_PROFILE}/sessions/end-others`),
  // Personal API keys (ADR-00031) are self-only. These probe DEV's real profile address and
  // real key, so the 404 comes from the self-only check and not from an address nobody has.
  unaddressed('GET', (i) => `/profile/${i.profile}/keys`),
  { ...unaddressed('POST', (i) => `/profile/${i.profile}/keys`), form: () => ({ name: 'Hijack', lifetime: '30d' }) },
  unaddressed('POST', (i) => `/profile/${i.profile}/keys/${i.personalKey}/revoke`),
  // Through OTHER's own profile the self-only check passes, so the key's owner is what refuses it
  // (and for STRANGER it is not their address, so the self-only check does).
  unaddressed('POST', (i) => `/profile/${i.otherProfile}/keys/${i.personalKey}/revoke`),
  // The avatar (ADR-00033): the picture is served to anyone the visibility rule shows
  // it to, the rest is the person's own; a guessed address finds nothing on any of them.
  unaddressed('GET', () => `/profile/${GUESSED_PROFILE}/avatar`),
  unaddressed('POST', () => `/profile/${GUESSED_PROFILE}/avatar`),
  unaddressed('GET', () => `/profile/${GUESSED_PROFILE}/avatar/crop`),
  unaddressed('POST', () => `/profile/${GUESSED_PROFILE}/avatar/crop`),
  unaddressed('GET', () => `/profile/${GUESSED_PROFILE}/avatar/preview/${GUESSED_PROFILE}`),
  unaddressed('GET', () => `/profile/${GUESSED_PROFILE}/avatar/status`),
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
    Object.assign(ids, await populatePersonalKey(devCtx));
    ids.otherProfile = await profileAddress(otherCtx);
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

  test('an authenticated non-member reaches nothing but the public paths and the not-a-member page', async () => {
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

// ADR-00021 obligation 4. A thumbnail is reached through its recipe, behind the
// household check. The main server's recipe has no video, so this suite starts a
// server on the recipe-view seed, whose first recipe names a video with a stored
// thumbnail, and asks for that image as its household and as the two other
// identities. The seed is built here and the server is started by the shared
// helper, which takes the recipe id from the seed's "recipe" key.
const THUMBNAIL_PROBE = hidden('GET', (i) => `/recipes/${i.recipe}/video/thumbnail`);

test.describe('a recipe thumbnail is served only to the household that owns the recipe', () => {
  test.describe.configure({ mode: 'serial', timeout: SETUP_TIMEOUT_MS });

  let videoWork: Workdir | undefined;
  let videoServer: Server | undefined;
  const ctxs: APIRequestContext[] = [];

  test.beforeAll(async ({ playwright }) => {
    test.setTimeout(SETUP_TIMEOUT_MS);
    const dir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-tenancy-video-'));
    const seed = join(dir, 'seed');
    execFileSync('go', ['build', '-o', seed, './e2e/recipeview/seed'], { cwd: repoRoot, stdio: 'inherit' });
    videoWork = { dir, seed };
    videoServer = await startServer(videoWork, videoPort, true);
    for (const who of [DEV, OTHER, STRANGER]) ctxs.push(await as(playwright, videoServer, who));
  });

  test.afterAll(async () => {
    await Promise.all(ctxs.map((c) => c.dispose()));
    await stopServer(videoServer);
    removeWorkdir(videoWork);
  });

  test('the owner gets the image, another household gets 404 and a non-member 403', async () => {
    const ids = { recipe: videoServer!.recipe } as Ids;
    const path = THUMBNAIL_PROBE.path(ids);
    const [dev, other, stranger] = ctxs;
    const own = await send(dev, THUMBNAIL_PROBE.method, path);
    expect(own.status(), 'the owning household').toBe(200);
    expect(own.headers()['content-type']).toBe('image/png');
    expect((await own.body()).length, 'the image body').toBeGreaterThan(0);

    const foreign = await send(other, THUMBNAIL_PROBE.method, path);
    expect(foreign.status(), 'another household').toBe(THUMBNAIL_PROBE.other);
    expect(foreign.headers()['content-type'] ?? '').not.toMatch(/^image\//);
    const outsider = await send(stranger, THUMBNAIL_PROBE.method, path);
    expect(outsider.status(), 'a non-member').toBe(THUMBNAIL_PROBE.stranger);
  });
});

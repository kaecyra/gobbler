import { test, expect, type APIRequestContext, type APIResponse, type Playwright } from '@playwright/test';
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { DatabaseSync } from 'node:sqlite';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { DEV, OTHER, OPERATOR, STRANGER, asIdentity } from '../fixtures/identity';

// Cross-household tests (ADR-00018 obligation 5). The membership check is the
// only lock between households, so every route the router registers is
// requested here by an authenticated non-member (STRANGER) and by a member of a
// household that does not hold the data (OTHER), against data that belongs to
// the dev household (DEV). The route-coverage gate (fixtures/route-coverage.mjs)
// fails when a route is missing from this file or this file stops naming either
// identity.
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
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

const repoRoot = fileURLToPath(new URL('../..', import.meta.url));
const routesPort = Number(process.env.GOBBLER_E2E_TENANCY_PORT ?? 38541);
const deletePort = Number(process.env.GOBBLER_E2E_TENANCY_DELETE_PORT ?? 38542);
const baselinePort = Number(process.env.GOBBLER_E2E_TENANCY_BASELINE_PORT ?? 38543);

// Strings that exist only in DEV's data. A response to anyone else that
// contains one is a leak.
const DEV_HOUSEHOLD = 'Dev household';
const LIST_NAME = 'Tenancy weekly list';
const MANUAL_ITEM = 'tenancy manual item';
const DRAFT_TEXT = 'tenancy draft cinnamon';
const INVITEE = 'tenancy-invitee@e2e.example';
const MARKERS = [
  'E2E Shared Pie',
  'e2e sender',
  LIST_NAME,
  MANUAL_ITEM,
  DRAFT_TEXT,
  INVITEE,
  DEV_HOUSEHOLD,
  DEV,
];

interface Server {
  dataDir: string;
  origin: string;
  child: ChildProcess;
}

async function startServer(port: number, seed: boolean): Promise<Server & { recipe: number }> {
  const origin = `http://127.0.0.1:${port}`;
  const stale = await fetch(`${origin}/healthz`).then(() => true, () => false);
  if (stale) throw new Error(`something already listens on ${origin}; set the GOBBLER_E2E_TENANCY*_PORT variables`);
  const dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-tenancy-'));
  let recipe = 0;
  if (seed) {
    const out = execFileSync('go', ['run', './e2e/sharing/seed', dataDir], { cwd: repoRoot, encoding: 'utf8' });
    ({ recipe } = JSON.parse(out));
  }
  const child = spawn(bin!, ['serve'], {
    env: {
      PATH: process.env.PATH ?? '',
      GOBBLER_LISTEN_ADDR: `127.0.0.1:${port}`,
      GOBBLER_DATA_DIR: dataDir,
      GOBBLER_LOG_LEVEL: 'warn',
      GOBBLER_DEV_AUTH_BYPASS: 'true',
      GOBBLER_OPERATOR_EMAILS: OPERATOR,
    },
    stdio: 'inherit',
  });
  const deadline = Date.now() + 20_000;
  for (;;) {
    try {
      if ((await fetch(`${origin}/healthz`)).ok) break;
    } catch {
      // not listening yet
    }
    if (Date.now() > deadline) {
      child.kill('SIGTERM');
      throw new Error(`the tenancy server on ${origin} did not start`);
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  return { dataDir, origin, child, recipe };
}

/** Stops the server and waits for it, so SQLite has checkpointed its WAL. */
async function stopServer(s: Server | undefined): Promise<void> {
  if (!s || s.child.exitCode !== null) return;
  const exited = new Promise((resolve) => s.child.once('exit', resolve));
  s.child.kill('SIGTERM');
  await exited;
}

function removeData(s: Server | undefined): void {
  if (s) rmSync(s.dataDir, { recursive: true, force: true });
}

function as(playwright: Playwright, s: Server, email: string): Promise<APIRequestContext> {
  return playwright.request.newContext({ baseURL: s.origin, ...asIdentity(email) });
}

/** One request that does not follow redirects, so a redirect is seen as one. */
function send(ctx: APIRequestContext, method: string, path: string, form?: Record<string, string>): Promise<APIResponse> {
  return ctx.fetch(path, { method, form, maxRedirects: 0, failOnStatusCode: false, timeout: 5_000 });
}

/** Sends a request that must succeed (2xx or a redirect) and returns it. */
async function ok(ctx: APIRequestContext, method: string, path: string, form?: Record<string, string>): Promise<APIResponse> {
  const res = await send(ctx, method, path, form);
  expect(res.status(), `${method} ${path}`).toBeLessThan(400);
  return res;
}

async function page(ctx: APIRequestContext, path: string): Promise<string> {
  const res = await send(ctx, 'GET', path);
  expect(res.status(), `GET ${path}`).toBe(200);
  return res.text();
}

/** The last capture of re in text, as a number. */
function lastId(text: string, re: RegExp, what: string): number {
  const all = [...text.matchAll(re)];
  if (all.length === 0) throw new Error(`no ${what} found on the page`);
  return Number(all[all.length - 1][1]);
}

let emailCounter = 0;
function uniqueEmail(label: string): string {
  emailCounter += 1;
  return `${label}-${process.pid}-${Date.now()}-${emailCounter}@e2e.example`;
}

interface Ids {
  recipe: number;
  list: number;
  entry: number;
  manual: number;
  ingredient: number;
  token: string;
  joinInvitation: number;
  operatorInvitation: number;
  promotion: number;
  share: number;
}

/**
 * Gives the dev household data of every kind a route can reach: a shopping list
 * with a recipe entry and a manual item, a pantry item, a suggested overlay
 * ingredient, a pending ingestion draft, a join invitation, and a share of the
 * seeded recipe to shareTo. The operator issues a household invitation. Returns
 * the ids the probes address.
 */
async function populate(dev: APIRequestContext, op: APIRequestContext, recipe: number, shareTo: string): Promise<Ids> {
  const created = await ok(dev, 'POST', '/shopping', { name: LIST_NAME });
  const list = Number(created.headers().location?.split('/').pop());
  expect(list).toBeGreaterThan(0);
  await ok(dev, 'POST', `/shopping/${list}/recipes`, { recipe: String(recipe), multiplier: '1' });
  await ok(dev, 'POST', `/shopping/${list}/manual`, { text: MANUAL_ITEM });
  const listPage = await page(dev, `/shopping/${list}`);
  const entry = lastId(listPage, /\/shopping\/\d+\/recipes\/(\d+)\/remove/g, 'recipe entry');
  const manual = lastId(listPage, /\/shopping\/\d+\/manual\/(\d+)\/remove/g, 'manual item');

  const catalog = await page(dev, '/catalog?q=e2e+sender');
  const ingredient = lastId(catalog, /href="\/catalog\/(\d+)">e2e sender fruit/g, 'overlay ingredient');
  await ok(dev, 'POST', '/shopping/pantry/add', { ingredient: String(ingredient) });
  await ok(dev, 'POST', `/catalog/${ingredient}/suggest`);
  const promotion = lastId(await page(op, '/operator/promotions'), /\/operator\/promotions\/(\d+)"/g, 'promotion');

  const draft = await ok(dev, 'POST', '/ingest', { text: `1 cup ${DRAFT_TEXT}\nMix it.` });
  const token = draft.headers().location?.split('/').pop() ?? '';
  expect(token).toMatch(/^[0-9a-f]+$/);

  await ok(dev, 'POST', '/household/invitations', { email: INVITEE });
  const joinInvitation = lastId(await page(dev, '/household'), /\/household\/invitations\/(\d+)\/revoke/g, 'join invitation');
  await ok(op, 'POST', '/operator/invitations', { email: uniqueEmail('founder') });
  const operatorInvitation = lastId(await page(op, '/operator'), /\/operator\/invitations\/(\d+)\/revoke/g, 'household invitation');

  await ok(dev, 'POST', `/shares/new/${recipe}`, { email: shareTo });
  return { recipe, list, entry, manual, ingredient, token, joinInvitation, operatorInvitation, promotion, share: 0 };
}

/** Starts a household named name for a new email, through the operator, and returns its member's context. */
async function startHousehold(playwright: Playwright, s: Server, op: APIRequestContext, name: string): Promise<{ email: string; ctx: APIRequestContext }> {
  const email = uniqueEmail('third');
  await ok(op, 'POST', '/operator/invitations', { email });
  const id = lastId(await page(op, '/operator'), /\/operator\/invitations\/(\d+)\/revoke/g, 'household invitation');
  const ctx = await as(playwright, s, email);
  await ok(ctx, 'POST', `/invitations/${id}/accept`, { name });
  return { email, ctx };
}

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

const own = (path: string): Probe => ({ method: 'GET', path: () => path, stranger: NOT_MEMBER, other: 200 });
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

const PROBES: Probe[] = [
  // Public: no household data, whoever asks.
  { method: 'GET', path: () => '/healthz', stranger: 200, other: 200 },
  { method: 'GET', path: () => '/static/app.css', stranger: 200, other: 200 },
  // The not-a-member page: shown to the non-member, a member is sent home.
  { method: 'GET', path: () => '/not-a-member', stranger: 200, other: 303, otherLocation: '/' },

  // Pages with no id: a member sees only its own household.
  own('/'),
  own('/recipes'),
  // The searches use words from DEV's data that the page does not echo back as a whole marker.
  own('/recipes?text=Pie'),
  own('/catalog'),
  own('/catalog?q=fruit'),
  own('/shopping'),
  own('/shopping/pantry'),
  own('/shopping/pantry?q=fruit'),
  own('/shopping/shopping.css'),
  own('/shopping/shopping.js'),
  own('/shares'),
  own('/shares/sent'),
  own('/household'),
  own('/ingest'),
  own('/ingest/photo'),
    hidden('GET', (i) => `/catalog/merge?source=${i.ingredient}&target=1`),

  // Operator and settings pages are refused to both: neither is an operator.
  refusedToBoth('GET', () => '/settings'),
  refusedToBoth('POST', () => '/settings/backup'),
  refusedToBoth('GET', () => '/operator'),
  refusedToBoth('GET', () => '/operator/promotions'),
  refusedToBoth('GET', (i) => `/operator/promotions/${i.promotion}`),
  refusedToBoth('POST', (i) => `/operator/promotions/${i.promotion}`),
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
  hidden('POST', (i) => `/shopping/${i.list}/check`, () => ({ key: 'i:1', checked: '1' })),
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

function leaks(text: string): string[] {
  return MARKERS.filter((m) => text.includes(m));
}

let routes: (Server & { recipe: number }) | undefined;
let ids: Ids | undefined;
let devCtx: APIRequestContext | undefined;
let opCtx: APIRequestContext | undefined;
let thirdCtx: APIRequestContext | undefined;
let strangerCtx: APIRequestContext | undefined;
let otherCtx: APIRequestContext | undefined;
let thirdEmail = '';

test.describe('every route, as a non-member and as a member of another household', () => {
  test.describe.configure({ mode: 'serial' });

  test.beforeAll(async ({ playwright }) => {
    routes = await startServer(routesPort, true);
    devCtx = await as(playwright, routes, DEV);
    opCtx = await as(playwright, routes, OPERATOR);
    strangerCtx = await as(playwright, routes, STRANGER);
    otherCtx = await as(playwright, routes, OTHER);
    const third = await startHousehold(playwright, routes, opCtx, 'Third kitchen');
    thirdCtx = third.ctx;
    thirdEmail = third.email;
    ids = await populate(devCtx, opCtx, routes.recipe, thirdEmail);
    ids.share = lastId(await page(thirdCtx, '/shares'), /\/shares\/(\d+)\/accept/g, 'share');
  });

  test.afterAll(async () => {
    await Promise.all([devCtx, opCtx, thirdCtx, strangerCtx, otherCtx].map((c) => c?.dispose()));
    await stopServer(routes);
    removeData(routes);
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
    const i = ids!;
    const dev = devCtx!;
    const recipe = await page(dev, `/recipes/${i.recipe}`);
    expect(recipe).toContain('E2E Shared Pie');
    expect(recipe).not.toContain('Hijacked');
    const journal = await page(dev, `/recipes/${i.recipe}/journal`);
    expect(journal).toContain('e2e sender private note');
    expect(journal).toContain('e2e sender private comment');
    expect(journal).not.toContain('hijacked');
    const list = await page(dev, `/shopping/${i.list}`);
    expect(list).toContain(LIST_NAME);
    expect(list).toContain(MANUAL_ITEM);
    expect(list).toContain(`/shopping/${i.list}/recipes/${i.entry}/remove`);
    expect(list).not.toContain('hijacked');
    const fruit = await page(dev, `/catalog/${i.ingredient}`);
    expect(fruit).toContain('e2e sender fruit');
    expect(fruit).not.toContain('hijacked');
    expect(await page(dev, '/shopping/pantry')).toContain('e2e sender fruit');
    expect(await page(dev, `/ingest/${i.token}`)).toContain(DRAFT_TEXT);
    const household = await page(dev, '/household');
    expect(household).toContain(INVITEE);
    expect(household).toContain(DEV);
    const op = await page(opCtx!, '/operator');
    expect(op).toContain(`/operator/invitations/${i.operatorInvitation}/revoke`);
    expect(await page(opCtx!, '/operator/promotions')).toContain(`/operator/promotions/${i.promotion}`);
    // The share is still pending for the household it was sent to.
    expect(await page(thirdCtx!, '/shares')).toContain(`/shares/${i.share}/accept`);
    // And the other household's own data is its own: it made one list and one draft.
    const others = await page(otherCtx!, '/shopping');
    expect(others).toContain('Other weekly list');
    expect(others).not.toContain(LIST_NAME);
  });
});

// Deleting a household (ADR-00018, Members) removes all of its data. The check
// does not list tables: it deletes a household that holds data of every kind
// from one server and an empty household from a second, identical server, then
// compares every table's row count. Anything the first deletion left behind
// shows as a difference. A scan of every text column for DEV's strings catches
// a row that survives under a different count (for example, one the baseline
// also holds).
test.describe('deleting a household leaves no rows anywhere', () => {
  test.describe.configure({ mode: 'serial' });

  let rich: (Server & { recipe: number }) | undefined;
  let baseline: Server | undefined;

  test.afterAll(async () => {
    await Promise.all([stopServer(rich), stopServer(baseline)]);
    removeData(rich);
    removeData(baseline);
  });

  function tables(db: DatabaseSync): { name: string; household: boolean; text: string[] }[] {
    const virtual = (db.prepare("SELECT name FROM sqlite_master WHERE type = 'table' AND sql LIKE 'CREATE VIRTUAL TABLE%'").all() as { name: string }[]).map((r) => r.name);
    const names = (db.prepare("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name").all() as { name: string }[])
      .map((r) => r.name)
      // A virtual table's shadow tables hold index fragments whose count depends on how the index was built, not on what it indexes.
      .filter((n) => !virtual.some((v) => n.startsWith(`${v}_`)));
    return names.map((name) => {
      const cols = db.prepare(`SELECT name, type FROM pragma_table_info('${name}')`).all() as { name: string; type: string }[];
      return {
        name,
        household: cols.some((c) => c.name === 'household_id'),
        text: cols.filter((c) => /char|text|clob/i.test(c.type) || c.type === '').map((c) => c.name),
      };
    });
  }

  function counts(db: DatabaseSync): Record<string, number> {
    return Object.fromEntries(tables(db).map((t) => [t.name, (db.prepare(`SELECT count(*) AS n FROM "${t.name}"`).get() as { n: number }).n]));
  }

  test('the rows of a deleted household are gone from every table', async ({ playwright }) => {
    rich = await startServer(deletePort, true);
    baseline = await startServer(baselinePort, false);
    const devRich = await as(playwright, rich, DEV);
    const opRich = await as(playwright, rich, OPERATOR);
    const devBase = await as(playwright, baseline, DEV);
    const opBase = await as(playwright, baseline, OPERATOR);
    try {
      // Both servers have the dev bypass households. The rich one gets data of
      // every kind, including a recipe share sent to the other household, whose
      // copy of the recipe would otherwise outlive its sender.
      await populate(devRich, opRich, rich.recipe, OTHER);
      await page(devBase, '/');
      // The operator's household invitation belongs to no household, so it
      // survives the deletion; the baseline holds one too.
      await ok(opBase, 'POST', '/operator/invitations', { email: uniqueEmail('founder') });

      const before = new DatabaseSync(join(rich.dataDir, 'gobbler.db'), { readOnly: true });
      const householdId = (before.prepare('SELECT id FROM households WHERE name = ?').get(DEV_HOUSEHOLD) as { id: number }).id;
      const held = tables(before).filter((t) => t.household);
      expect(held.map((t) => t.name), 'the household-scoped tables found by schema').not.toHaveLength(0);
      const populated = held.filter((t) => (before.prepare(`SELECT count(*) AS n FROM "${t.name}" WHERE household_id = ?`).get(householdId) as { n: number }).n > 0);
      // Without data in the main kinds of table the comparison below proves little.
      for (const name of ['recipes', 'shopping_lists', 'invitations']) {
        expect(populated.map((t) => t.name), `the dev household holds ${name} before deletion`).toContain(name);
      }
      before.close();

      for (const ctx of [devRich, devBase]) {
        const res = await send(ctx, 'POST', '/household/delete', { confirm: DEV_HOUSEHOLD });
        expect(res.status(), 'deleting the household').toBeLessThan(400);
        expect((await send(ctx, 'GET', '/recipes')).status(), 'the deleted household no longer admits its member').toBe(403);
      }
    } finally {
      await Promise.all([devRich, opRich, devBase, opBase].map((c) => c.dispose()));
    }
    await Promise.all([stopServer(rich), stopServer(baseline)]);

    const after = new DatabaseSync(join(rich.dataDir, 'gobbler.db'), { readOnly: true });
    const empty = new DatabaseSync(join(baseline!.dataDir, 'gobbler.db'), { readOnly: true });
    try {
      const householdsLeft = after.prepare('SELECT count(*) AS n FROM households WHERE name = ?').get(DEV_HOUSEHOLD) as { n: number };
      expect(householdsLeft.n, 'the household row').toBe(0);
      for (const t of tables(after).filter((t) => t.household)) {
        const gone = after.prepare(`SELECT count(*) AS n FROM "${t.name}" WHERE household_id NOT IN (SELECT id FROM households)`).get() as { n: number };
        expect(gone.n, `${t.name} rows whose household no longer exists`).toBe(0);
      }
      expect(counts(after), 'row counts after deleting a household with data match those after deleting an empty one').toEqual(counts(empty));
      for (const t of tables(after)) {
        for (const col of t.text) {
          for (const marker of [...MARKERS, 'tenancy']) {
            const hit = after.prepare(`SELECT count(*) AS n FROM "${t.name}" WHERE "${col}" LIKE ?`).get(`%${marker}%`) as { n: number };
            expect(hit.n, `${t.name}.${col} still holds ${JSON.stringify(marker)}`).toBe(0);
          }
        }
      }
    } finally {
      after.close();
      empty.close();
    }
  });
});

import { expect, type APIRequestContext, type APIResponse, type Playwright } from '@playwright/test';
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { DatabaseSync } from 'node:sqlite';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { freePorts } from '../ports.mjs';
import { DEV, OTHER, OPERATOR, asIdentity } from '../identity';

// Setup and verification for the cross-household specs (*.tenancy.spec.ts). It
// lives here, not in a spec, on purpose: the route-coverage gate counts the URL
// literals in a tenancy spec as probes, so everything that requests a URL for
// any other reason (seeding DEV's data, reading it back, deleting a household)
// is kept out of the specs. See fixtures/route-coverage.mjs.

const bin = process.env.GOBBLER_E2E_BIN;
const repoRoot = fileURLToPath(new URL('../../..', import.meta.url));

// Strings that exist only in DEV's data. A response to anyone else that
// contains one is a leak.
export const DEV_HOUSEHOLD = 'Dev household';
export const LIST_NAME = 'Tenancy weekly list';
export const MANUAL_ITEM = 'tenancy manual item';
export const DRAFT_TEXT = 'tenancy draft cinnamon';
export const INVITEE = 'tenancy-invitee@e2e.example';
export const MARKERS = ['E2E Shared Pie', 'e2e sender', LIST_NAME, MANUAL_ITEM, DRAFT_TEXT, INVITEE, DEV_HOUSEHOLD, DEV];

/** Time for a server, its seed and its data to come up inside a hook or test. */
export const SETUP_TIMEOUT_MS = 180_000;

export function leaks(text: string): string[] {
  return MARKERS.filter((m) => text.includes(m));
}

/** A temp directory holding the seed binary (built once) and every server's data. */
export interface Workdir {
  dir: string;
  seed: string;
}

export function makeWorkdir(): Workdir {
  const dir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-tenancy-'));
  const seed = join(dir, 'seed');
  execFileSync('go', ['build', '-o', seed, './e2e/sharing/seed'], { cwd: repoRoot, stdio: 'inherit' });
  return { dir, seed };
}

export function removeWorkdir(w: Workdir | undefined): void {
  if (w) rmSync(w.dir, { recursive: true, force: true });
}

export interface Server {
  dataDir: string;
  origin: string;
  child: ChildProcess;
  /** The id of the seeded recipe, or 0 when the server was not seeded. */
  recipe: number;
}

/**
 * Starts a server on port, or, if that port was taken, on a free one. The
 * kernel can still give a port to another test's outgoing connection between
 * the probe and the bind, and then the server exits at once with "address
 * already in use", which the health poll cannot tell from a slow start and
 * used to wait out its whole deadline.
 */
export async function startServer(w: Workdir, port: number, seed: boolean): Promise<Server> {
  if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');
  const stale = await fetch(`http://127.0.0.1:${port}/healthz`).then(() => true, () => false);
  if (stale) throw new Error(`something already listens on http://127.0.0.1:${port}; set the GOBBLER_E2E_TENANCY*_PORT variables`);
  const dataDir = mkdtempSync(join(w.dir, 'data-'));
  let recipe = 0;
  if (seed) ({ recipe } = JSON.parse(execFileSync(w.seed, [dataDir], { encoding: 'utf8' })));
  const attempts = 3;
  const failures: string[] = [];
  for (let attempt = 1; ; attempt++) {
    const s = await tryStart(dataDir, port, recipe);
    if ('child' in s) return s;
    failures.push(`port ${port}: ${s.exit}`);
    if (attempt === attempts) {
      throw new Error(`the tenancy server did not start after ${attempts} attempts (${failures.join('; ')}); its own log is above`);
    }
    [port] = await freePorts(1);
  }
}

/**
 * One start attempt: the server when healthy, or how the child exited before it
 * was. An early exit may be a taken port or a real crash; the caller retries
 * and reports every exit, so a crash is not mistaken for a collision.
 */
async function tryStart(dataDir: string, port: number, recipe: number): Promise<Server | { exit: string }> {
  const origin = `http://127.0.0.1:${port}`;
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
  let exited = false;
  let exit = '';
  child.once('exit', (code, signal) => {
    exited = true;
    exit = signal ? `exited on signal ${signal}` : `exited with code ${code}`;
  });
  const deadline = Date.now() + 20_000;
  for (;;) {
    // A healthy answer from a child that has already exited is the other
    // process that holds the port, not this server.
    if (exited) return { exit };
    try {
      if ((await fetch(`${origin}/healthz`)).ok && !exited) break;
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
export async function stopServer(s: Server | undefined): Promise<void> {
  if (!s || s.child.exitCode !== null) return;
  const exited = new Promise((resolve) => s.child.once('exit', resolve));
  s.child.kill('SIGTERM');
  await exited;
}

/** A request context whose every request is made by email. */
export function as(playwright: Playwright, s: Server, email: string): Promise<APIRequestContext> {
  return playwright.request.newContext({ baseURL: s.origin, ...asIdentity(email) });
}

/** One request that does not follow redirects, so a redirect is seen as one. */
export function send(ctx: APIRequestContext, method: string, path: string, form?: Record<string, string>): Promise<APIResponse> {
  return ctx.fetch(path, { method, form, maxRedirects: 0, failOnStatusCode: false, timeout: 5_000 });
}

/** Sends a request that must succeed (2xx or a redirect) and returns it. */
export async function ok(ctx: APIRequestContext, method: string, path: string, form?: Record<string, string>): Promise<APIResponse> {
  const res = await send(ctx, method, path, form);
  expect(res.status(), `${method} ${path}`).toBeLessThan(400);
  return res;
}

export async function page(ctx: APIRequestContext, path: string): Promise<string> {
  const res = await send(ctx, 'GET', path);
  expect(res.status(), `GET ${path}`).toBe(200);
  return res.text();
}

/** The last capture of re in text, as a number. */
export function lastId(text: string, re: RegExp, what: string): number {
  const all = [...text.matchAll(re)];
  if (all.length === 0) throw new Error(`no ${what} found on the page`);
  return Number(all[all.length - 1][1]);
}

let emailCounter = 0;
export function uniqueEmail(label: string): string {
  emailCounter += 1;
  return `${label}-${process.pid}-${Date.now()}-${emailCounter}@e2e.example`;
}

export interface Ids {
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
 * with a recipe entry, a manual item and a check, a pantry item, a suggested
 * overlay ingredient, a pending ingestion draft, a join invitation, a made-it
 * entry and a rating, and a share of the seeded recipe to shareTo. The operator
 * issues a household invitation. Returns the ids the probes address.
 */
export async function populate(dev: APIRequestContext, op: APIRequestContext, recipe: number, shareTo: string): Promise<Ids> {
  // One save through the editor adds what the seed lacks: tags, a line
  // qualifier, a second step that depends on the first, and equipment used in a
  // step. It keeps the seeded lines and the first step as they were.
  await ok(dev, 'POST', `/recipes/${recipe}/edit`, {
    version: '1',
    action: 'save',
    name: 'E2E Shared Pie',
    tags: 'pie, dessert',
    'c.0.name': '',
    'c.0.l.0.ing': 'onion',
    'c.0.l.0.qty': '1',
    'c.0.l.0.raw': '1 onion',
    'c.0.l.0.q.0.kind': 'preparation',
    'c.0.l.0.q.0.text': 'diced',
    'c.0.l.1.ing': 'e2e sender fruit',
    'c.0.l.1.qty': '3',
    'c.0.l.1.raw': '3 e2e sender fruit',
    'c.0.s.0.id': 'bake',
    'c.0.s.0.text': 'Bake the pie.',
    'c.0.s.0.to': '0',
    'c.0.s.1.id': 'serve',
    'c.0.s.1.text': 'Serve it.',
    'c.0.s.1.to': '0',
    'c.0.s.1.dep': 'bake',
    'e.0.name': 'oven',
    'e.0.qty': '1',
    'e.0.step': 'bake',
  });
  // Changing a central ingredient's aisle is stored as the household's own override.
  const onion = lastId(await page(dev, '/catalog?q=onion'), /href="\/catalog\/(\d+)">onion</g, 'central onion');
  await ok(dev, 'POST', `/catalog/${onion}`, { name: 'onion', aisle: 'bakery', aliases: '', density: '', usda_id: '' });

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
  // The fruit's line is checked off; the check probe posts this already-checked
  // key as another household (see the note there).
  expect(await page(dev, `/shopping/${list}/live`)).toContain(`data-key="i:${ingredient}"`);
  await ok(dev, 'POST', `/shopping/${list}/check`, { key: `i:${ingredient}`, checked: '1' });
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

  await ok(dev, 'POST', `/recipes/${recipe}/journal/made`, { comment: 'tenancy made-it' });
  await ok(dev, 'POST', `/recipes/${recipe}/journal/rating`, { rating: '4' });
  await ok(dev, 'POST', `/shares/new/${recipe}`, { email: shareTo });
  return { recipe, list, entry, manual, ingredient, token, joinInvitation, operatorInvitation, promotion, share: 0 };
}

/** Starts a household named name for a new email, through the operator, and returns its member's context. */
export async function startHousehold(playwright: Playwright, s: Server, op: APIRequestContext, name: string): Promise<{ email: string; ctx: APIRequestContext }> {
  const email = uniqueEmail('third');
  await ok(op, 'POST', '/operator/invitations', { email });
  const id = lastId(await page(op, '/operator'), /\/operator\/invitations\/(\d+)\/revoke/g, 'household invitation');
  const ctx = await as(playwright, s, email);
  await ok(ctx, 'POST', `/invitations/${id}/accept`, { name });
  return { email, ctx };
}

/** Reads the share id the recipient household was offered. */
export async function pendingShare(ctx: APIRequestContext): Promise<number> {
  return lastId(await page(ctx, '/shares'), /\/shares\/(\d+)\/accept/g, 'share');
}

/** DEV's data is exactly as populate left it, after other identities have tried to change it. */
export async function expectUntouched(ids: Ids, who: { dev: APIRequestContext; op: APIRequestContext; third: APIRequestContext; other: APIRequestContext }): Promise<void> {
  const { dev, op, third, other } = who;
  const recipe = await page(dev, `/recipes/${ids.recipe}`);
  expect(recipe).toContain('E2E Shared Pie');
  expect(recipe).not.toContain('Hijacked');
  const journal = await page(dev, `/recipes/${ids.recipe}/journal`);
  expect(journal).toContain('e2e sender private note');
  expect(journal).toContain('e2e sender private comment');
  expect(journal).not.toContain('hijacked');
  const list = await page(dev, `/shopping/${ids.list}`);
  expect(list).toContain(LIST_NAME);
  expect(list).toContain(MANUAL_ITEM);
  expect(list).toContain(`/shopping/${ids.list}/recipes/${ids.entry}/remove`);
  expect(list).not.toContain('hijacked');
  const fruit = await page(dev, `/catalog/${ids.ingredient}`);
  expect(fruit).toContain('e2e sender fruit');
  expect(fruit).not.toContain('hijacked');
  expect(await page(dev, '/shopping/pantry')).toContain('e2e sender fruit');
  expect(await page(dev, `/ingest/${ids.token}`)).toContain(DRAFT_TEXT);
  const household = await page(dev, '/household');
  expect(household).toContain(INVITEE);
  expect(household).toContain(DEV);
  expect(await page(op, '/operator')).toContain(`/operator/invitations/${ids.operatorInvitation}/revoke`);
  expect(await page(op, '/operator/promotions')).toContain(`/operator/promotions/${ids.promotion}`);
  // The share is still pending for the household it was sent to.
  expect(await page(third, '/shares')).toContain(`/shares/${ids.share}/accept`);
  // And the other household's own data is its own.
  const others = await page(other, '/shopping');
  expect(others).toContain('Other weekly list');
  expect(others).not.toContain(LIST_NAME);
}

interface Table {
  name: string;
  household: boolean;
  text: string[];
}

function tables(db: DatabaseSync): Table[] {
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

/**
 * Tables with a household_id column that populate() does not fill for the dev
 * household, each with the reason, so the deletion check is not vacuous for
 * them. A table here is still compared with the baseline, where it must also be
 * empty, so it cannot hide a leftover; it just is not evidence that deletion
 * removes a row of that kind. Add data to populate() rather than a name here
 * when an e2e request can create it.
 */
export const UNPOPULATED: Record<string, string> = {
  recipe_special_times:
    'no web form creates special times: the editor only preserves them and an LLM draft is the only source, which e2e never runs. ' +
    'Deleting a recipe empties the table (internal/store/recipe_test.go), and household deletion reaches it through the same cascade from recipes.',
};

/**
 * Deleting a household (ADR-00018, Members) removes all of its data. The check
 * does not list tables to look at. It deletes a household that holds data from
 * one server and the same household, empty, from a second identical server,
 * then compares every table's row count: anything the first deletion left
 * behind shows as a difference. Two guards keep that comparison from passing
 * vacuously. Before the delete, every table with a household_id column must
 * hold a row for the household, except those named in UNPOPULATED. A table
 * scoped by a different column name is not named by the schema check, but it is
 * still in the row-count comparison and in the text-column scan, which looks
 * for DEV's strings in every column of every table.
 */
export async function expectHouseholdDeletionClean(playwright: Playwright, w: Workdir, ports: { rich: number; baseline: number }): Promise<void> {
  let rich: Server | undefined;
  let baseline: Server | undefined;
  try {
    rich = await startServer(w, ports.rich, true);
    baseline = await startServer(w, ports.baseline, false);
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
      try {
        const householdId = (before.prepare('SELECT id FROM households WHERE name = ?').get(DEV_HOUSEHOLD) as { id: number }).id;
        const held = tables(before).filter((t) => t.household);
        expect(held.map((t) => t.name), 'the household-scoped tables found by schema').not.toHaveLength(0);
        const empty = held
          .filter((t) => (before.prepare(`SELECT count(*) AS n FROM "${t.name}" WHERE household_id = ?`).get(householdId) as { n: number }).n === 0)
          .map((t) => t.name);
        const unexpected = empty.filter((n) => !(n in UNPOPULATED));
        expect(unexpected, 'household-scoped tables holding no row for the dev household before deletion; fill them in populate() or name them in UNPOPULATED with a reason').toEqual([]);
        const stale = Object.keys(UNPOPULATED).filter((n) => !empty.includes(n));
        expect(stale, 'UNPOPULATED names a table that is now populated or gone; remove the entry').toEqual([]);
      } finally {
        before.close();
      }

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
    const emptyDb = new DatabaseSync(join(baseline.dataDir, 'gobbler.db'), { readOnly: true });
    try {
      const householdsLeft = after.prepare('SELECT count(*) AS n FROM households WHERE name = ?').get(DEV_HOUSEHOLD) as { n: number };
      expect(householdsLeft.n, 'the household row').toBe(0);
      for (const t of tables(after).filter((t) => t.household)) {
        const gone = after.prepare(`SELECT count(*) AS n FROM "${t.name}" WHERE household_id NOT IN (SELECT id FROM households)`).get() as { n: number };
        expect(gone.n, `${t.name} rows whose household no longer exists`).toBe(0);
      }
      expect(counts(after), 'row counts after deleting a household with data match those after deleting an empty one').toEqual(counts(emptyDb));
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
      emptyDb.close();
    }
  } finally {
    await Promise.all([stopServer(rich), stopServer(baseline)]);
  }
}

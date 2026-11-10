import { test, expect } from '@playwright/test';
import { META_PATH, startMcpServer, withKey, rpc, tool, toolError, type McpServer } from './support';

// The MCP endpoint (ADR-00034) driven with real API keys over Streamable HTTP:
// list the tools, search, read a recipe, put it on a shopping list, and see a
// recipe write wait for review. A real key's whole path runs: the bearer check,
// the membership check, the tool and the stores.

let server: McpServer;

test.beforeAll(async () => {
  server = await startMcpServer('GOBBLER_E2E_MCP_PORT');
});
test.afterAll(async () => {
  await server?.stop();
});

test('lists the tools, searches, reads a recipe and adds it to a list', async () => {
  const ctx = await withKey(server.origin, server.seed.devKey);

  const res = await rpc(ctx, 'tools/list');
  expect(res.status()).toBe(200);
  const names = (await res.json()).result.tools.map((t: { name: string }) => t.name);
  expect(names).toEqual(expect.arrayContaining(['search_recipes', 'get_recipe', 'create_shopping_list', 'add_recipe_to_shopping_list', 'import_recipe']));

  const found = await tool(ctx, 'search_recipes', { query: 'pancakes' });
  expect(found.items.map((r: { id: number }) => r.id)).toEqual([server.seed.pancakes]);

  const recipe = await tool(ctx, 'get_recipe', { id: server.seed.pancakes });
  expect(recipe.name).toBe('E2E MCP Pancakes');
  expect(recipe.components[0].ingredients[0].text).toContain('e2e mcp flour');

  const list = await tool(ctx, 'create_shopping_list', { name: 'Agent week' });
  await tool(ctx, 'add_recipe_to_shopping_list', { list_id: list.id, recipe_id: server.seed.pancakes, scale: '2' });
  const got = await tool(ctx, 'get_shopping_list', { id: list.id });
  const flour = got.items.find((i: { name: string }) => i.name === 'e2e mcp flour');
  expect(flour.amounts).toEqual(['4 cup']);
  await tool(ctx, 'check_shopping_item', { list_id: list.id, key: flour.key, checked: true });
  expect((await tool(ctx, 'get_shopping_list', { id: list.id })).items[0].checked).toBe(true);
  await ctx.dispose();
});

test('a household key acts for the household', async () => {
  const ctx = await withKey(server.origin, server.seed.householdKey);
  const all = await tool(ctx, 'list_recipes');
  expect(all.items.map((r: { id: number }) => r.id)).toContain(server.seed.pancakes);
  await ctx.dispose();
});

test('a recipe write is a draft with a review path, and saves nothing', async () => {
  const ctx = await withKey(server.origin, server.seed.devKey);
  const before = (await tool(ctx, 'list_recipes')).items.length;
  const draft = await tool(ctx, 'create_recipe_draft', { name: 'Agent toast', ingredients: ['2 slices bread'], steps: ['Toast the bread.'] });
  expect(draft.review_path).toMatch(/^\/ingest\/[0-9a-f]{32}$/);
  expect((await tool(ctx, 'list_recipes')).items.length).toBe(before);
  expect(await toolError(ctx, 'import_recipe', {})).toContain('url or text');
  await ctx.dispose();
});

test('an edit is a proposal: the recipe is unchanged until a person saves it, and the review page shows it', async () => {
  const ctx = await withKey(server.origin, server.seed.devKey);
  const id = server.seed.pancakes;
  const recipe = await tool(ctx, 'get_recipe', { id });
  const components = recipe.components.map((c: any) => ({
    name: c.name,
    ingredients: c.ingredients.map((l: any) => ({ key: l.key, text: l.text })),
    steps: c.steps.map((s: any) => ({ id: s.id, text: s.text, minutes: s.minutes, depends_on: s.depends_on })),
  }));
  expect(components[0].ingredients[0].key).toMatch(/^lin-/);

  const out = await tool(ctx, 'propose_recipe_edit', {
    id,
    version: recipe.version,
    reason: 'A thicker batter holds the blueberries.',
    recipe: { name: 'E2E MCP Pancakes, thicker', components },
  });
  expect(out.review_path).toBe(`/recipes/${id}/edit?proposal=${out.token}`);

  // Nothing was saved.
  const after = await tool(ctx, 'get_recipe', { id });
  expect(after.name).toBe('E2E MCP Pancakes');
  expect(after.version).toBe(recipe.version);
  const state = await tool(ctx, 'get_recipe_proposal', { token: out.token });
  expect(state).toMatchObject({ state: 'pending', stale: false, current_version: recipe.version, review_path: out.review_path });

  // A person opens the review path in the editor and sees the proposal.
  const person = await withKey(server.origin);
  const page = await person.get(out.review_path);
  expect(page.status()).toBe(200);
  const html = await page.text();
  expect(html).toContain('A thicker batter holds the blueberries.');
  expect(html).toContain('E2E MCP Pancakes, thicker');
  // A stale version, a missing reason and an unchanged recipe are refused.
  expect(await toolError(ctx, 'propose_recipe_edit', { id, version: recipe.version + 5, reason: 'x', recipe: { name: 'x', components } })).toContain('version');
  expect(await toolError(ctx, 'propose_recipe_edit', { id, version: recipe.version, reason: ' ', recipe: { name: 'x', components } })).toContain('reason');
  await person.dispose();
  await ctx.dispose();
});

test('an agent revises a held draft, and the review page shows its marks but not its reason', async () => {
  const ctx = await withKey(server.origin, server.seed.devKey);
  const created = await tool(ctx, 'create_recipe_draft', { name: 'Agent scones', ingredients: ['2 cups flour'], steps: ['Mix.', 'Bake.'] });
  const token = created.review_path.replace('/ingest/', '');
  const listed = await tool(ctx, 'list_recipe_drafts');
  expect(listed.items.find((d: any) => d.token === token)).toMatchObject({ name: 'Agent scones' });
  expect(listed.items.find((d: any) => d.token === token)).not.toHaveProperty('revised_by_agent');

  const held = await tool(ctx, 'get_recipe_draft', { token });
  expect(held.revision).toMatch(/^[0-9a-f]+$/);
  const draft = held.draft;
  draft.components[0].steps[1].text = 'Bake until golden.';
  const out = await tool(ctx, 'revise_recipe_draft', { token, revision: held.revision, draft, reason: 'House style names the doneness cue.' });
  expect(out.revision).not.toBe(held.revision);
  // The old revision is refused now that the draft has changed.
  expect(await toolError(ctx, 'revise_recipe_draft', { token, revision: held.revision, draft, reason: 'again' })).toContain('changed since you read it');

  const person = await withKey(server.origin);
  const page = await person.get(created.review_path);
  expect(page.status()).toBe(200);
  const html = await page.text();
  expect(html).not.toContain('House style names the doneness cue.');
  expect(html).toContain('Bake until golden.');
  expect(html).toContain('class="glitter"');
  await person.dispose();
  await ctx.dispose();
});

test('another household is not found, and an unknown list is refused', async () => {
  const ctx = await withKey(server.origin, server.seed.devKey);
  expect(await toolError(ctx, 'get_recipe', { id: server.seed.secret })).toBe('not found');
  expect(await toolError(ctx, 'get_shopping_list', { id: 999999 })).toBe('not found');
  await ctx.dispose();
});

test('a missing, unknown or expired key is 401 with WWW-Authenticate: Bearer and the resource metadata, never a redirect', async () => {
  for (const key of [undefined, 'gob_not-a-real-key', server.seed.shortKey]) {
    const ctx = await withKey(server.origin, key);
    const res = await rpc(ctx, 'tools/list');
    expect(res.status(), String(key)).toBe(401);
    expect(res.headers()['www-authenticate']).toBe(`Bearer resource_metadata="${server.origin}${META_PATH}"`);
    expect(res.headers()['location']).toBeUndefined();
    await ctx.dispose();
  }
});

test('the dev identity alone does not open the endpoint, and a key opens no HTML page', async () => {
  const dev = await withKey(server.origin, undefined, { 'X-Gobbler-Dev-Identity': 'dev@localhost' });
  expect((await rpc(dev, 'tools/list')).status()).toBe(401);
  await dev.dispose();

  const keyed = await withKey(server.origin, server.seed.devKey, { 'X-Gobbler-Dev-Identity': 'other@localhost' });
  // The page is served for the header's identity; the key plays no part in it.
  const page = await keyed.get('/recipes');
  expect(await page.text()).not.toContain('E2E MCP Pancakes');
  await keyed.dispose();
});

test('a key whose owner belongs to no household is refused', async () => {
  const ctx = await withKey(server.origin, server.seed.strangerKey);
  expect((await rpc(ctx, 'tools/list')).status()).toBe(403);
  await ctx.dispose();
});

test('production mode: no session means 401 Bearer on /mcp', async ({ request }) => {
  const anonymous = process.env.GOBBLER_E2E_ANONYMOUS_URL!;
  for (const method of ['GET', 'POST'] as const) {
    const res = await request.fetch(`${anonymous}/mcp`, { method, maxRedirects: 0 });
    expect(res.status()).toBe(401);
    // The production-mode server is configured with a public base URL.
    expect(res.headers()['www-authenticate']).toBe(`Bearer resource_metadata="https://gobbler.e2e.invalid${META_PATH}"`);
  }
});

test('a burst past the limit gets 429 with Retry-After', async () => {
  const ctx = await withKey(server.origin, server.seed.otherKey);
  const statuses: number[] = [];
  let limited;
  for (let i = 0; i < 80 && !limited; i++) {
    const res = await rpc(ctx, 'tools/list');
    statuses.push(res.status());
    if (res.status() === 429) limited = res;
  }
  expect(limited, `statuses ${statuses.join(',')}`).toBeDefined();
  expect(Number(limited!.headers()['retry-after'])).toBeGreaterThanOrEqual(1);
  await ctx.dispose();
});

import { test, expect, request as pwRequest, type APIRequestContext } from '@playwright/test';
import { OTHER, STRANGER, asIdentity } from '../fixtures/identity';
import { META_PATH, startMcpServer, withKey, rpc, tool, toolError, type McpServer } from './support';

// Cross-household tests for the MCP endpoint (ADR-00018 obligation 5,
// ADR-00034). The endpoint's identity is a bearer key, not the session, so the
// dev-auth identity header STRANGER and OTHER send opens nothing; and a real
// key of another household sees nothing of DEV's. The route gate reads the
// PROBES lines: a method literal followed by the path literal.

let server: McpServer;
const as = (email: string): Promise<APIRequestContext> => pwRequest.newContext({ baseURL: server.origin, ...asIdentity(email) });

test.beforeAll(async () => {
  server = await startMcpServer('GOBBLER_E2E_MCP_TENANCY_PORT');
});
test.afterAll(async () => {
  await server?.stop();
});

const PROBES = [
  { method: 'POST', path: '/mcp' },
  { method: 'GET', path: '/mcp' },
  { method: 'DELETE', path: '/mcp' },
];

async function expectRefused(ctx: APIRequestContext) {
  for (const p of PROBES) {
    const res = await ctx.fetch(p.path, { method: p.method, data: p.method === 'POST' ? { jsonrpc: '2.0', id: 1, method: 'tools/list' } : undefined, maxRedirects: 0 });
    expect(res.status(), `${p.method} ${p.path}`).toBe(401);
    expect(res.headers()['www-authenticate']).toBe(`Bearer resource_metadata="${server.origin}${META_PATH}"`);
    expect(await res.text()).not.toContain('E2E MCP');
  }
  await ctx.dispose();
}

test('a non-member with a session identity alone gets 401 Bearer on every method', async () => {
  await expectRefused(await as(STRANGER));
});

test('a member of another household with a session identity alone gets 401 Bearer on every method', async () => {
  await expectRefused(await as(OTHER));
});

test('the key of another household sees and changes none of DEV data', async () => {
  const other = await withKey(server.origin, server.seed.otherKey);
  const listed = await tool(other, 'list_recipes');
  expect(JSON.stringify(listed)).not.toContain('Pancakes');
  expect(await toolError(other, 'get_recipe', { id: server.seed.pancakes })).toBe('not found');

  const dev = await withKey(server.origin, server.seed.devKey);
  const list = await tool(dev, 'create_shopping_list', { name: 'DEV only list' });
  expect(await toolError(other, 'get_shopping_list', { id: list.id })).toBe('not found');
  expect(await toolError(other, 'add_manual_shopping_item', { list_id: list.id, text: 'hijack' })).toBe('not found');
  expect(await toolError(other, 'add_recipe_to_shopping_list', { list_id: list.id, recipe_id: server.seed.pancakes })).toBe('not found');
  // Nothing of the other household's reached DEV's list.
  expect((await tool(dev, 'get_shopping_list', { id: list.id })).items).toEqual([]);
  // The non-member's key is refused outright.
  const stranger = await withKey(server.origin, server.seed.strangerKey);
  expect((await rpc(stranger, 'tools/list')).status()).toBe(403);
  await Promise.all([other.dispose(), dev.dispose(), stranger.dispose()]);
});

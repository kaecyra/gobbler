import { test, expect, request as pwRequest } from '@playwright/test';
import { connectApp } from '../oauth/support';
import { META_PATH, startMcpServer, withKey, rpc, tool, toolError, type McpServer } from './support';

// The MCP endpoint (ADR-00034) driven with an OAuth access token: a test client
// completes the whole flow against the real binary (register, approve, exchange
// the code), then calls tools with the token it was given. The flow helpers are
// the OAuth specs' own.

let server: McpServer;

test.beforeAll(async () => {
  server = await startMcpServer('GOBBLER_E2E_MCP_OAUTH_PORT');
});
test.afterAll(async () => {
  await server?.stop();
});

test('a client completes the OAuth flow and then calls tools on /mcp with its token', async () => {
  // The dev identity (DEV) approves the client.
  const approver = await pwRequest.newContext({ baseURL: server.origin });
  const { tokens, resource } = await connectApp(approver, 'E2E MCP agent');
  expect(resource).toBe(`${server.origin}/mcp`);
  await approver.dispose();

  const ctx = await withKey(server.origin, tokens.access_token);
  const res = await rpc(ctx, 'tools/list');
  expect(res.status()).toBe(200);

  const found = await tool(ctx, 'search_recipes', { query: 'pancakes' });
  expect(found.items.map((r: { id: number }) => r.id)).toEqual([server.seed.pancakes]);
  expect((await tool(ctx, 'get_recipe', { id: server.seed.pancakes })).name).toBe('E2E MCP Pancakes');

  // A write through the token lands in the person's household and is readable.
  const list = await tool(ctx, 'create_shopping_list', { name: 'Agent via OAuth' });
  await tool(ctx, 'add_manual_shopping_item', { list_id: list.id, text: 'oat milk' });
  expect((await tool(ctx, 'get_shopping_list', { id: list.id })).items.length).toBe(1);

  // Another household's recipe is not found for this token.
  expect(await toolError(ctx, 'get_recipe', { id: server.seed.secret })).toBe('not found');
  await ctx.dispose();
});

test('a token the server did not issue is 401 with the resource metadata, which names this endpoint', async () => {
  for (const token of ['goa_not-a-real-token', undefined]) {
    const ctx = await withKey(server.origin, token);
    const res = await rpc(ctx, 'tools/list');
    expect(res.status()).toBe(401);
    expect(res.headers()['location']).toBeUndefined();
    const challenge = res.headers()['www-authenticate'];
    expect(challenge).toBe(`Bearer resource_metadata="${server.origin}${META_PATH}"`);
    await ctx.dispose();
  }
  const meta = await pwRequest.newContext({ baseURL: server.origin });
  const doc = await (await meta.get(META_PATH)).json();
  expect(doc.resource).toBe(`${server.origin}/mcp`);
  await meta.dispose();
});

test('a token does not open an HTML page', async () => {
  const approver = await pwRequest.newContext({ baseURL: server.origin });
  const { tokens } = await connectApp(approver, 'E2E MCP page probe');
  await approver.dispose();
  // The page is served for the header's identity (another household); the token plays no part.
  const ctx = await withKey(server.origin, tokens.access_token, { 'X-Gobbler-Dev-Identity': 'other@localhost' });
  expect(await (await ctx.get('/recipes')).text()).not.toContain('E2E MCP Pancakes');
  await ctx.dispose();
});

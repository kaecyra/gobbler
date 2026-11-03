import { request as pwRequest, type APIRequestContext, type APIResponse } from '@playwright/test';
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { portFromEnv } from '../fixtures/ports.mjs';

// A gobbler of the MCP specs' own, on a data directory seeded through the real
// stores (seed/main.go): dev-auth mode on loopback, throwaway data. The seed
// prints the ids and the API key secrets the specs use. Request helpers that
// carry the endpoint's path live here, not in a tenancy spec, so no path
// literal outside a probe counts as coverage there (route-coverage.mjs).

export interface Seed {
  pancakes: number;
  secret: number;
  devKey: string;
  otherKey: string;
  strangerKey: string;
  householdKey: string;
  shortKey: string;
}

export interface McpServer {
  origin: string;
  seed: Seed;
  stop(): Promise<void>;
}

const repoRoot = fileURLToPath(new URL('../..', import.meta.url));

export async function startMcpServer(portEnv: string): Promise<McpServer> {
  const bin = process.env.GOBBLER_E2E_BIN;
  if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');
  const port = await portFromEnv(process.env, portEnv);
  const origin = `http://127.0.0.1:${port}`;
  if (await fetch(`${origin}/healthz`).then(() => true, () => false)) throw new Error(`something already listens on ${origin}; set ${portEnv}`);
  const dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-mcp-'));
  const seed: Seed = JSON.parse(execFileSync('go', ['run', './e2e/mcp/seed', dataDir], { cwd: repoRoot, encoding: 'utf8' }));
  const server: ChildProcess = spawn(bin, ['serve'], {
    env: {
      PATH: process.env.PATH ?? '',
      GOBBLER_LISTEN_ADDR: `127.0.0.1:${port}`,
      GOBBLER_DATA_DIR: dataDir,
      GOBBLER_LOG_LEVEL: 'warn',
      GOBBLER_DEV_AUTH_BYPASS: 'true',
      GOBBLER_MCP_RATE: '1',
      GOBBLER_MCP_BURST: '40',
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
    if (Date.now() > deadline) throw new Error('the MCP server did not start');
    await new Promise((r) => setTimeout(r, 100));
  }
  return {
    origin,
    seed,
    async stop() {
      if (server.exitCode === null) {
        const exited = new Promise((resolve) => server.once('exit', resolve));
        server.kill('SIGTERM');
        await exited;
      }
      rmSync(dataDir, { recursive: true, force: true });
    },
  };
}

/** A request context that sends key as a bearer credential; none when key is undefined. */
export async function withKey(origin: string, key?: string, headers: Record<string, string> = {}): Promise<APIRequestContext> {
  return pwRequest.newContext({ baseURL: origin, extraHTTPHeaders: { ...(key ? { Authorization: `Bearer ${key}` } : {}), ...headers } });
}

const MCP_HEADERS = { 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream' };

/** POST one JSON-RPC request to the endpoint. */
export function rpc(ctx: APIRequestContext, method: string, params: unknown = {}): Promise<APIResponse> {
  return ctx.post('/mcp', { headers: MCP_HEADERS, data: { jsonrpc: '2.0', id: 1, method, params } });
}

/** Call a tool and return its structured result; throws on a protocol or tool error. */
export async function tool<T = any>(ctx: APIRequestContext, name: string, args: Record<string, unknown> = {}): Promise<T> {
  const res = await rpc(ctx, 'tools/call', { name, arguments: args });
  if (res.status() !== 200) throw new Error(`${name}: HTTP ${res.status()}`);
  const body = await res.json();
  if (body.error || body.result?.isError) throw new Error(`${name}: ${JSON.stringify(body.error ?? body.result.content)}`);
  return body.result.structuredContent as T;
}

/** Call a tool that is expected to fail and return the error text. */
export async function toolError(ctx: APIRequestContext, name: string, args: Record<string, unknown> = {}): Promise<string> {
  const res = await rpc(ctx, 'tools/call', { name, arguments: args });
  const body = await res.json();
  if (!body.result?.isError) throw new Error(`${name} did not fail: ${JSON.stringify(body)}`);
  return body.result.content.map((c: { text: string }) => c.text).join(' ');
}

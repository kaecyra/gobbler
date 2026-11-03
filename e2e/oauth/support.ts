import { expect, type APIRequestContext } from '@playwright/test';
import { createHash } from 'node:crypto';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';

// Helpers for the OAuth specs: a stand-in for an MCP client, which registers
// itself, sends a person to the authorization endpoint with PKCE and the
// resource, and trades the code for tokens (ADR-00034). The person's approval
// is the form post the consent page makes, so a spec that does not look at the
// page can make a grant in a few calls.

/** A PKCE code verifier (43 characters of the unreserved set). */
export const VERIFIER = 'dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk';
/** A loopback redirect URI, which the registration accepts and a browser can be made to fulfil. */
export const REDIRECT = 'http://127.0.0.1:41699/callback';

export function challengeOf(verifier: string): string {
  return createHash('sha256').update(verifier).digest('base64url');
}

/** The resource the server issues tokens for, from its Protected Resource Metadata. */
export async function resourceOf(request: APIRequestContext): Promise<string> {
  const res = await request.get('/.well-known/oauth-protected-resource/mcp');
  expect(res.status()).toBe(200);
  return (await res.json()).resource as string;
}

/** Registers a client by Dynamic Client Registration and returns its id. */
export async function registerClient(request: APIRequestContext, name = 'E2E agent', redirect = REDIRECT): Promise<string> {
  const res = await request.post('/oauth/register', {
    data: { client_name: name, redirect_uris: [redirect], token_endpoint_auth_method: 'none' },
  });
  expect(res.status()).toBe(201);
  return (await res.json()).client_id as string;
}

/** The parameters of a valid authorization request. */
export function authParams(clientId: string, resource: string, redirect = REDIRECT): Record<string, string> {
  return {
    response_type: 'code',
    client_id: clientId,
    redirect_uri: redirect,
    state: 'st4te',
    code_challenge: challengeOf(VERIFIER),
    code_challenge_method: 'S256',
    resource,
  };
}

/** Posts the consent form as the request's identity and returns where it redirects. */
export async function decide(request: APIRequestContext, params: Record<string, string>, decision: 'allow' | 'deny'): Promise<URL> {
  const res = await request.post('/oauth/authorize', { form: { ...params, decision }, maxRedirects: 0, failOnStatusCode: false });
  expect(res.status(), await res.text()).toBe(303);
  return new URL(res.headers()['location']);
}

/** The form that trades a code for tokens. */
export function codeForm(clientId: string, code: string, resource: string, redirect = REDIRECT): Record<string, string> {
  return { grant_type: 'authorization_code', client_id: clientId, code, redirect_uri: redirect, code_verifier: VERIFIER, resource };
}

export interface Tokens {
  access_token: string;
  refresh_token: string;
  token_type: string;
  expires_in: number;
}

/** Registers a client, has the identity of request approve it, and exchanges the code. */
export async function connectApp(request: APIRequestContext, name = 'E2E agent'): Promise<{ clientId: string; resource: string; tokens: Tokens }> {
  const resource = await resourceOf(request);
  const clientId = await registerClient(request, name);
  const back = await decide(request, authParams(clientId, resource), 'allow');
  const code = back.searchParams.get('code');
  expect(code).toBeTruthy();
  const res = await request.post('/oauth/token', { form: codeForm(clientId, code!, resource) });
  expect(res.status(), await res.text()).toBe(200);
  return { clientId, resource, tokens: (await res.json()) as Tokens };
}

/**
 * A stand-in for the client's own redirect URI, on a free loopback port, so a
 * browser that follows the redirect has somewhere to land. close() stops it.
 */
export async function startCallback(): Promise<{ redirect: string; host: string; close: () => Promise<void> }> {
  const server: Server = createServer((_req, res) => {
    res.writeHead(200, { 'Content-Type': 'text/plain' });
    res.end('client callback');
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const { port } = server.address() as AddressInfo;
  return {
    redirect: `http://127.0.0.1:${port}/callback`,
    host: `127.0.0.1:${port}`,
    close: () => new Promise<void>((resolve) => server.close(() => resolve())),
  };
}

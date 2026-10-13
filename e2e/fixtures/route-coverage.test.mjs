import assert from 'node:assert/strict';
import { test } from 'node:test';
import { extractURLs, matchesPattern, stripComments, tenancyUncovered, uncovered } from './route-coverage.mjs';

test('a literal pattern matches only the same path', () => {
  assert.ok(matchesPattern('/healthz', '/healthz'));
  assert.ok(!matchesPattern('/healthz', '/health'));
  assert.ok(!matchesPattern('/health', '/healthz'));
  assert.ok(!matchesPattern('/health', '/healthz/x'));
});

test('the root pattern matches only the root', () => {
  assert.ok(matchesPattern('/', '/'));
  assert.ok(!matchesPattern('/', '/healthz'));
});

test('a trailing slash does not change the path', () => {
  assert.ok(matchesPattern('/recipes', '/recipes/'));
  assert.ok(matchesPattern('/recipes/', '/recipes'));
});

test('a {param} matches exactly one segment', () => {
  assert.ok(matchesPattern('/recipes/{id}', '/recipes/42'));
  assert.ok(matchesPattern('/recipes/{id:[0-9]+}', '/recipes/42'));
  assert.ok(!matchesPattern('/recipes/{id}', '/recipes'));
  assert.ok(!matchesPattern('/recipes/{id}', '/recipes/42/edit'));
  assert.ok(matchesPattern('/recipes/{id}/edit', '/recipes/42/edit'));
});

test('a trailing /* matches the rest of the path', () => {
  assert.ok(matchesPattern('/static/*', '/static/app.css'));
  assert.ok(matchesPattern('/static/*', '/static/css/app.css'));
  assert.ok(!matchesPattern('/static/*', '/staticfile'));
  assert.ok(!matchesPattern('/static/*', '/other/app.css'));
});

test('extractURLs finds path literals, drops query and hash, and fills template holes', () => {
  const urls = extractURLs(
    `get('/a?x=1'); goto("/b#top"); goto(\`/recipes/\${id}/edit\`); const s = 'not a path'; get('//evil')`,
  );
  assert.deepEqual(urls.sort(), ['/a', '/b', '/recipes/x/edit']);
});

test('uncovered lists patterns no URL reaches, once each', () => {
  const routes = [
    { method: 'GET', pattern: '/healthz' },
    { method: 'GET', pattern: '/other' },
    { method: 'POST', pattern: '/other' },
    { method: 'GET', pattern: '/static/*' },
  ];
  assert.deepEqual(uncovered(routes, ['/healthz', '/static/app.css']), ['/other']);
});

test('stripComments drops block and line comments but keeps code', () => {
  const code = stripComments("/* STRANGER */\n// OTHER here\nconst a = asIdentity(OTHER); // STRANGER too\n");
  assert.ok(/\bOTHER\b/.test(code));
  assert.ok(!/\bSTRANGER\b/.test(code));
});

const tenancyRoutes = [
  { method: 'GET', pattern: '/recipes/{id}' },
  { method: 'POST', pattern: '/recipes/{id}/clone' },
];
const both = "for (const who of [STRANGER, OTHER]) { get(`/recipes/${id}`); post(`/recipes/${id}/clone`); }";

test('tenancyUncovered passes a route requested by both identities', () => {
  assert.deepEqual(tenancyUncovered(tenancyRoutes, [{ text: both }]), []);
});

test('tenancyUncovered fails a route no tenancy spec requests', () => {
  const text = "for (const who of [STRANGER, OTHER]) { get(`/recipes/${id}`); }";
  const got = tenancyUncovered(tenancyRoutes, [{ text }]);
  assert.deepEqual(got.map((g) => g.pattern), ['/recipes/{id}/clone']);
  assert.match(got[0].missing[0], /request/);
});

test('tenancyUncovered fails a spec that omits the non-member identity', () => {
  const got = tenancyUncovered(tenancyRoutes, [{ text: both.replace('STRANGER, ', '') }]);
  assert.equal(got.length, 2);
  for (const g of got) assert.deepEqual(g.missing, ['the non-member identity STRANGER']);
});

test('tenancyUncovered fails a spec that omits the other-household identity', () => {
  const got = tenancyUncovered(tenancyRoutes, [{ text: both.replace(', OTHER', '') }]);
  assert.equal(got.length, 2);
  for (const g of got) assert.deepEqual(g.missing, ['the other-household member identity OTHER']);
});

test('tenancyUncovered does not count an identity named only in a comment', () => {
  const text = '// STRANGER and OTHER\nget(`/recipes/${id}`); post(`/recipes/${id}/clone`);';
  const got = tenancyUncovered(tenancyRoutes, [{ text }]);
  assert.equal(got.length, 2);
  for (const g of got) assert.equal(g.missing.length, 2);
});

test('tenancyUncovered takes the identities from a spec that requests the route, not from another spec', () => {
  const urls = { text: "get(`/recipes/${id}`); post(`/recipes/${id}/clone`);" };
  const ids = { text: 'asIdentity(STRANGER); asIdentity(OTHER);' };
  assert.equal(tenancyUncovered(tenancyRoutes, [urls, ids]).length, 2);
});

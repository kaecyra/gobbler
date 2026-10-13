import assert from 'node:assert/strict';
import { test } from 'node:test';
import { extractProbes, extractURLs, matchesPattern, resolveRoute, stripComments, tenancyStray, tenancyUncovered, uncovered } from './route-coverage.mjs';

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

test('extractProbes pairs a method with the first path on its line, and reports paths on no probe line', () => {
  const text = [
    "import { STRANGER } from '/not/a/path';",
    "hidden('POST', (i) => `/recipes/${i.id}/clone`, () => ({ next: '/elsewhere' })),",
    "{ method: 'GET', path: () => '/healthz', other: 303, otherLocation: '/' },",
    "await ok(dev, `/shopping/${list}`);",
  ].join('\n');
  const got = extractProbes(text);
  assert.deepEqual(got.probes, [
    { method: 'POST', url: '/recipes/x/clone' },
    { method: 'GET', url: '/healthz' },
  ]);
  assert.deepEqual(got.stray, ['/shopping/x']);
});

const routes = [
  { method: 'GET', pattern: '/recipes/{id}' },
  { method: 'POST', pattern: '/recipes/{id}' },
  { method: 'POST', pattern: '/recipes/{id}/clone' },
];
const identities = "strangerCtx = await as(playwright, server, STRANGER);\notherCtx = await as(playwright, server, OTHER);";
const probes = [
  "hidden('GET', (i) => `/recipes/${i.recipe}`),",
  "hidden('POST', (i) => `/recipes/${i.recipe}`),",
  "hidden('POST', (i) => `/recipes/${i.recipe}/clone`),",
].join('\n');
const spec = (...parts) => ({ path: 'x.tenancy.spec.ts', text: parts.join('\n') });

test('tenancyUncovered passes every route probed with its method by a spec using both identities', () => {
  assert.deepEqual(tenancyUncovered(routes, [spec(probes, identities)]), []);
});

test('tenancyUncovered fails a route no probe reaches', () => {
  const got = tenancyUncovered(routes, [spec(probes.split('\n').slice(0, 2).join('\n'), identities)]);
  assert.deepEqual(got.map((g) => `${g.method} ${g.pattern}`), ['POST /recipes/{id}/clone']);
  assert.match(got[0].missing[0], /POST probe/);
});

test('tenancyUncovered compares methods: a GET probe does not cover a POST-only route', () => {
  const got = tenancyUncovered(routes, [spec("hidden('GET', (i) => `/recipes/${i.recipe}/clone`),", probes.split('\n').slice(0, 2).join('\n'), identities)]);
  assert.deepEqual(got.map((g) => `${g.method} ${g.pattern}`), ['POST /recipes/{id}/clone']);
});

test('tenancyUncovered covers a route registered for every method with a probe of any method', () => {
  const all = ['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS', 'CONNECT', 'TRACE', 'QUERY'].map((method) => ({ method, pattern: '/static/*' }));
  assert.deepEqual(tenancyUncovered(all, [spec("{ method: 'GET', path: () => '/static/app.css' },", identities)]), []);
  assert.equal(tenancyUncovered(all, [spec(identities)]).length, 1);
});

test('tenancyUncovered does not count a path that is not on a probe line', () => {
  const setup = "await ok(dev, `/recipes/${id}/clone`);";
  const got = tenancyUncovered(routes, [spec(probes.split('\n').slice(0, 2).join('\n'), setup, identities)]);
  assert.deepEqual(got.map((g) => g.pattern), ['/recipes/{id}/clone']);
  assert.equal(tenancyStray([spec(setup)])[0].urls[0], '/recipes/x/clone');
});

test('tenancyUncovered fails a spec that never passes STRANGER or OTHER to as(...)', () => {
  const noStranger = tenancyUncovered(routes, [spec(probes, "as(playwright, server, OTHER);")]);
  assert.equal(noStranger.length, 3);
  for (const g of noStranger) assert.match(g.missing.join(), /STRANGER/);
  const noOther = tenancyUncovered(routes, [spec(probes, "as(playwright, server, STRANGER);")]);
  assert.equal(noOther.length, 3);
  for (const g of noOther) assert.match(g.missing.join(), /OTHER/);
});

test('tenancyUncovered does not count an import, a comment or a bare mention as using an identity', () => {
  const text = [
    "import { OTHER, STRANGER } from '../fixtures/identity';",
    '// as(playwright, server, STRANGER) and as(playwright, server, OTHER)',
    'const who = [STRANGER, OTHER];',
    probes,
  ].join('\n');
  const got = tenancyUncovered(routes, [spec(text)]);
  assert.equal(got.length, 3);
  for (const g of got) assert.equal(g.missing.length, 2);
});

test('tenancyUncovered takes the identities from the spec that holds the probe, not from another spec', () => {
  assert.equal(tenancyUncovered(routes, [spec(probes), spec(identities)]).length, 3);
});

test('a request helper taking the method second is not a probe', () => {
  const got = extractProbes("await send(ctx, 'POST', `/shopping/${id}/regenerate`);\nhidden('POST', (i) => `/x`);");
  assert.deepEqual(got.probes, [{ method: 'POST', url: '/x' }]);
  assert.deepEqual(got.stray, ['/shopping/x/regenerate']);
});

test('resolveRoute picks the most specific pattern: literal over {param} over *', () => {
  const patterns = ['/catalog/{id}', '/catalog/merge', '/static/*', '/{x}/*'];
  assert.equal(resolveRoute(patterns, '/catalog/merge'), '/catalog/merge');
  assert.equal(resolveRoute(patterns, '/catalog/7'), '/catalog/{id}');
  assert.equal(resolveRoute(patterns, '/static/app.css'), '/static/*');
  assert.equal(resolveRoute(patterns, '/nothing/here'), '/{x}/*');
  assert.equal(resolveRoute(['/a'], '/b'), null);
});

test('a probe of a literal sibling does not cover the {param} route beside it', () => {
  const sibling = [
    { method: 'GET', pattern: '/catalog/{id}' },
    { method: 'GET', pattern: '/catalog/merge' },
  ];
  const onlyMerge = spec("hidden('GET', (i) => `/catalog/merge`),", identities);
  assert.deepEqual(tenancyUncovered(sibling, [onlyMerge]).map((g) => g.pattern), ['/catalog/{id}']);
  const both = spec("hidden('GET', (i) => `/catalog/merge`),\nhidden('GET', (i) => `/catalog/${i.id}`),", identities);
  assert.deepEqual(tenancyUncovered(sibling, [both]), []);
});

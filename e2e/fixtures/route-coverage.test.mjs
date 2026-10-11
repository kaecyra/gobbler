import assert from 'node:assert/strict';
import { test } from 'node:test';
import { extractURLs, matchesPattern, uncovered } from './route-coverage.mjs';

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

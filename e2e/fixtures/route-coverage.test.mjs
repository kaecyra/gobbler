import assert from 'node:assert/strict';
import { test } from 'node:test';
import { findRoutes, uncovered } from './route-coverage.mjs';

const go = (text) => [{ name: 'x.go', text }];

test('findRoutes reads literals and resolves path constants', () => {
  const routes = findRoutes(go(`const HealthPath = "/healthz"
r.Get(HealthPath, h)
r.Get("/", home)
r.Handle("/static/*", s)
r.Mount("/recipes", x)`));
  assert.deepEqual(routes.map((r) => r.path).sort(), ['/', '/healthz', '/recipes', '/static/*']);
});

test('findRoutes ignores commented-out registrations', () => {
  assert.deepEqual(findRoutes(go('// r.Mount("/recipes", x)\nr.Get("/a", h)')).map((r) => r.path), ['/a']);
});

test('findRoutes refuses a path it cannot resolve', () => {
  assert.throws(() => findRoutes(go('r.Get(somePath, h)')), /cannot resolve the route path somePath/);
});

test('uncovered names a route no spec requests', () => {
  const routes = findRoutes(go('r.Get("/healthz", h)\nr.Get("/other", h)'));
  const missing = uncovered(routes, [`await request.get('/healthz')`]);
  assert.deepEqual(missing.map((r) => r.path), ['/other']);
});

test('an exact route needs an exact literal, not a longer path', () => {
  const routes = findRoutes(go('r.Get("/", h)'));
  assert.equal(uncovered(routes, [`page.goto('/healthz')`]).length, 1);
  assert.equal(uncovered(routes, [`page.goto('/')`]).length, 0);
});

test('wildcard, param and mount routes match by prefix', () => {
  const routes = findRoutes(go('r.Handle("/static/*", s)\nr.Get("/recipes/{id}", h)\nr.Mount("/lists", x)'));
  const specs = [`get('/static/app.css')`, 'goto(`/recipes/1`)', `goto('/lists/3')`];
  assert.deepEqual(uncovered(routes, specs), []);
  assert.equal(uncovered(routes, []).length, 3);
});

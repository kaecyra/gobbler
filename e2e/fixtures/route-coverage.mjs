// Route-coverage check (ADR-00013 obligation 2): every route the real
// internal/web router registers has at least one e2e spec that requests it.
//
// The routes come from fixtures/routes (a Go program that builds the router and
// walks it with chi, so mounted feature routes appear with full paths). A spec
// covers a route when it contains a string literal URL that matches the route
// pattern segment by segment: a literal segment matches itself, {param} matches
// exactly one segment, and a trailing /* matches the rest. Specs must write the
// full path in one literal or template string (`/recipes/${id}`), not build it
// by concatenation. Methods are not compared: a pattern is covered by a request
// to it with any method.
import { execFileSync } from 'node:child_process';
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

function segments(path) {
  return path.split('/').filter((s) => s !== '');
}

export function matchesPattern(pattern, url) {
  const p = segments(pattern);
  const u = segments(url);
  for (let i = 0; i < p.length; i++) {
    if (p[i] === '*' && i === p.length - 1) return true;
    if (i >= u.length) return false;
    if (p[i].startsWith('{') && p[i].endsWith('}')) continue;
    if (p[i] !== u[i]) return false;
  }
  return p.length === u.length;
}

/** String literals in a spec that look like same-origin paths, normalised. */
export function extractURLs(specText) {
  const urls = [];
  for (const m of specText.matchAll(/(["'`])(\/(?:(?!\1)[^\n\\])*)\1/g)) {
    let url = m[2].replace(/\$\{[^}]*\}/g, 'x');
    url = url.split(/[?#]/)[0];
    if (url.startsWith('//')) continue;
    urls.push(url);
  }
  return urls;
}

/** Patterns (deduplicated across methods) that no URL reaches. */
export function uncovered(routes, urls) {
  const patterns = [...new Set(routes.map((r) => r.pattern))];
  return patterns.filter((p) => !urls.some((u) => matchesPattern(p, u)));
}

function walk(dir) {
  const out = [];
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    if (e.name === 'node_modules') continue;
    const p = join(dir, e.name);
    if (e.isDirectory()) out.push(...walk(p));
    else if (e.name.endsWith('.spec.ts')) out.push(p);
  }
  return out;
}

function loadRoutes(repoRoot) {
  const out = execFileSync('go', ['run', './e2e/fixtures/routes'], {
    cwd: repoRoot,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'inherit'],
  });
  return JSON.parse(out);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const repoRoot = fileURLToPath(new URL('../..', import.meta.url));
  const e2eDir = fileURLToPath(new URL('..', import.meta.url));
  const routes = loadRoutes(repoRoot);
  if (routes.length === 0) {
    console.error('the router reports no routes; the check cannot be trusted');
    process.exit(1);
  }
  const urls = walk(e2eDir).flatMap((p) => extractURLs(readFileSync(p, 'utf8')));
  const missing = uncovered(routes, urls);
  if (missing.length > 0) {
    for (const p of missing) console.error(`route ${p} has no e2e test under e2e/`);
    process.exit(1);
  }
  console.log(`route coverage: ${[...new Set(routes.map((r) => r.pattern))].join(', ')}`);
}

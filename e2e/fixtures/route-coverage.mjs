// Route-coverage check (ADR-00013 obligation 2): every route the real
// internal/web router registers has at least one e2e spec that requests it, and
// a cross-household spec (ADR-00018 obligation 5, see tenancyUncovered below).
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

// Cross-household coverage (ADR-00018 obligation 5): the membership check is the
// only lock between households, so every route is also requested by an
// authenticated non-member and by a member of a different household. Those
// specs are the files ending in TENANCY_SUFFIX. A route is covered when one of
// them requests it with a URL literal that matches the pattern (as above) and
// names both identities from fixtures/identity.ts in code: STRANGER (a verified
// email with no household) and OTHER (a member of a household that does not
// hold the data under test). A tenancy spec runs every route it lists as both
// identities, so the per-file check is a per-route check. There are no
// exemptions: a public or operator-only route is requested by the same two
// identities, and the spec asserts the rule for it (no tenant data in the
// response; operator pages refused to both).
export const TENANCY_SUFFIX = '.tenancy.spec.ts';
export const NON_MEMBER_IDENTITY = 'STRANGER';
export const OTHER_MEMBER_IDENTITY = 'OTHER';

/** Source with block comments and line comments removed, so a name mentioned only in a comment does not count. */
export function stripComments(src) {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .split('\n')
    .map((line) => line.replace(/^\s*\/\/.*$/, '').replace(/\s\/\/\s.*$/, ''))
    .join('\n');
}

function mentions(code, identifier) {
  return new RegExp(`\\b${identifier}\\b`).test(code);
}

/**
 * Patterns lacking cross-household coverage, each with what is missing. specs
 * are the tenancy specs' { text }; a spec that requests a pattern but omits an
 * identity reports that identity as missing for the pattern.
 */
export function tenancyUncovered(routes, specs) {
  const analysed = specs.map((s) => {
    const code = stripComments(s.text);
    return {
      urls: extractURLs(code),
      nonMember: mentions(code, NON_MEMBER_IDENTITY),
      otherMember: mentions(code, OTHER_MEMBER_IDENTITY),
    };
  });
  const out = [];
  for (const pattern of new Set(routes.map((r) => r.pattern))) {
    const reaching = analysed.filter((s) => s.urls.some((u) => matchesPattern(pattern, u)));
    const missing = [];
    if (reaching.length === 0) {
      missing.push('a request in any *' + TENANCY_SUFFIX);
    } else {
      if (!reaching.some((s) => s.nonMember)) missing.push(`the non-member identity ${NON_MEMBER_IDENTITY}`);
      if (!reaching.some((s) => s.otherMember)) missing.push(`the other-household member identity ${OTHER_MEMBER_IDENTITY}`);
    }
    if (missing.length > 0) out.push({ pattern, missing });
  }
  return out;
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
  const specPaths = walk(e2eDir);
  const texts = specPaths.map((p) => ({ path: p, text: readFileSync(p, 'utf8') }));
  const urls = texts.flatMap((s) => extractURLs(s.text));
  const missing = uncovered(routes, urls);
  const tenancy = tenancyUncovered(
    routes,
    texts.filter((s) => s.path.endsWith(TENANCY_SUFFIX)),
  );
  for (const p of missing) console.error(`route ${p} has no e2e test under e2e/`);
  for (const { pattern, missing: what } of tenancy) {
    console.error(`route ${pattern} has no cross-household test (ADR-00018): missing ${what.join(' and ')}`);
  }
  if (missing.length > 0 || tenancy.length > 0) process.exit(1);
  console.log(`route coverage: ${[...new Set(routes.map((r) => r.pattern))].join(', ')}`);
}

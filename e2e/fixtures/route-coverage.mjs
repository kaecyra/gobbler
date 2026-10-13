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
// specs are the files ending in TENANCY_SUFFIX.
//
// A probe is a line of a tenancy spec holding a method literal ('GET', 'POST',
// ...) followed on the same line by a path literal: the route table of the spec.
// A route (method and pattern) is covered when a probe has that method and a
// path that matches the pattern (as above). A route registered for every HTTP
// method (the static file mount) needs a probe with any one method. Methods are
// compared, so a GET probe does not cover a POST-only route.
//
// Every other request a tenancy spec needs, to seed data or read it back, lives
// outside the spec (fixtures/routes/tenancy-support.ts). A path literal in a
// tenancy spec that is not on a probe line is an error, so nothing but a probe
// can be counted as coverage.
//
// The spec must also run its probes as both identities from fixtures/identity.ts:
// STRANGER (a verified email with no household) and OTHER (a member of a
// household that does not hold the data under test). That is checked as each
// being passed to as(...) or asIdentity(...), the call that makes a request
// context act as an email, and not by an import or a mention. A spec sends all
// of its probes as both identities, so the per-file check is a per-route check.
// There are no exemptions: a public or operator-only route is probed by the
// same two identities, and the spec asserts the rule for it (no tenant data in
// the response; operator pages refused to both).
export const TENANCY_SUFFIX = '.tenancy.spec.ts';
export const NON_MEMBER_IDENTITY = 'STRANGER';
export const OTHER_MEMBER_IDENTITY = 'OTHER';
const HTTP_METHODS = ['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS', 'CONNECT', 'TRACE'];

// The method is the first argument of a call (hidden('POST', ...)) or the value
// of a method: key, so a request helper that takes the method second
// (send(ctx, 'POST', ...)) is not a probe.
const PROBE_LINE = new RegExp(`(?:\\b\\w+\\(\\s*|\\bmethod:\\s*)(["'])(${HTTP_METHODS.join('|')})\\1[^\\n]*?(["'\`])(/(?:(?!\\3)[^\\n\\\\])*)\\3`);

/** Source with block comments and line comments removed, so a name mentioned only in a comment does not count. */
export function stripComments(src) {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .split('\n')
    .map((line) => line.replace(/^\s*\/\/.*$/, '').replace(/\s\/\/\s.*$/, ''))
    .join('\n');
}

/** Source without comments and import statements: only what runs. */
function codeOf(src) {
  return stripComments(src).replace(/^\s*import\b[\s\S]*?\bfrom\s*(["'])[^"'\n]*\1\s*;?/gm, '');
}

function normalise(url) {
  return url.replace(/\$\{[^}]*\}/g, 'x').split(/[?#]/)[0];
}

/**
 * The probes of a tenancy spec ({ method, url }, the first path literal after a
 * method literal on a line) and the path literals that are on no probe line.
 */
export function extractProbes(specText) {
  const probes = [];
  const stray = [];
  for (const line of codeOf(specText).split('\n')) {
    const m = line.match(PROBE_LINE);
    if (m) probes.push({ method: m[2], url: normalise(m[4]) });
    else stray.push(...extractURLs(line));
  }
  return { probes, stray };
}

/** True when identifier is an argument of as(...) or asIdentity(...), the calls that make requests act as an email. */
function actsAs(code, identifier) {
  const word = new RegExp(`\\b${identifier}\\b`);
  for (const m of code.matchAll(/\b(?:as|asIdentity)\(([^()]*)\)/g)) {
    if (word.test(m[1])) return true;
  }
  return false;
}

/** The methods a route must be probed with: all of them for a route mounted on every method, else each it lists. */
function requiredMethods(methods) {
  return methods.length >= HTTP_METHODS.length ? [null] : methods;
}

/**
 * Routes lacking cross-household coverage, each with what is missing. specs are
 * the tenancy specs' { text }. A probe counts only in a spec that also uses
 * both identities, so a spec naming one of them reports the other as missing
 * for every route it probes.
 */
export function tenancyUncovered(routes, specs) {
  const analysed = specs.map((s) => {
    const code = codeOf(s.text);
    return {
      probes: extractProbes(s.text).probes,
      nonMember: actsAs(code, NON_MEMBER_IDENTITY),
      otherMember: actsAs(code, OTHER_MEMBER_IDENTITY),
    };
  });
  const methodsByPattern = new Map();
  for (const r of routes) {
    if (!methodsByPattern.has(r.pattern)) methodsByPattern.set(r.pattern, new Set());
    methodsByPattern.get(r.pattern).add(r.method);
  }
  const out = [];
  for (const [pattern, set] of methodsByPattern) {
    for (const method of requiredMethods([...set])) {
      const reaching = analysed.filter((s) => s.probes.some((p) => (method === null || p.method === method) && matchesPattern(pattern, p.url)));
      const missing = [];
      if (reaching.length === 0) {
        missing.push(`a ${method ?? 'request'} probe in any *${TENANCY_SUFFIX}`);
      } else {
        if (!reaching.some((s) => s.nonMember)) missing.push(`the non-member identity ${NON_MEMBER_IDENTITY} passed to as(...)`);
        if (!reaching.some((s) => s.otherMember)) missing.push(`the other-household member identity ${OTHER_MEMBER_IDENTITY} passed to as(...)`);
      }
      if (missing.length > 0) out.push({ method: method ?? '*', pattern, missing });
    }
  }
  return out;
}

/** Path literals in tenancy specs that are on no probe line, by spec. */
export function tenancyStray(specs) {
  return specs.map((s) => ({ path: s.path, urls: extractProbes(s.text).stray })).filter((s) => s.urls.length > 0);
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
  const tenancySpecs = texts.filter((s) => s.path.endsWith(TENANCY_SUFFIX));
  const tenancy = tenancyUncovered(routes, tenancySpecs);
  const stray = tenancyStray(tenancySpecs);
  for (const p of missing) console.error(`route ${p} has no e2e test under e2e/`);
  for (const { method, pattern, missing: what } of tenancy) {
    console.error(`route ${method} ${pattern} has no cross-household test (ADR-00018): missing ${what.join(' and ')}`);
  }
  for (const { path, urls } of stray) {
    console.error(`${path}: path literal(s) on no probe line (${urls.join(', ')}); move setup requests to fixtures/routes/tenancy-support.ts`);
  }
  if (missing.length > 0 || tenancy.length > 0 || stray.length > 0) process.exit(1);
  console.log(`route coverage: ${[...new Set(routes.map((r) => r.pattern))].join(', ')}`);
}

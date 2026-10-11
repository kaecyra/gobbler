// Route-coverage check (ADR-00013 obligation 2): every route registered in
// internal/web has at least one e2e spec that requests it.
//
// Routes are read from the chi registration calls in the non-test Go files
// directly under internal/web (r.Get("/x", ...), r.Mount("/x", ...), and the
// same with a string constant such as HealthPath). Routes declared inside a
// feature subpackage are relative to their Mount, so a feature is covered at
// its Mount prefix. A spec covers a route when it contains a string literal
// that is exactly the route, or, for a route with a {param} or * segment or a
// Mount, one that starts with the route up to that point.
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const METHODS = 'Get|Post|Put|Patch|Delete|Head|Options|Handle|HandleFunc|Method|MethodFunc|Mount|Connect|Trace';
const CALL = new RegExp(`\\.(${METHODS})\\(\\s*([A-Za-z_][A-Za-z0-9_]*|"[^"]*")`, 'g');
const CONST = /([A-Za-z_][A-Za-z0-9_]*)\s*=\s*"(\/[^"]*)"/g;

function stripComments(src) {
  return src
    .split('\n')
    .filter((l) => !l.trimStart().startsWith('//'))
    .join('\n');
}

/** @param {{name: string, text: string}[]} files non-test Go sources */
export function findRoutes(files) {
  const texts = files.map((f) => ({ name: f.name, text: stripComments(f.text) }));
  const consts = new Map();
  for (const { text } of texts) {
    for (const m of text.matchAll(CONST)) consts.set(m[1], m[2]);
  }
  const routes = new Map();
  for (const { name, text } of texts) {
    for (const m of text.matchAll(CALL)) {
      const [, kind, arg] = m;
      let path;
      if (arg.startsWith('"')) path = arg.slice(1, -1);
      else if (consts.has(arg)) path = consts.get(arg);
      else if (kind === 'Mount' || kind === 'Handle' || kind === 'HandleFunc' || /^(Get|Post|Put|Patch|Delete|Head|Options|Connect|Trace)$/.test(kind)) {
        // An identifier that is not a known path constant is a route we cannot
        // read; refuse rather than skip it.
        throw new Error(`${name}: cannot resolve the route path ${arg} in .${kind}(...)`);
      } else continue;
      if (!path.startsWith('/')) continue; // not a route (e.g. a Get on a non-router)
      routes.set(path, { path, mount: kind === 'Mount' });
    }
  }
  return [...routes.values()];
}

function needle(route) {
  if (route.mount) return { text: route.path, prefix: true };
  const i = route.path.search(/[{*]/);
  return i === -1 ? { text: route.path, prefix: false } : { text: route.path.slice(0, i), prefix: true };
}

function escape(s) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/** @returns routes with no covering spec */
export function uncovered(routes, specTexts) {
  return routes.filter((route) => {
    const n = needle(route);
    const re = new RegExp(`["'\`]${escape(n.text)}${n.prefix ? '' : '["\'`]'}`);
    return !specTexts.some((t) => re.test(t));
  });
}

function walk(dir, pick) {
  const out = [];
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    if (e.name === 'node_modules') continue;
    const p = join(dir, e.name);
    if (e.isDirectory()) out.push(...walk(p, pick));
    else if (pick(e.name)) out.push(p);
  }
  return out;
}

export function check(webDir, e2eDir) {
  const goFiles = readdirSync(webDir)
    .filter((n) => n.endsWith('.go') && !n.endsWith('_test.go') && !n.endsWith('_templ.go'))
    .map((n) => ({ name: n, text: readFileSync(join(webDir, n), 'utf8') }));
  const routes = findRoutes(goFiles);
  const specs = walk(e2eDir, (n) => n.endsWith('.spec.ts')).map((p) => readFileSync(p, 'utf8'));
  return { routes, missing: uncovered(routes, specs) };
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const [webDir, e2eDir] = process.argv.slice(2);
  if (!webDir || !e2eDir) {
    console.error('usage: route-coverage.mjs <internal/web dir> <e2e dir>');
    process.exit(2);
  }
  const { routes, missing } = check(webDir, e2eDir);
  if (routes.length === 0) {
    console.error(`no routes found under ${webDir}; the check cannot be trusted`);
    process.exit(1);
  }
  if (missing.length > 0) {
    for (const r of missing) console.error(`route ${r.path} has no e2e test under ${e2eDir}`);
    process.exit(1);
  }
  console.log(`route coverage: ${routes.map((r) => r.path).join(', ')}`);
}

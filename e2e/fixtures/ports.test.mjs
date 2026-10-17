import assert from 'node:assert/strict';
import { createServer } from 'node:net';
import { test } from 'node:test';
import { freePorts, portFromEnv, portsFromEnv } from './ports.mjs';

const listen = (port) =>
  new Promise((resolve, reject) => {
    const s = createServer();
    s.once('error', reject);
    s.listen(port, '127.0.0.1', () => resolve(s));
  });

test('freePorts returns distinct ports that can be bound at once', async () => {
  const ports = await freePorts(4);
  assert.equal(new Set(ports).size, 4);
  const servers = await Promise.all(ports.map(listen));
  servers.forEach((s) => s.close());
});

test('portFromEnv honours a set variable', async () => {
  assert.equal(await portFromEnv({ GOBBLER_E2E_X_PORT: '4321' }, 'GOBBLER_E2E_X_PORT'), 4321);
});

test('portFromEnv without the variable returns a bindable port', async () => {
  const port = await portFromEnv({}, 'GOBBLER_E2E_X_PORT');
  (await listen(port)).close();
});

test('portsFromEnv pins the named ports and gives the rest distinct free ones', async () => {
  const [a, b, c] = await portsFromEnv({ A: '4321' }, ['A', 'B', 'C']);
  assert.equal(a, 4321);
  assert.notEqual(b, c);
  (await listen(b)).close();
});

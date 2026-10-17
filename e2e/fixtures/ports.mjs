import { createServer } from 'node:net';

// Ports for the servers the e2e specs start themselves. A fixed default sits in
// the kernel's ephemeral range (32768-60999), where another test's outgoing
// connection can hold it when the server binds: the server exits with "address
// already in use" and the health poll waits out its whole deadline (#257). Ask
// the kernel for a free port instead.

/** n distinct ports the kernel has just shown to be free. */
export async function freePorts(n) {
  const probes = [];
  try {
    for (let i = 0; i < n; i++) {
      probes.push(
        await new Promise((resolve, reject) => {
          const probe = createServer();
          probe.once('error', reject);
          probe.listen(0, '127.0.0.1', () => resolve(probe));
        }),
      );
    }
    return probes.map((p) => p.address().port);
  } finally {
    // Held open until all are chosen, so the kernel cannot hand one out twice.
    await Promise.all(probes.map((p) => new Promise((resolve) => p.close(resolve))));
  }
}

/**
 * One port per name: env[name] when set, otherwise a free one. The free ones are
 * taken together, so they are distinct from each other.
 */
export async function portsFromEnv(env, names) {
  const free = await freePorts(names.length);
  return names.map((name, i) => (env[name] ? Number(env[name]) : free[i]));
}

/** The port named by env[name] when set, otherwise a free one. */
export async function portFromEnv(env, name) {
  return (await portsFromEnv(env, [name]))[0];
}

# ADR-00057: Integration settings live in the database, and their secrets are encrypted there under a key from the environment

**Status:** Accepted

**Date:** 2026-11-10

## Context

AGENTS.md and [ADR-00007](00007-llm-provider-interface.md) put every API key
and password in the environment. Changing one means shell access to the VM and
a restart, and nothing in the app shows what is set.
[ADR-00055](00055-invitation-email.md) (mail) and
[ADR-00056](00056-llm-provider-per-task.md) (LLM providers) both add
integrations an operator will want to change from the app. Each could have
grown its own storage; the owner wants one.

Nightly snapshots of the database are uploaded to S3. A secret stored in the
database in clear would travel with every snapshot.

Options considered:

- **Secrets stay in the environment; only non-secret settings move to the
  database.** Snapshots stay free of secrets. Rejected by the owner: an
  operator would still need the shell to set up or rotate a key, which is the
  problem being solved.
- **Secrets in the database in clear.** Rejected: every snapshot, and anyone
  who obtains the database file, would hold working keys.
- **Secrets in the database, encrypted under a key held only in the
  environment.** Chosen. A snapshot or a copied database file is useless
  without the environment. This does not protect against someone with access
  to the VM itself, who can read both; nothing short of an external key
  service does, and that is out of proportion for one home server.
- **Mail in the environment, LLM keys in the database**, drawing the line
  between secrets needed to start and integrations an operator changes.
  Rejected: mail is on the same side of that line as the LLM, and two stores
  for one kind of secret leave the next reader asking which is authoritative.

## Decision

### What moves, and what stays

Integration settings live in the database and are edited by an operator on
the operator's Deployment page: the mail settings of ADR-00055 and the LLM
settings of ADR-00056. This is per deployment, not per household.

What the binary needs to start, to sign people in and to protect the data
stays in the environment: the OIDC client secret, the S3 credentials, the
encryption key below, and every other setting not named above. Rate limits
remain environment settings, as for MCP and OAuth (ADR-00034, ADR-00036).

The `GOBBLER_LLM_*`, `GOBBLER_ANTHROPIC_API_KEY` and `GOBBLER_GEMINI_API_KEY`
environment variables are removed, not imported. A deployment that set them
re-enters its LLM settings on the Deployment page after upgrading. A
deployment that still sets one is told at startup, in the log, that it is
ignored.

### One store for every integration secret

There is one store for integration secrets, in a package of its own (not
`internal/credential`, which names bearer credential kinds). Every integration
that holds a secret uses it; none keeps its own table or cipher.

- A secret is addressed by an integration name and a field name, for example
  mail and password.
- Each value is sealed with AES-256-GCM under a key derived from the
  environment's encryption key with HKDF-SHA256, with a fresh random nonce per
  write. The integration and field names are the associated data, so a value
  copied into another row fails to open.
- The encryption key is 32 random bytes, base64-encoded, in one environment
  variable. It is never logged, never stored in the database and never sent
  to a browser.

### On the page

A secret can be entered, replaced or cleared, never read back. Once saved, the
page shows only that it is set; the value never reaches the browser again.
Non-secret settings (provider, model, host, port, user, from address, timeout)
are shown and edited normally.

### When the key is missing or wrong

gobbler still starts. Secrets that cannot be opened, because the key is
unset, changed or does not match (for example after restoring a snapshot on a
new machine), are treated as unset: the integration they belong to is off,
the Deployment page marks each as unreadable and asks for it again, and the
failure is logged at error level at startup. Saving a secret with no
encryption key configured is refused with a message saying the key is
missing. There is no rotation command: changing the key and re-entering the
secrets is the rotation.

## Consequences

This amends ADR-00007's Decision ("API keys come only from the environment")
and the AGENTS.md boundary "No secrets in the repository. API keys, tokens and
S3 credentials come only from the environment": integration secrets come from
the database, encrypted under a key from the environment. Nothing secret is
committed to the repository either way.

**Obligations:**

1. AGENTS.md's secrets boundary is reworded to say integration secrets are
   stored encrypted in the database under a key from the environment, citing
   this ADR, and every other secret comes only from the environment.
2. ADR-00007 gains a note at its Decision pointing to this ADR for where API
   keys live.
3. The secret store's tests show: a round trip; two writes of one value give
   different ciphertexts; a ciphertext moved to another integration or field
   fails to open; a wrong key fails to open and is reported as unreadable,
   not as an empty value.
4. A test shows a startup with stored secrets and no encryption key, or the
   wrong one, starts, leaves the affected integration off and logs the
   failure.
5. A web test shows no response from the Deployment page contains a saved
   secret's value.
6. A config test shows that each removed variable (every `GOBBLER_LLM_*`
   variable and both provider API key variables), when set, is logged as
   ignored at startup.
7. `docs/design.md`'s configuration table drops the LLM row's environment
   settings, gains the encryption key, and says where integration settings
   live. `.env.example` drops the LLM variables and gains the encryption key,
   empty, with a comment giving a command to generate one.
8. `docs/operations.md` says how to generate the encryption key, that losing
   it means re-entering every integration secret, and that snapshots carry the
   secrets encrypted.

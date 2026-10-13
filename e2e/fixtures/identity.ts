// Identities for the dev-auth e2e server (ADR-00018). With the dev bypass on,
// the binary takes the identity of a request from X-Gobbler-Dev-Identity and
// falls back to DEV. The bypass seeds DEV and OTHER as members of two separate
// households. OPERATOR is listed in GOBBLER_OPERATOR_EMAILS by serve.mjs and
// belongs to no household; STRANGER is neither a member nor an operator.
export const DEV = 'dev@localhost';
export const OTHER = 'other@localhost';
export const OPERATOR = 'operator@e2e.example';
export const STRANGER = 'stranger@e2e.example';

const HEADER = 'X-Gobbler-Dev-Identity';

/** Playwright `use` options that make every request act as email. */
export function asIdentity(email: string) {
  return { extraHTTPHeaders: { [HEADER]: email } };
}

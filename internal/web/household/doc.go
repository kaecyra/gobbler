// Package household is the household's membership pages (ADR-00018): the
// members and the invitations into the household, leaving it, deleting it, and
// accepting an invitation, which a verified non-member can reach. It depends on
// the household store through the small Store interface and imports no other
// web feature package (ADR-00012). It is mounted from internal/web.
package household

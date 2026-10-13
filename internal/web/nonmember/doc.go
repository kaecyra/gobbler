// Package nonmember is the "not a member" page (ADR-00018). A verified email
// that belongs to no household sees it in place of every page it cannot open,
// and it shows nothing but the email the person signed in with: no household
// data, no catalog data, no counts. It imports no other web feature package
// (ADR-00012) and is mounted from internal/web.
package nonmember

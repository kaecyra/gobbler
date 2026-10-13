// Package operator is the operator's pages (ADR-00018, Invitations). Today that
// is issuing and revoking household invitations, which shows no household's
// data. Package web admits only an operator email to a path under Pattern. The
// package depends on the household store through the small Households
// interface and imports no other web feature package (ADR-00012). It is
// mounted from internal/web.
package operator

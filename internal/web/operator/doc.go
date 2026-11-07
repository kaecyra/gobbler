// Package operator is the operator's pages (ADR-00018, Invitations): issuing
// and revoking household invitations, and reviewing the ingredients households
// suggest for the central catalog. Neither shows a household's data. The
// equipment review and the central ingredient editor are operator pages too,
// served by internal/web/catalog. Package web admits only an operator email to
// a path under Pattern. The package reaches the household and catalog services
// through the small Households and Promoter interfaces and imports no other web
// feature package (ADR-00012). It is mounted from internal/web.
package operator

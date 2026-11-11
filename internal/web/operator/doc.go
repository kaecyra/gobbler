// Package operator is the operator's pages (ADR-00018, Invitations): the
// operator area's overview, issuing and revoking household invitations, and
// reviewing the ingredients households suggest for the central catalog, and the
// LLM usage page (ADR-00058), which shows every household's calls and the cost
// of each model and sets model prices. None shows a household's recipes or other
// data. The equipment review and the central ingredient
// editor (internal/web/catalog) and the deployment page (internal/web/settings)
// are operator pages too; every one is drawn in layout.OperatorPage, whose rail
// links them all (docs/ui/design.md, Operator area). Package web admits only an operator email to
// a path under Pattern. The package reaches the household and catalog services
// through the small Households, Suggestions and Promoter interfaces and imports no other web
// feature package (ADR-00012). It is mounted from internal/web.
package operator

// Package extract pulls a raw recipe out of fetched HTML with rules alone
// (ADR-00006): schema.org JSON-LD first, then microdata, then a registry of
// site-specific extractors, and when none match, the page's main text for the
// text-sectioning path (parse.SplitSections). A recipe read from JSON-LD or
// microdata is then checked for a recipe plugin's markup (WP Recipe Maker,
// Tasty Recipes, Create), detected by markup and never by host, whose headings
// supply the ingredient groups and step sections schema.org leaves flat.
//
// Everything here is pure and deterministic over the HTML bytes: no network
// I/O (fetching belongs to the ingest pipeline) and no model. The result is
// raw: ingredient strings are not parsed (that is the line parser's job);
// time fields are kept as written and also read into durations by
// parse.Duration. A page that matches no method, or a block that
// cannot be read, is reported through Result.Method and Result.Issues, not as
// an error, so one bad block never hides a good one.
package extract

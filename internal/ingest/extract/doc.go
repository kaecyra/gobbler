// Package extract pulls a raw recipe out of fetched HTML with rules alone
// (ADR-00006): schema.org JSON-LD first, then microdata, then a registry of
// site-specific extractors, and when none match, the page's main text for the
// text-sectioning path (parse.SplitSections).
//
// Everything here is pure and deterministic over the HTML bytes: no network
// I/O (fetching belongs to the ingest pipeline) and no model. The result is
// raw: ingredient strings and time fields are not parsed, that is the line
// and duration parsers' job. A page that matches no method, or a block that
// cannot be read, is reported through Result.Method and Result.Issues, not as
// an error, so one bad block never hides a good one.
package extract

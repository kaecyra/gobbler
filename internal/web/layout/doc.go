// Package layout holds the shared page layout and the HTTP error mapping every
// web feature uses (ADR-00012). A feature renders its content inside Page and
// reports failures through WriteError; it never builds its own shell.
package layout

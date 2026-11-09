package mcp

import apiingest "github.com/kaecyra/gobbler/internal/api/ingest"

// PreviewOf is a draft in preview_import's shape, so a test can compare a held
// draft with a preview field for field.
func PreviewOf(d apiingest.Draft) any { return previewOf(d) }

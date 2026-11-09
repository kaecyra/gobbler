// Package recipes is the recipe service: every operation on saved recipes and
// on what waits to change them, written once, for the web handlers and the MCP
// tools to call (ADR-00043). It is in-process. It is not a network API, it
// speaks neither HTTP nor JSON and it reads no cookie or header. It follows the
// pattern internal/api/shopping set: a method takes the context, then the
// caller.Caller, then its own arguments; it refuses a Caller that is not a
// member of the household with ErrNotMember before touching a row; and it
// returns the domain's errors, which the web turns into statuses in
// internal/web/layout (statusTable) and MCP into tool errors in
// internal/mcp/tools.go.
//
// What is in it:
//
//   - Reading and searching recipes, the library's cards, revisions and
//     versions, notes and made-it entries, video thumbnails.
//   - Changing them: the version-checked save, restore, clone, delete (with the
//     recipe's picture objects), keeping the preview image of a video.
//   - What waits for a person to review: proposed edits, ingestion drafts
//     (hold, read, replace, discard, choose the picture), the save of a
//     confirmed draft or re-import, and starting a re-import.
//   - The picture of a recipe as the recipe screens need it (its key and the
//     draft's candidates), and the pictures offered on its edit page: a search
//     of its source page, an upload, and the choice applied on save
//     (ADR-00051).
//
// What is not: fetching and reading a page, text or photo into a draft is the
// ingestion pipeline (internal/ingest), which has no household data of its own
// and stays a library the adapters call; the service holds its result. The
// catalog, journal, sharing and shopping are other resources.
//
// Where internal/picture sits. It stays a domain package that this one calls,
// and is not moved under internal/api. Its operations are keyed to a recipe or
// a draft that this service has already admitted a Caller to, scope themselves
// by the household on the context, and are also reached by the household
// delete (through an interface) and by the media-store sweep at start-up,
// neither of which has a Caller. Moving it would put a Caller on calls those
// two cannot make, or leave two copies. The picture routes
// (internal/web/picture) go through this service (PictureKey, Candidates),
// which admits the Caller. Rejected: a full move under internal/api/recipes,
// for the reason above; and a separate internal/api/pictures resource, because
// the picture service is not a household-facing resource of its own: MCP
// reaches only its search, through FindPictures here (ADR-00051).
//
// The package re-exports the domain types and errors its callers need as
// aliases, so internal/web and internal/mcp import this package and neither
// internal/store nor internal/picture. A depguard rule in .golangci.yml fails
// make lint when they do.
package recipes

// Package session is the sign-in session store (ADR-00030): a server-side row
// per signed-in browser, named by an opaque random token that the cookie
// carries. The database holds only the token's SHA-256, so neither a backup
// nor a copy of the file can be replayed as a cookie. A session lasts a fixed
// time from sign-in; deleting its row ends it at once. The SQL is in queries/,
// generated into sessiondb (ADR-00014).
package session

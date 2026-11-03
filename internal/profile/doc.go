// Package profile is the profile of a person (ADR-00032): a display name and an
// avatar, keyed by the email, and reached at an opaque random address that
// never contains the email. It also holds the one visibility rule that every
// view of another person's fields goes through (Visible), and the default
// avatar drawn from a hash of the email (NewIdenticon), which fetches nothing
// from outside. Avatars is the upload path of the picture itself (ADR-00033):
// it checks and stores an upload, takes the crop, processes it in the
// background and records the result on the profile; the bytes live in
// internal/media. The SQL is in queries/, generated into profiledb (ADR-00014).
package profile

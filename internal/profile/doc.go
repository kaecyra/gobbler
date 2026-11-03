// Package profile is the profile of a person (ADR-00032): a display name and an
// avatar, keyed by the email, and reached at an opaque random address that
// never contains the email. It also holds the one visibility rule that every
// view of another person's fields goes through (Visible), and the default
// avatar drawn from a hash of the email (NewIdenticon), which fetches nothing
// from outside. The SQL is in queries/, generated into profiledb (ADR-00014).
package profile

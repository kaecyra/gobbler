// Package config reads every application setting from the environment, once,
// and validates it at start-up. No other package reads the environment for
// application settings.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/mail"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Environment variable names.
const (
	EnvListenAddr          = "GOBBLER_LISTEN_ADDR"
	EnvDataDir             = "GOBBLER_DATA_DIR"
	EnvLogLevel            = "GOBBLER_LOG_LEVEL"
	EnvDevAuthBypass       = "GOBBLER_DEV_AUTH_BYPASS"
	EnvAccessTeamDomain    = "GOBBLER_ACCESS_TEAM_DOMAIN"
	EnvAccessAUD           = "GOBBLER_ACCESS_AUD"
	EnvOIDCIssuer          = "GOBBLER_OIDC_ISSUER"
	EnvOIDCClientID        = "GOBBLER_OIDC_CLIENT_ID"
	EnvOIDCClientSecret    = "GOBBLER_OIDC_CLIENT_SECRET"
	EnvPublicBaseURL       = "GOBBLER_PUBLIC_BASE_URL"
	EnvSessionLifetime     = "GOBBLER_SESSION_LIFETIME"
	EnvOperatorEmails      = "GOBBLER_OPERATOR_EMAILS"
	EnvConfidenceThreshold = "GOBBLER_INGEST_CONFIDENCE_THRESHOLD"
	EnvFetchTimeout        = "GOBBLER_INGEST_FETCH_TIMEOUT"
	EnvUserAgent           = "GOBBLER_INGEST_USER_AGENT"
	EnvLLMProvider         = "GOBBLER_LLM_PROVIDER"
	EnvLLMTextModel        = "GOBBLER_LLM_TEXT_MODEL"
	EnvLLMVisionModel      = "GOBBLER_LLM_VISION_MODEL"
	EnvLLMTimeout          = "GOBBLER_LLM_TIMEOUT"
	EnvAnthropicAPIKey     = "GOBBLER_ANTHROPIC_API_KEY"
	EnvGeminiAPIKey        = "GOBBLER_GEMINI_API_KEY"
	EnvVideoThumbnailHost  = "GOBBLER_VIDEO_THUMBNAIL_HOST"
	EnvBackupSchedule      = "GOBBLER_BACKUP_SCHEDULE"
	EnvBackupLocalKeep     = "GOBBLER_BACKUP_LOCAL_RETENTION"
	EnvBackupRemoteKeep    = "GOBBLER_BACKUP_REMOTE_RETENTION"
	EnvS3Endpoint          = "GOBBLER_S3_ENDPOINT"
	EnvS3Region            = "GOBBLER_S3_REGION"
	EnvS3Bucket            = "GOBBLER_S3_BUCKET"
	EnvS3Prefix            = "GOBBLER_S3_PREFIX"
	EnvS3AccessKeyID       = "GOBBLER_S3_ACCESS_KEY_ID"
	EnvS3SecretAccessKey   = "GOBBLER_S3_SECRET_ACCESS_KEY"
)

// LLM provider names accepted by EnvLLMProvider.
const (
	ProviderClaude = "claude"
	ProviderGemini = "gemini"
)

const (
	defaultListenAddr = "127.0.0.1:8080"
	defaultDataDir    = "data"
	defaultLogLevel   = "info"
	defaultUserAgent  = "gobbler (self-hosted recipe manager)"
	defaultSchedule   = "03:00"
)

const (
	defaultConfidenceThreshold = 0.7
	defaultFetchTimeout        = 15 * time.Second
	defaultLLMTimeout          = 60 * time.Second
	defaultLocalRetention      = 7
	defaultRemoteRetention     = 30
	defaultSessionLifetime     = 30 * 24 * time.Hour
)

// Config is the validated application configuration. Load returns it by value
// and nothing mutates it afterwards. Credentials are Secret values, so
// printing or logging a Config never reveals them.
type Config struct {
	Server    Server
	Access    Access
	OIDC      OIDC
	Session   Session
	Operator  Operator
	Ingestion Ingestion
	LLM       LLM
	Video     Video
	Backup    Backup
}

// Server holds listener, storage and logging settings.
type Server struct {
	ListenAddr string
	DataDir    string
	LogLevel   slog.Level
	// DevAuthBypass skips Access verification. Valid only on a loopback
	// ListenAddr (ADR-00003).
	DevAuthBypass bool
}

// Access holds the Cloudflare Access settings (ADR-00003). Both are empty
// when DevAuthBypass is on, and when sign-in is configured, which replaces
// Access as the identity source.
type Access struct {
	TeamDomain string
	AUD        string
}

// OIDC holds the sign-in settings (ADR-00030): the one OpenID Connect issuer
// the app signs people in with, its client credentials, and the public base
// URL the redirect URI is built from. No default names a provider; the issuer
// is whatever the environment says.
//
// None of it is required at start-up yet. When it is set, sign-in is the
// identity source and the Access settings must be unset; when it is not,
// Cloudflare Access stays the identity source until it is removed. Each value
// is validated when set, and setting any of them requires all four, so a
// half-configured sign-in fails at start-up rather than at the first sign-in.
type OIDC struct {
	// Issuer is the issuer URL exactly as given. ID tokens are checked
	// against it character for character, so it is not normalised. It must
	// be https unless DevAuthBypass is on.
	Issuer       string
	ClientID     string
	ClientSecret Secret
	// PublicBaseURL is the origin the site is reached at, with no trailing
	// slash and no path. It must be https unless DevAuthBypass is on.
	PublicBaseURL string
}

// Configured reports whether sign-in is configured. Load guarantees that it
// is all or nothing.
func (o OIDC) Configured() bool { return o.Issuer != "" }

// Session holds sign-in session settings (ADR-00030).
type Session struct {
	// Lifetime is how long a session lasts from sign-in. Use never extends
	// it.
	Lifetime time.Duration
}

// Operator holds the operator identities (ADR-00018). The operator is defined
// here and never in the database, so operator status cannot be granted from
// inside the app.
type Operator struct {
	// Emails are trimmed and lower-cased, without duplicates.
	Emails []string
}

// IsOperator reports whether the verified email is an operator's.
func (o Operator) IsOperator(email string) bool {
	return slices.Contains(o.Emails, strings.ToLower(strings.TrimSpace(email)))
}

// Ingestion holds ingestion settings (ADR-00006).
type Ingestion struct {
	// ConfidenceThreshold is in [0, 1]; drafts scoring below it fall back to
	// the LLM.
	ConfidenceThreshold float64
	FetchTimeout        time.Duration
	UserAgent           string
}

// LLM holds provider settings (ADR-00007). Provider is empty when the LLM is
// not configured.
type LLM struct {
	Provider    string
	TextModel   string
	VisionModel string
	Timeout     time.Duration
	// APIKey is the key of the selected Provider.
	APIKey Secret
}

// Configured reports whether an LLM provider is selected.
func (l LLM) Configured() bool { return l.Provider != "" }

// Video holds recipe video settings (ADR-00021).
type Video struct {
	// ThumbnailHost is the origin thumbnails are downloaded from at save, with
	// no trailing slash. Empty means YouTube's image host (video.NewFetcherAt
	// knows it); only the e2e suite sets it.
	ThumbnailHost string
}

// Backup holds backup settings (ADR-00010).
type Backup struct {
	// Schedule is the daily run time as "HH:MM".
	Schedule        string
	LocalRetention  int
	RemoteRetention int
	S3              S3
}

// S3 holds the remote backup target. Endpoint is empty for local-only backups.
type S3 struct {
	Endpoint        string
	Region          string
	Bucket          string
	Prefix          string
	AccessKeyID     Secret
	SecretAccessKey Secret
}

// Configured reports whether remote backups are enabled.
func (s S3) Configured() bool { return s.Endpoint != "" }

// Load reads the process environment and validates it. The error names every
// offending variable.
func Load() (Config, error) {
	return loadFrom(os.LookupEnv)
}

// loadFrom is Load over an arbitrary lookup function, for tests.
func loadFrom(lookup func(string) (string, bool)) (Config, error) {
	r := reader{lookup: lookup}
	var c Config

	c.Server.ListenAddr = r.str(EnvListenAddr, defaultListenAddr)
	c.Server.DataDir = r.str(EnvDataDir, defaultDataDir)
	c.Server.LogLevel = r.logLevel(EnvLogLevel, defaultLogLevel)
	c.Server.DevAuthBypass = r.boolean(EnvDevAuthBypass)
	if c.Server.DevAuthBypass && !IsLoopbackAddr(c.Server.ListenAddr) {
		r.fail(EnvDevAuthBypass, "refusing to start: dev auth bypass requires a loopback %s, got %q", EnvListenAddr, c.Server.ListenAddr)
	}

	r.oidc(&c.OIDC, c.Server.DevAuthBypass)

	c.Access.TeamDomain = r.str(EnvAccessTeamDomain, "")
	c.Access.AUD = r.str(EnvAccessAUD, "")
	switch {
	case c.Server.DevAuthBypass:
		c.Access = Access{}
	case c.OIDC.Configured():
		// Sign-in replaces Access as the identity source (ADR-00030), so an
		// Access setting would be silently ignored.
		for _, s := range []struct{ env, v string }{{EnvAccessTeamDomain, c.Access.TeamDomain}, {EnvAccessAUD, c.Access.AUD}} {
			if s.v != "" {
				r.fail(s.env, "set but sign-in (%s) is configured, which replaces Access; unset it", EnvOIDCIssuer)
			}
		}
	default:
		const why = "required unless " + EnvDevAuthBypass + " or sign-in (" + EnvOIDCIssuer + ") is set"
		r.require(EnvAccessTeamDomain, c.Access.TeamDomain, why)
		r.require(EnvAccessAUD, c.Access.AUD, why)
	}
	c.Session.Lifetime = r.duration(EnvSessionLifetime, defaultSessionLifetime)

	c.Operator.Emails = r.emails(EnvOperatorEmails)

	c.Ingestion.ConfidenceThreshold = r.float(EnvConfidenceThreshold, defaultConfidenceThreshold, 0, 1)
	c.Ingestion.FetchTimeout = r.duration(EnvFetchTimeout, defaultFetchTimeout)
	c.Ingestion.UserAgent = r.str(EnvUserAgent, defaultUserAgent)

	r.llm(&c.LLM)

	c.Video.ThumbnailHost = r.httpOrigin(EnvVideoThumbnailHost)

	r.backup(&c.Backup)

	if err := errors.Join(r.errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return c, nil
}

// oidc reads the sign-in settings. Plain http is accepted only with the dev
// bypass on, whose listener is loopback, for a local or test issuer.
func (r *reader) oidc(o *OIDC, devBypass bool) {
	o.Issuer = r.str(EnvOIDCIssuer, "")
	o.ClientID = r.str(EnvOIDCClientID, "")
	o.ClientSecret = Secret{r.str(EnvOIDCClientSecret, "")}
	o.PublicBaseURL = r.httpOrigin(EnvPublicBaseURL)

	schemes := "https://"
	if devBypass {
		schemes = "http:// or https://"
	}
	if o.Issuer != "" {
		u, err := url.Parse(o.Issuer)
		schemeOK := err == nil && (u.Scheme == "https" || (devBypass && u.Scheme == "http"))
		if !schemeOK || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			r.fail(EnvOIDCIssuer, "invalid issuer URL %q (want %s and a host, with no query or fragment)", o.Issuer, schemes)
		}
	}
	if !devBypass && strings.HasPrefix(o.PublicBaseURL, "http://") {
		r.fail(EnvPublicBaseURL, "invalid base URL %q (want https:// unless %s is set)", o.PublicBaseURL, EnvDevAuthBypass)
	}

	// PublicBaseURL is empty both when unset and when invalid, so an invalid
	// one alone is reported once, by httpOrigin, rather than as missing.
	baseSet := r.str(EnvPublicBaseURL, "") != ""
	if o.Issuer == "" && o.ClientID == "" && !o.ClientSecret.IsSet() && !baseSet {
		return // sign-in not configured
	}
	const why = "required when any OIDC setting is set"
	r.require(EnvOIDCIssuer, o.Issuer, why)
	r.require(EnvOIDCClientID, o.ClientID, why)
	r.require(EnvOIDCClientSecret, o.ClientSecret.value, why)
	if !baseSet {
		r.fail(EnvPublicBaseURL, "%s", why)
	}
}

func (r *reader) llm(l *LLM) {
	l.Provider = strings.ToLower(r.str(EnvLLMProvider, ""))
	l.TextModel = r.str(EnvLLMTextModel, "")
	l.VisionModel = r.str(EnvLLMVisionModel, "")
	l.Timeout = r.duration(EnvLLMTimeout, defaultLLMTimeout)
	anthropicKey := r.str(EnvAnthropicAPIKey, "")
	geminiKey := r.str(EnvGeminiAPIKey, "")

	var keyEnv, key string
	switch l.Provider {
	case "":
		// Unconfigured is not an error, but settings that would be silently
		// ignored are.
		for _, s := range []struct{ env, v string }{
			{EnvLLMTextModel, l.TextModel}, {EnvLLMVisionModel, l.VisionModel},
			{EnvAnthropicAPIKey, anthropicKey}, {EnvGeminiAPIKey, geminiKey},
		} {
			if s.v != "" {
				r.fail(s.env, "set but %s is not; select a provider or unset it", EnvLLMProvider)
			}
		}
		return
	case ProviderClaude:
		keyEnv, key = EnvAnthropicAPIKey, anthropicKey
	case ProviderGemini:
		keyEnv, key = EnvGeminiAPIKey, geminiKey
	default:
		r.fail(EnvLLMProvider, "unknown provider %q (want %s or %s)", l.Provider, ProviderClaude, ProviderGemini)
		return
	}
	r.require(keyEnv, key, "required for provider "+l.Provider)
	r.require(EnvLLMTextModel, l.TextModel, "required when "+EnvLLMProvider+" is set")
	r.require(EnvLLMVisionModel, l.VisionModel, "required when "+EnvLLMProvider+" is set")
	l.APIKey = Secret{key}
}

func (r *reader) backup(b *Backup) {
	b.Schedule = r.str(EnvBackupSchedule, defaultSchedule)
	if _, err := time.Parse("15:04", b.Schedule); err != nil {
		r.fail(EnvBackupSchedule, "invalid time of day %q (want HH:MM, 24-hour)", b.Schedule)
	}
	b.LocalRetention = r.integer(EnvBackupLocalKeep, defaultLocalRetention, 1)
	b.RemoteRetention = r.integer(EnvBackupRemoteKeep, defaultRemoteRetention, 1)

	s := &b.S3
	s.Endpoint = r.str(EnvS3Endpoint, "")
	s.Region = r.str(EnvS3Region, "")
	s.Bucket = r.str(EnvS3Bucket, "")
	s.Prefix = r.str(EnvS3Prefix, "")
	s.AccessKeyID = Secret{r.str(EnvS3AccessKeyID, "")}
	s.SecretAccessKey = Secret{r.str(EnvS3SecretAccessKey, "")}

	anySet := s.Endpoint != "" || s.Region != "" || s.Bucket != "" || s.Prefix != "" ||
		s.AccessKeyID.IsSet() || s.SecretAccessKey.IsSet()
	if !anySet {
		return // local-only backups
	}
	const why = "required when any S3 setting is set"
	r.require(EnvS3Endpoint, s.Endpoint, why)
	r.require(EnvS3Region, s.Region, why)
	r.require(EnvS3Bucket, s.Bucket, why)
	r.require(EnvS3AccessKeyID, s.AccessKeyID.value, why)
	r.require(EnvS3SecretAccessKey, s.SecretAccessKey.value, why)
}

// reader accumulates validation errors while reading variables.
type reader struct {
	lookup func(string) (string, bool)
	errs   []error
}

func (r *reader) fail(env, format string, args ...any) {
	r.errs = append(r.errs, fmt.Errorf("%s: %s", env, fmt.Sprintf(format, args...)))
}

func (r *reader) require(env, value, why string) {
	if value == "" {
		r.fail(env, "%s", why)
	}
}

// str returns the trimmed value, or def when unset or blank.
func (r *reader) str(env, def string) string {
	if v, ok := r.lookup(env); ok {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return def
}

// emails reads a comma-separated list of email addresses, normalised to lower
// case and deduplicated. A blank list is empty; an entry that is not a bare
// address is an error naming it.
func (r *reader) emails(env string) []string {
	v := r.str(env, "")
	if v == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		e := strings.ToLower(strings.TrimSpace(part))
		if a, err := mail.ParseAddress(e); err != nil || a.Address != e {
			r.fail(env, "invalid email address %q (want a comma-separated list of bare addresses)", strings.TrimSpace(part))
			continue
		}
		if !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	return out
}

// httpOrigin returns the value as an http or https origin without a trailing
// slash, or "" when unset. A path, query, fragment or credentials, or any
// other scheme, is an error.
func (r *reader) httpOrigin(env string) string {
	v := r.str(env, "")
	if v == "" {
		return ""
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.User != nil || strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.Fragment != "" {
		r.fail(env, "invalid origin %q (want http:// or https:// and a host, with no path)", v)
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func (r *reader) boolean(env string) bool {
	v := r.str(env, "")
	if v == "" {
		return false
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		r.fail(env, "invalid boolean %q (want true or false)", v)
	}
	return b
}

func (r *reader) duration(env string, def time.Duration) time.Duration {
	v := r.str(env, "")
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		r.fail(env, "invalid duration %q (want e.g. 15s)", v)
		return def
	}
	if d <= 0 {
		r.fail(env, "duration %q must be positive", v)
	}
	return d
}

func (r *reader) integer(env string, def, lo int) int {
	v := r.str(env, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		r.fail(env, "invalid integer %q", v)
		return def
	}
	if n < lo {
		r.fail(env, "%d is below the minimum %d", n, lo)
	}
	return n
}

func (r *reader) float(env string, def, lo, hi float64) float64 {
	v := r.str(env, "")
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || math.IsNaN(f) {
		r.fail(env, "invalid number %q", v)
		return def
	}
	if f < lo || f > hi {
		r.fail(env, "%v is outside the range %v to %v", f, lo, hi)
	}
	return f
}

func (r *reader) logLevel(env, def string) slog.Level {
	v := strings.ToLower(r.str(env, def))
	var l slog.Level
	switch v {
	case "debug":
		l = slog.LevelDebug
	case "info":
		l = slog.LevelInfo
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		r.fail(env, "unknown log level %q (want debug, info, warn or error)", v)
		l = slog.LevelInfo
	}
	return l
}

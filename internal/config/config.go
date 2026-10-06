// Package config reads every application setting from the environment, once,
// and validates it at start-up. No other package reads the environment for
// application settings.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
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
	EnvConfidenceThreshold = "GOBBLER_INGEST_CONFIDENCE_THRESHOLD"
	EnvFetchTimeout        = "GOBBLER_INGEST_FETCH_TIMEOUT"
	EnvUserAgent           = "GOBBLER_INGEST_USER_AGENT"
	EnvLLMProvider         = "GOBBLER_LLM_PROVIDER"
	EnvLLMTextModel        = "GOBBLER_LLM_TEXT_MODEL"
	EnvLLMVisionModel      = "GOBBLER_LLM_VISION_MODEL"
	EnvLLMTimeout          = "GOBBLER_LLM_TIMEOUT"
	EnvAnthropicAPIKey     = "GOBBLER_ANTHROPIC_API_KEY"
	EnvGeminiAPIKey        = "GOBBLER_GEMINI_API_KEY"
	EnvTodoistToken        = "GOBBLER_TODOIST_TOKEN"
	EnvTodoistProject      = "GOBBLER_TODOIST_PROJECT"
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
)

// Config is the validated application configuration. Load returns it by value
// and nothing mutates it afterwards. Credentials are Secret values, so
// printing or logging a Config never reveals them.
type Config struct {
	Server    Server
	Access    Access
	Ingestion Ingestion
	LLM       LLM
	Todoist   Todoist
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
// when DevAuthBypass is on.
type Access struct {
	TeamDomain string
	AUD        string
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

// Todoist holds Todoist push settings. Token is empty when unconfigured.
type Todoist struct {
	Token   Secret
	Project string
}

// Configured reports whether a Todoist token is set.
func (t Todoist) Configured() bool { return t.Token.IsSet() }

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

	c.Access.TeamDomain = r.str(EnvAccessTeamDomain, "")
	c.Access.AUD = r.str(EnvAccessAUD, "")
	if c.Server.DevAuthBypass {
		c.Access = Access{}
	} else {
		r.require(EnvAccessTeamDomain, c.Access.TeamDomain, "required unless "+EnvDevAuthBypass+" is set")
		r.require(EnvAccessAUD, c.Access.AUD, "required unless "+EnvDevAuthBypass+" is set")
	}

	c.Ingestion.ConfidenceThreshold = r.float(EnvConfidenceThreshold, defaultConfidenceThreshold, 0, 1)
	c.Ingestion.FetchTimeout = r.duration(EnvFetchTimeout, defaultFetchTimeout)
	c.Ingestion.UserAgent = r.str(EnvUserAgent, defaultUserAgent)

	r.llm(&c.LLM)

	c.Todoist.Token = Secret{r.str(EnvTodoistToken, "")}
	c.Todoist.Project = r.str(EnvTodoistProject, "")
	if c.Todoist.Project != "" {
		r.require(EnvTodoistToken, c.Todoist.Token.value, "required when "+EnvTodoistProject+" is set")
	}

	r.backup(&c.Backup)

	if err := errors.Join(r.errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return c, nil
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

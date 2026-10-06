package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

// base is the smallest valid production environment.
func base() map[string]string {
	return map[string]string{
		EnvAccessTeamDomain: "team.cloudflareaccess.com",
		EnvAccessAUD:        "aud-tag",
	}
}

func with(kv ...string) map[string]string {
	m := base()
	for i := 0; i < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

func TestLoadDefaults(t *testing.T) {
	c, err := loadFrom(env(base()))
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.ListenAddr != "127.0.0.1:8080" || c.Server.LogLevel != slog.LevelInfo || c.Server.DevAuthBypass {
		t.Errorf("server defaults: %+v", c.Server)
	}
	if c.Ingestion.ConfidenceThreshold != 0.7 || c.Ingestion.FetchTimeout != 15*time.Second || c.Ingestion.UserAgent == "" {
		t.Errorf("ingestion defaults: %+v", c.Ingestion)
	}
	if c.LLM.Configured() || c.Todoist.Configured() || c.Backup.S3.Configured() {
		t.Error("optional groups must default to unconfigured")
	}
	if c.LLM.TextModel != "" || c.LLM.VisionModel != "" {
		t.Error("no model identifier may be defaulted")
	}
	if c.Backup.Schedule != "03:00" || c.Backup.LocalRetention != 7 || c.Backup.RemoteRetention != 30 {
		t.Errorf("backup defaults: %+v", c.Backup)
	}
}

func TestLoadCompleteEnvironment(t *testing.T) {
	c, err := loadFrom(env(map[string]string{
		EnvListenAddr: "0.0.0.0:9000", EnvDataDir: "/var/lib/gobbler", EnvLogLevel: "DEBUG",
		EnvAccessTeamDomain: "t.example.com", EnvAccessAUD: "aud",
		EnvConfidenceThreshold: "0.55", EnvFetchTimeout: "20s", EnvUserAgent: "ua",
		EnvLLMProvider: "gemini", EnvLLMTextModel: "tm", EnvLLMVisionModel: "vm", EnvLLMTimeout: "90s",
		EnvGeminiAPIKey: "gk", EnvAnthropicAPIKey: "ak",
		EnvTodoistToken: "tok", EnvTodoistProject: "Groceries",
		EnvBackupSchedule: "04:30", EnvBackupLocalKeep: "3", EnvBackupRemoteKeep: "10",
		EnvS3Endpoint: "https://s3.example.com", EnvS3Region: "r", EnvS3Bucket: "b", EnvS3Prefix: "p/",
		EnvS3AccessKeyID: "id", EnvS3SecretAccessKey: "sk",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.ListenAddr != "0.0.0.0:9000" || c.Server.DataDir != "/var/lib/gobbler" || c.Server.LogLevel != slog.LevelDebug {
		t.Errorf("server: %+v", c.Server)
	}
	if c.Access.TeamDomain != "t.example.com" || c.Access.AUD != "aud" {
		t.Errorf("access: %+v", c.Access)
	}
	if c.Ingestion.ConfidenceThreshold != 0.55 || c.Ingestion.FetchTimeout != 20*time.Second || c.Ingestion.UserAgent != "ua" {
		t.Errorf("ingestion: %+v", c.Ingestion)
	}
	if c.LLM.Provider != ProviderGemini || c.LLM.TextModel != "tm" || c.LLM.VisionModel != "vm" ||
		c.LLM.Timeout != 90*time.Second || c.LLM.APIKey.Reveal() != "gk" {
		t.Errorf("llm: %+v", c.LLM)
	}
	if !c.Todoist.Configured() || c.Todoist.Token.Reveal() != "tok" || c.Todoist.Project != "Groceries" {
		t.Errorf("todoist: %+v", c.Todoist)
	}
	b := c.Backup
	if b.Schedule != "04:30" || b.LocalRetention != 3 || b.RemoteRetention != 10 || !b.S3.Configured() ||
		b.S3.Prefix != "p/" || b.S3.AccessKeyID.Reveal() != "id" || b.S3.SecretAccessKey.Reveal() != "sk" {
		t.Errorf("backup: %+v", b)
	}
}

func TestClaudeProviderSelectsAnthropicKey(t *testing.T) {
	c, err := loadFrom(env(with(EnvLLMProvider, "claude", EnvLLMTextModel, "t", EnvLLMVisionModel, "v",
		EnvAnthropicAPIKey, "ak", EnvGeminiAPIKey, "gk")))
	if err != nil {
		t.Fatal(err)
	}
	if c.LLM.APIKey.Reveal() != "ak" {
		t.Errorf("key = %q", c.LLM.APIKey.Reveal())
	}
}

func TestValidationErrorsNameVariable(t *testing.T) {
	llm := []string{EnvLLMProvider, "claude", EnvLLMTextModel, "t", EnvLLMVisionModel, "v"}
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"missing team domain", map[string]string{EnvAccessAUD: "a"}, EnvAccessTeamDomain},
		{"missing aud", map[string]string{EnvAccessTeamDomain: "t"}, EnvAccessAUD},
		{"bad log level", with(EnvLogLevel, "loud"), EnvLogLevel},
		{"bad bool", with(EnvDevAuthBypass, "maybe"), EnvDevAuthBypass},
		{"bad fetch duration", with(EnvFetchTimeout, "soon"), EnvFetchTimeout},
		{"zero fetch duration", with(EnvFetchTimeout, "0s"), EnvFetchTimeout},
		{"negative llm timeout", with(EnvLLMTimeout, "-5s"), EnvLLMTimeout},
		{"threshold not a number", with(EnvConfidenceThreshold, "high"), EnvConfidenceThreshold},
		{"threshold NaN", with(EnvConfidenceThreshold, "NaN"), EnvConfidenceThreshold},
		{"threshold above range", with(EnvConfidenceThreshold, "1.1"), EnvConfidenceThreshold},
		{"threshold below range", with(EnvConfidenceThreshold, "-0.1"), EnvConfidenceThreshold},
		{"unknown provider", with(EnvLLMProvider, "gpt"), EnvLLMProvider},
		{"claude without key", with(llm...), EnvAnthropicAPIKey},
		{"gemini without key", with(EnvLLMProvider, "gemini", EnvLLMTextModel, "t", EnvLLMVisionModel, "v", EnvAnthropicAPIKey, "x"), EnvGeminiAPIKey},
		{"provider without text model", with(EnvLLMProvider, "claude", EnvLLMVisionModel, "v", EnvAnthropicAPIKey, "k"), EnvLLMTextModel},
		{"provider without vision model", with(EnvLLMProvider, "claude", EnvLLMTextModel, "t", EnvAnthropicAPIKey, "k"), EnvLLMVisionModel},
		{"key without provider", with(EnvAnthropicAPIKey, "k"), EnvAnthropicAPIKey},
		{"model without provider", with(EnvLLMTextModel, "m"), EnvLLMTextModel},
		{"todoist project without token", with(EnvTodoistProject, "p"), EnvTodoistToken},
		{"bad schedule", with(EnvBackupSchedule, "25:00"), EnvBackupSchedule},
		{"schedule not a time", with(EnvBackupSchedule, "nightly"), EnvBackupSchedule},
		{"local retention zero", with(EnvBackupLocalKeep, "0"), EnvBackupLocalKeep},
		{"local retention text", with(EnvBackupLocalKeep, "many"), EnvBackupLocalKeep},
		{"remote retention negative", with(EnvBackupRemoteKeep, "-1"), EnvBackupRemoteKeep},
		{"s3 bucket without credentials", with(EnvS3Endpoint, "e", EnvS3Region, "r", EnvS3Bucket, "b"), EnvS3AccessKeyID},
		{"s3 credentials without secret", with(EnvS3Endpoint, "e", EnvS3Region, "r", EnvS3Bucket, "b", EnvS3AccessKeyID, "i"), EnvS3SecretAccessKey},
		{"s3 bucket without endpoint", with(EnvS3Bucket, "b", EnvS3Region, "r", EnvS3AccessKeyID, "i", EnvS3SecretAccessKey, "s"), EnvS3Endpoint},
		{"s3 prefix alone", with(EnvS3Prefix, "p"), EnvS3Endpoint},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadFrom(env(tt.env))
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not name %s", err, tt.want)
			}
		})
	}
}

func TestAllProblemsReportedTogether(t *testing.T) {
	_, err := loadFrom(env(map[string]string{EnvLogLevel: "x", EnvFetchTimeout: "y"}))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, v := range []string{EnvLogLevel, EnvFetchTimeout, EnvAccessTeamDomain, EnvAccessAUD} {
		if !strings.Contains(err.Error(), v) {
			t.Errorf("error does not name %s: %v", v, err)
		}
	}
}

func TestDevBypass(t *testing.T) {
	tests := []struct {
		addr    string
		wantErr bool
	}{
		{"127.0.0.1:8080", false},
		{"127.1.2.3:8080", false},
		{"[::1]:8080", false},
		{"localhost:8080", false},
		{"LOCALHOST:8080", false},
		{"0.0.0.0:8080", true},
		{":8080", true},
		{"[::]:8080", true},
		{"192.168.1.10:8080", true},
		{"203.0.113.7:8080", true},
		{"example.com:8080", true},
		{"nonsense", true},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			// Access settings deliberately absent: not required with bypass.
			c, err := loadFrom(env(map[string]string{EnvDevAuthBypass: "true", EnvListenAddr: tt.addr}))
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), EnvDevAuthBypass) {
					t.Fatalf("want error naming %s, got %v", EnvDevAuthBypass, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !c.Server.DevAuthBypass {
				t.Error("bypass not set")
			}
		})
	}
}

func TestNonLoopbackAllowedWithoutBypass(t *testing.T) {
	if _, err := loadFrom(env(with(EnvListenAddr, "0.0.0.0:8080"))); err != nil {
		t.Fatal(err)
	}
}

func TestDevBypassIgnoresAccessSettings(t *testing.T) {
	c, err := loadFrom(env(map[string]string{EnvDevAuthBypass: "1", EnvAccessAUD: "a"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Access != (Access{}) {
		t.Errorf("access should be cleared under bypass: %+v", c.Access)
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:1": true, "[::1]:1": true, "localhost:1": true, "[::1%lo0]:1": true,
		"[::ffff:127.0.0.1]:1": true, "10.0.0.1:1": false, "": false, "127.0.0.1": false,
	} {
		if got := IsLoopbackAddr(addr); got != want {
			t.Errorf("IsLoopbackAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}

const (
	secretLLM  = "sk-ant-LLMSECRET"
	secretTodo = "TODOISTSECRET"
	secretID   = "S3IDSECRET"
	secretKey  = "S3KEYSECRET"
)

func secretConfig(t *testing.T) Config {
	t.Helper()
	c, err := loadFrom(env(with(
		EnvLLMProvider, "claude", EnvLLMTextModel, "t", EnvLLMVisionModel, "v", EnvAnthropicAPIKey, secretLLM,
		EnvTodoistToken, secretTodo,
		EnvS3Endpoint, "e", EnvS3Region, "r", EnvS3Bucket, "b", EnvS3AccessKeyID, secretID, EnvS3SecretAccessKey, secretKey,
	)))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func assertNoSecrets(t *testing.T, how, out string) {
	t.Helper()
	for _, s := range []string{secretLLM, secretTodo, secretID, secretKey} {
		if strings.Contains(out, s) {
			t.Errorf("%s leaks %q: %s", how, s, out)
		}
	}
	if !strings.Contains(out, redacted) {
		t.Errorf("%s shows no redaction marker: %s", how, out)
	}
}

func TestSecretsRedacted(t *testing.T) {
	c := secretConfig(t)
	verbs := []string{"%v", "%+v", "%#v", "%s", "%d", "%t", "%x", "%X", "%q", "%T", "%p", "%e", "%08d", "%-20s", "%10v", "% x", "%+q", "%#x", "%.2s"}
	for _, verb := range verbs {
		// %T and %p never touch the value; only check what they print.
		if verb == "%T" || verb == "%p" {
			continue
		}
		for _, v := range []any{c, &c, c.LLM, c.Todoist, c.Backup.S3, c.LLM.APIKey, &c.LLM.APIKey} {
			out := fmt.Sprintf(verb, v)
			for _, s := range []string{secretLLM, secretTodo, secretID, secretKey} {
				if strings.Contains(out, s) {
					t.Errorf("%s of %T leaks %q: %s", verb, v, s, out)
				}
			}
		}
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%d", "%t", "%x", "%q"} {
		assertNoSecrets(t, verb, fmt.Sprintf(verb, c))
		assertNoSecrets(t, verb+" pointer", fmt.Sprintf(verb, &c))
	}

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("cfg", "config", c, "key", c.LLM.APIKey)
	assertNoSecrets(t, "slog json", buf.String())
	buf.Reset()
	slog.New(slog.NewTextHandler(&buf, nil)).Info("cfg", "config", c)
	assertNoSecrets(t, "slog text", buf.String())

	j, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	assertNoSecrets(t, "json", string(j))
}

func TestSecretRevealAndEmpty(t *testing.T) {
	if (Secret{}).String() != "" || (Secret{}).IsSet() {
		t.Error("empty secret should render empty and be unset")
	}
	if s := (Secret{"x"}); s.Reveal() != "x" || !s.IsSet() {
		t.Error("Reveal/IsSet broken")
	}
}

func TestErrorsDoNotContainSecrets(t *testing.T) {
	_, err := loadFrom(env(with(EnvLLMProvider, "bogus", EnvAnthropicAPIKey, secretLLM, EnvTodoistProject, "p")))
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secretLLM) {
		t.Errorf("error leaks secret: %v", err)
	}
}

func TestLoadReadsProcessEnvironment(t *testing.T) {
	t.Setenv(EnvAccessTeamDomain, "team")
	t.Setenv(EnvAccessAUD, "aud")
	t.Setenv(EnvDataDir, "/d")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.DataDir != "/d" {
		t.Errorf("DataDir = %q", c.Server.DataDir)
	}
}

func TestBlankValuesTreatedAsUnset(t *testing.T) {
	c, err := loadFrom(env(with(EnvDataDir, "  ", EnvLLMProvider, "")))
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.DataDir != "data" || c.LLM.Configured() {
		t.Errorf("blank not treated as unset: %+v", c)
	}
}

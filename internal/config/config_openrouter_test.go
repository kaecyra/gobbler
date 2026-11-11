package config

import "testing"

func TestOpenRouterURLIsAnOriginWithoutTrailingSlash(t *testing.T) {
	c, err := loadFrom(env(base()))
	if err != nil {
		t.Fatal(err)
	}
	if c.LLMUsage.OpenRouterURL != "" {
		t.Errorf("default OpenRouter URL = %q, want empty (OpenRouter's own)", c.LLMUsage.OpenRouterURL)
	}
	c, err = loadFrom(env(with(EnvOpenRouterURL, "http://127.0.0.1:41700/")))
	if err != nil {
		t.Fatal(err)
	}
	if c.LLMUsage.OpenRouterURL != "http://127.0.0.1:41700" {
		t.Errorf("OpenRouter URL = %q", c.LLMUsage.OpenRouterURL)
	}
	if _, err := loadFrom(env(with(EnvOpenRouterURL, "openrouter.ai"))); err == nil {
		t.Error("a bare host was accepted")
	}
}

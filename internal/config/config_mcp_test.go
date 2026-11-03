package config

import (
	"strings"
	"testing"
)

func TestMCPLimitsDefaultAndAreConfigurable(t *testing.T) {
	c, err := loadFrom(env(base()))
	if err != nil || c.MCP.Rate != defaultMCPRate || c.MCP.Burst != defaultMCPBurst {
		t.Fatalf("defaults = %+v, %v", c.MCP, err)
	}
	c, err = loadFrom(env(with(EnvMCPRate, "0.5", EnvMCPBurst, "3")))
	if err != nil || c.MCP.Rate != 0.5 || c.MCP.Burst != 3 {
		t.Errorf("set = %+v, %v", c.MCP, err)
	}
}

func TestMCPLimitsRefuseNonsense(t *testing.T) {
	for _, kv := range [][2]string{{EnvMCPRate, "0"}, {EnvMCPRate, "fast"}, {EnvMCPBurst, "0"}, {EnvMCPBurst, "many"}} {
		_, err := loadFrom(env(with(kv[0], kv[1])))
		if err == nil || !strings.Contains(err.Error(), kv[0]) {
			t.Errorf("%s=%s: err = %v, want it to name the variable", kv[0], kv[1], err)
		}
	}
}

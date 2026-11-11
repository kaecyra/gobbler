package config

import (
	"strings"
	"testing"
)

func TestMailLimitsDefaultAndAreConfigurable(t *testing.T) {
	c, err := loadFrom(env(base()))
	if err != nil || c.Mail.Rate != defaultMailRate || c.Mail.Burst != defaultMailBurst {
		t.Fatalf("defaults = %+v, %v", c.Mail, err)
	}
	c, err = loadFrom(env(with(EnvMailRate, "0.5", EnvMailBurst, "3")))
	if err != nil || c.Mail.Rate != 0.5 || c.Mail.Burst != 3 {
		t.Errorf("set = %+v, %v", c.Mail, err)
	}
}

func TestMailLimitsRefuseNonsense(t *testing.T) {
	for _, kv := range [][2]string{{EnvMailRate, "0"}, {EnvMailRate, "fast"}, {EnvMailBurst, "0"}, {EnvMailBurst, "many"}} {
		_, err := loadFrom(env(with(kv[0], kv[1])))
		if err == nil || !strings.Contains(err.Error(), kv[0]) {
			t.Errorf("%s=%s: err = %v, want it to name the variable", kv[0], kv[1], err)
		}
	}
}

package config

import (
	"net/netip"
	"strings"
	"testing"
)

func TestOAuthLimitsDefaultAndAreConfigurable(t *testing.T) {
	c, err := loadFrom(env(base()))
	want := OAuth{
		Register: Bucket{defaultOAuthRegisterRate, defaultOAuthRegisterBurst},
		Token:    Bucket{defaultOAuthTokenRate, defaultOAuthTokenBurst},
	}
	if err != nil || c.OAuth != want {
		t.Fatalf("defaults = %+v, %v", c.OAuth, err)
	}
	c, err = loadFrom(env(with(EnvOAuthRegisterRate, "0.5", EnvOAuthRegisterBurst, "3", EnvOAuthTokenRate, "2", EnvOAuthTokenBurst, "4")))
	want = OAuth{Register: Bucket{0.5, 3}, Token: Bucket{2, 4}}
	if err != nil || c.OAuth != want {
		t.Errorf("set = %+v, %v", c.OAuth, err)
	}
}

func TestOAuthLimitsRefuseNonsense(t *testing.T) {
	for _, kv := range [][2]string{
		{EnvOAuthRegisterRate, "0"}, {EnvOAuthRegisterRate, "fast"}, {EnvOAuthRegisterBurst, "0"},
		{EnvOAuthTokenRate, "0"}, {EnvOAuthTokenBurst, "many"},
	} {
		_, err := loadFrom(env(with(kv[0], kv[1])))
		if err == nil || !strings.Contains(err.Error(), kv[0]) {
			t.Errorf("%s=%s: err = %v, want it to name the variable", kv[0], kv[1], err)
		}
	}
}

func TestTrustedProxies(t *testing.T) {
	c, err := loadFrom(env(base()))
	if err != nil || len(c.Server.TrustedProxies) != 0 {
		t.Fatalf("default = %v, %v; want none", c.Server.TrustedProxies, err)
	}
	c, err = loadFrom(env(with(EnvTrustedProxies, " 172.18.0.1, 10.0.0.0/8 ,::1,::ffff:192.0.2.1/128")))
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Prefix{
		netip.MustParsePrefix("172.18.0.1/32"), netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("::1/128"), netip.MustParsePrefix("192.0.2.1/32"),
	}
	if len(c.Server.TrustedProxies) != len(want) {
		t.Fatalf("got %v, want %v", c.Server.TrustedProxies, want)
	}
	for i := range want {
		if c.Server.TrustedProxies[i] != want[i] {
			t.Errorf("entry %d = %v, want %v", i, c.Server.TrustedProxies[i], want[i])
		}
	}
	for _, bad := range []string{"cloudflared", "10.0.0.0/33", "172.18.0.1,,", "::ffff:0:0/64"} {
		_, err := loadFrom(env(with(EnvTrustedProxies, bad)))
		if err == nil || !strings.Contains(err.Error(), EnvTrustedProxies) {
			t.Errorf("%q: err = %v, want it to name the variable", bad, err)
		}
	}
}

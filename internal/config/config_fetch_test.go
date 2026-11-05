package config

import "testing"

// The ingestion fetches may reach loopback only while the dev auth bypass is
// on (ADR-00040), and the setting has no other source.
func TestAllowLoopbackFetchFollowsDevAuthBypass(t *testing.T) {
	off, err := loadFrom(env(base()))
	if err != nil {
		t.Fatal(err)
	}
	if off.Ingestion.AllowLoopbackFetch {
		t.Error("loopback fetches allowed without the dev auth bypass")
	}
	on, err := loadFrom(env(map[string]string{EnvDevAuthBypass: "true"}))
	if err != nil {
		t.Fatal(err)
	}
	if !on.Ingestion.AllowLoopbackFetch {
		t.Error("loopback fetches refused under the dev auth bypass")
	}
}

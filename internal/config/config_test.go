package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigEnvironmentOverridesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"port":3051,"checkProtocolDrift":true,"zdr":true,"useProviderModels":true,"deviceProjectDir":"","cliMode":"learning"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PORT", "4020")
	t.Setenv("CC_CHECK_PROTOCOL_DRIFT", "false")
	t.Setenv("CMD_ZDR", "0")
	t.Setenv("CC_USE_PROVIDER_MODELS", "false")
	t.Setenv("CC_STREAM_IDLE_MS", "300000")
	t.Setenv("CC_MAX_BODY_MB", "3")
	t.Setenv("CC_MAX_INFLIGHT", "8")
	t.Setenv("CC_GATEWAY_ENABLED", "false")
	t.Setenv("CC_DATA_DIR", "custom-data")
	t.Setenv("CC_WEB_DIR", "custom-web")
	t.Setenv("CC_ADMIN_PASSWORD", "fixture-admin-password")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 4020 || c.CheckProtocolDrift || c.ZDR || c.UseProviderModels || c.CLIMode != "learning" || c.DeviceProjectDir != DefaultProjectDir || c.StreamIdle != 300*time.Second || c.MaxBodyBytes != 3<<20 || c.MaxInflight != 8 {
		t.Fatalf("incorrect configuration: %+v", c)
	}
	if c.GatewayEnabled || c.DataDir != "custom-data" || c.WebDir != "custom-web" || c.AdminPassword != "fixture-admin-password" {
		t.Fatal("gateway environment overrides were not applied")
	}
}

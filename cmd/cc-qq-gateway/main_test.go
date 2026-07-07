package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseOptionsDefaultsConfigPath(t *testing.T) {
	opts, err := parseOptions(nil)
	if err != nil {
		t.Fatalf("parseOptions returned error: %v", err)
	}
	if opts.configPath != "config.toml" {
		t.Fatalf("configPath = %q, want config.toml", opts.configPath)
	}
}

func TestParseOptionsAcceptsConfigFlag(t *testing.T) {
	opts, err := parseOptions([]string{"-config", "/tmp/gateway.toml"})
	if err != nil {
		t.Fatalf("parseOptions returned error: %v", err)
	}
	if opts.configPath != "/tmp/gateway.toml" {
		t.Fatalf("configPath = %q, want /tmp/gateway.toml", opts.configPath)
	}
}

func TestRunVersionPrintsVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "cc-qq-gateway v") {
		t.Fatalf("stdout = %q, want version banner", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

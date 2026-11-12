package cli

import (
	"bytes"
	"testing"
)

func TestRootPrintsVersion(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCmd("0.1.0")
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := out.String(); !bytes.Contains([]byte(got), []byte("pr-review 0.1.0")) {
		t.Fatalf("salida inesperada: %q", got)
	}
}

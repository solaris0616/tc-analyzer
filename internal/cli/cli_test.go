package cli

import (
	"bytes"
	"testing"
)

func TestRootCmdHelp(t *testing.T) {
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("RootCmd help failed: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("TwitCasting")) {
		t.Errorf("expected help output to contain TwitCasting, got %s", output)
	}
}

func TestConfigShowCommand(t *testing.T) {
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"config", "show"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Config show failed: %v", err)
	}
}

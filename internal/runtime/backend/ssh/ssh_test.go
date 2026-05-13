package ssh

import (
	"context"
	"os/exec"
	"testing"
)

func TestCommandUsesSystemSSHDefaults(t *testing.T) {
	cmd, err := (Backend{Host: "user@host"}).command(context.Background())
	if err != nil {
		t.Fatalf("command: %v", err)
	}
	if cmd.Path == "" || cmd.Args[0] != "ssh" {
		t.Fatalf("path = %q", cmd.Path)
	}
	want := []string{"ssh", "user@host", "tabula-runtime", "stdio"}
	if got := cmd.Args; len(got) != len(want) {
		t.Fatalf("args = %#v", got)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("args = %#v, want %#v", got, want)
			}
		}
	}
}

func TestCommandAllowsSSHArgsAndRemoteCommand(t *testing.T) {
	cmd, err := (Backend{Host: "build", SSHCommand: []string{"ssh", "-F", "/tmp/ssh_config"}, RemoteCmd: []string{"/opt/tabula/bin/tabula-runtime", "stdio", "--token-file", "/tmp/token"}}).command(context.Background())
	if err != nil {
		t.Fatalf("command: %v", err)
	}
	want := []string{"ssh", "-F", "/tmp/ssh_config", "build", "/opt/tabula/bin/tabula-runtime", "stdio", "--token-file", "/tmp/token"}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Fatalf("args = %#v, want %#v", cmd.Args, want)
		}
	}
}

func TestCommandRejectsMissingHost(t *testing.T) {
	if _, err := (Backend{}).command(context.Background()); err == nil {
		t.Fatal("expected missing host error")
	}
}

func TestLocalhostSSHLoopbackAvailable(t *testing.T) {
	if err := exec.Command("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=2", "localhost", "true").Run(); err != nil {
		t.Skipf("localhost ssh loopback unavailable: %v", err)
	}
}

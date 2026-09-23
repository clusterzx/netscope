package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

// TestIntegrationSSH reads the real Docker engine of the NetScope LXC through an SSH
// tunnel. Enable with NETSCOPE_INTEGRATION=1; the host defaults to 192.168.8.123
// (NETSCOPE_DOCKER_HOST) and the key to ~/.ssh/netscope_lxc (NETSCOPE_DOCKER_KEY).
func TestIntegrationSSH(t *testing.T) {
	if os.Getenv("NETSCOPE_INTEGRATION") != "1" {
		t.Skip("Live-Test: NETSCOPE_INTEGRATION=1 setzen")
	}
	host := os.Getenv("NETSCOPE_DOCKER_HOST")
	if host == "" {
		host = "192.168.8.123"
	}
	keyFile := os.Getenv("NETSCOPE_DOCKER_KEY")
	if keyFile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		keyFile = filepath.Join(home, ".ssh", "netscope_lxc")
	}
	key, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatalf("SSH-Schlüssel: %v", err)
	}
	creds := plugintest.Creds{1: {ID: 1, Name: "lxc", Type: plugin.CredSSH,
		Public: map[string]string{"username": "root"}, Secret: map[string]string{"private_key": string(key)}}}
	obs, rc, err := run(t, map[string]any{"endpoints": []any{"ssh://root@" + host}, "ssh_credential": 1}, creds)
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 1 {
		t.Fatalf("got %d observations, want 1", len(obs))
	}
	o := obs[0]
	inv := o.Inventory.(engineInventory)
	if o.IP != host || o.Hostname == "" || o.Containers == nil || o.Containers.Engine != "docker" ||
		o.Containers.Images == nil || inv.EngineVersion == "" || inv.APIVersion == "" || inv.CPUs == 0 {
		t.Fatalf("observation: ip=%s hostname=%s inventory=%+v", o.IP, o.Hostname, inv)
	}
	for _, c := range o.Containers.Containers {
		if c.Name == "" || strings.HasPrefix(c.Name, "/") || c.ID == "" || c.Image == "" {
			t.Errorf("container: %+v", c)
		}
	}
	kh, err := os.ReadFile(filepath.Join(rc.DataDir, "known_hosts"))
	if err != nil || len(kh) == 0 {
		t.Errorf("host key not pinned: %v", err)
	}
	t.Logf("%s (%s): Docker %s, API %s, %s, %d Container, %d Images, Compose: %v",
		o.Hostname, o.IP, inv.EngineVersion, inv.APIVersion, inv.OS, len(o.Containers.Containers), len(o.Containers.Images), inv.ComposeProjects)
}

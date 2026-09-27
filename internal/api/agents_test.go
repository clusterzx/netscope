package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAgentInstallCommands(t *testing.T) {
	if got := installCommandWindows("http://192.168.8.123:8080", "nse_abc"); got !=
		"& ([scriptblock]::Create((irm 'http://192.168.8.123:8080/agent/install.ps1'))) -Token nse_abc" {
		t.Errorf("http: %s", got)
	}
	if got := installCommandWindows("https://netscope.lan", "nse_abc"); !strings.HasPrefix(got, "[Net.ServicePointManager]::SecurityProtocol = 'Tls12'; & ") {
		t.Errorf("https: %s", got)
	}
	if got := installCommand("https://netscope.lan", "nse_abc", true); got != "curl -fsSL https://netscope.lan/agent/install.sh | sudo sh -s -- --token nse_abc --docker" {
		t.Errorf("linux: %s", got)
	}
}

func TestAgentInstallScripts(t *testing.T) {
	h := newHarness(t)
	for path, want := range map[string]string{"/agent/install.sh": "URL='http://", "/agent/install.ps1": "[string]$Url = 'http://"} {
		resp, err := http.Get(h.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), want) {
			t.Errorf("%s: %d %.200s", path, resp.StatusCode, body)
		}
	}
}

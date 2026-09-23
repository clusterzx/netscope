package proxmox

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// maxBody caps API responses (cluster/resources of large clusters stays far below).
const maxBody = 32 << 20

// client talks to the Proxmox VE REST API with an API token.
type client struct {
	base string // https://host:8006/api2/json
	auth string // PVEAPIToken=user@realm!name=secret
	http *http.Client
}

// apiError is a non-200 answer of the API.
type apiError struct {
	Path   string
	Status int
	Msg    string
}

func (e *apiError) Error() string {
	if e.Msg != "" {
		return fmt.Sprintf("%s: HTTP %d: %s", e.Path, e.Status, e.Msg)
	}
	return fmt.Sprintf("%s: HTTP %d", e.Path, e.Status)
}

// baseURL normalizes the configured URL to the API root (…/api2/json).
func baseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", fmt.Errorf("ungültige Proxmox-URL %q (erwartet z. B. https://pve.lan:8006)", raw)
	}
	p := strings.TrimRight(u.Path, "/")
	p = strings.TrimSuffix(p, "/api2/json")
	u.Path, u.RawPath, u.RawQuery, u.Fragment = p, "", "", ""
	return strings.TrimRight(u.String(), "/") + "/api2/json", nil
}

// normalizeSecret accepts what users paste from the Proxmox token dialog: the bare
// secret (UUID), "user@realm!name=secret" or the whole "PVEAPIToken=…" header value,
// optionally quoted.
func normalizeSecret(tokenID, secret string) string {
	s := strings.Trim(strings.TrimSpace(secret), `"'`)
	s = strings.TrimPrefix(s, "PVEAPIToken=")
	if tokenID != "" {
		s = strings.TrimPrefix(s, tokenID+"=")
	}
	return strings.TrimSpace(s)
}

// newClient builds an API client. tokenID has the form user@realm!name.
func newClient(rawURL, tokenID, secret string, verifyTLS bool, timeout time.Duration) (*client, error) {
	base, err := baseURL(rawURL)
	if err != nil {
		return nil, err
	}
	tokenID = strings.TrimSpace(tokenID)
	if !strings.Contains(tokenID, "@") || !strings.Contains(tokenID, "!") {
		return nil, fmt.Errorf("Token-ID %q hat nicht das Format user@realm!tokenname", tokenID)
	}
	secret = normalizeSecret(tokenID, secret)
	if secret == "" {
		return nil, errors.New("Token-Secret fehlt im Credential")
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: !verifyTLS} //nolint:gosec // user setting for self-signed PVE certificates
	tr.MaxIdleConnsPerHost = 16
	return &client{
		base: base,
		auth: "PVEAPIToken=" + tokenID + "=" + secret,
		http: &http.Client{Transport: tr, Timeout: timeout},
	}, nil
}

func (c *client) close() { c.http.CloseIdleConnections() }

// get fetches path (relative to the API root) and decodes the "data" member into out.
func (c *client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.auth)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	var env struct {
		Data    json.RawMessage `json:"data"`
		Message string          `json:"message"`
	}
	jsonErr := json.Unmarshal(body, &env)
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(env.Message)
		if msg == "" {
			// Proxmox puts the reason into the status line ("401 invalid token value!").
			msg = strings.TrimSpace(strings.TrimPrefix(resp.Status, strconv.Itoa(resp.StatusCode)))
		}
		return &apiError{Path: path, Status: resp.StatusCode, Msg: msg}
	}
	if jsonErr != nil {
		return fmt.Errorf("%s: ungültige JSON-Antwort: %w", path, jsonErr)
	}
	if out == nil || len(env.Data) == 0 || string(env.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// ---------------------------------------------------------------- API types

// num accepts JSON numbers and numeric strings; Proxmox returns both (e.g. memory "4096",
// loadavg ["0.12", …]). Non-numeric values decode as 0.
type num float64

func (n *num) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		*n = 0
		return nil
	}
	*n = num(f)
	return nil
}

func (n num) int() int64 { return int64(n) }

// clusterEntry is an item of GET /cluster/status (type "cluster" or "node").
type clusterEntry struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	IP      string `json:"ip"`
	Online  num    `json:"online"`
	Local   num    `json:"local"`
	NodeID  num    `json:"nodeid"`
	Quorate num    `json:"quorate"`
	Nodes   num    `json:"nodes"`
	Version num    `json:"version"`
}

// nodeEntry is an item of GET /nodes.
type nodeEntry struct {
	Node    string `json:"node"`
	Status  string `json:"status"` // online | offline | unknown
	CPU     num    `json:"cpu"`
	MaxCPU  num    `json:"maxcpu"`
	Mem     num    `json:"mem"`
	MaxMem  num    `json:"maxmem"`
	Disk    num    `json:"disk"`
	MaxDisk num    `json:"maxdisk"`
	Uptime  num    `json:"uptime"`
	Level   string `json:"level"`
}

// nodeStatus is GET /nodes/{node}/status.
type nodeStatus struct {
	PVEVersion string `json:"pveversion"`
	KVersion   string `json:"kversion"`
	Uptime     num    `json:"uptime"`
	CPU        num    `json:"cpu"`
	LoadAvg    []num  `json:"loadavg"`
	CPUInfo    struct {
		Model   string `json:"model"`
		Sockets num    `json:"sockets"`
		Cores   num    `json:"cores"`
		CPUs    num    `json:"cpus"`
		MHz     num    `json:"mhz"`
	} `json:"cpuinfo"`
	Memory struct {
		Total num `json:"total"`
		Used  num `json:"used"`
		Free  num `json:"free"`
	} `json:"memory"`
	Swap struct {
		Total num `json:"total"`
		Used  num `json:"used"`
	} `json:"swap"`
	RootFS struct {
		Total num `json:"total"`
		Used  num `json:"used"`
		Avail num `json:"avail"`
	} `json:"rootfs"`
	CurrentKernel struct {
		Release string `json:"release"`
		Machine string `json:"machine"`
	} `json:"current-kernel"`
	BootInfo struct {
		Mode       string `json:"mode"`
		SecureBoot num    `json:"secureboot"`
	} `json:"boot-info"`
}

// resource is an item of GET /cluster/resources?type=vm.
type resource struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // qemu | lxc
	VMID     num    `json:"vmid"`
	Name     string `json:"name"`
	Node     string `json:"node"`
	Status   string `json:"status"`
	Template num    `json:"template"`
	CPU      num    `json:"cpu"`
	MaxCPU   num    `json:"maxcpu"`
	Mem      num    `json:"mem"`
	MaxMem   num    `json:"maxmem"`
	Disk     num    `json:"disk"`
	MaxDisk  num    `json:"maxdisk"`
	Uptime   num    `json:"uptime"`
	NetIn    num    `json:"netin"`
	NetOut   num    `json:"netout"`
	Tags     string `json:"tags"`
	HAState  string `json:"hastate"`
	Pool     string `json:"pool"`
	Lock     string `json:"lock"`
}

// agentInterfaces is GET /nodes/{node}/qemu/{vmid}/agent/network-get-interfaces.
type agentInterfaces struct {
	Result []guestInterface `json:"result"`
}

// guestInterface is a network interface reported by the QEMU guest agent or, for
// containers, by GET /nodes/{node}/lxc/{vmid}/interfaces (hwaddr/inet/inet6 in PVE 8,
// ip-addresses in newer releases).
type guestInterface struct {
	Name        string `json:"name"`
	HardwareQGA string `json:"hardware-address"`
	HWAddr      string `json:"hwaddr"`
	IPAddresses []struct {
		Address string `json:"ip-address"`
		Type    string `json:"ip-address-type"`
		Prefix  num    `json:"prefix"`
	} `json:"ip-addresses"`
	Inet  string `json:"inet"`
	Inet6 string `json:"inet6"`
}

func (g guestInterface) mac() string {
	if g.HardwareQGA != "" {
		return g.HardwareQGA
	}
	return g.HWAddr
}

// addrs returns all addresses of the interface without prefix length.
func (g guestInterface) addrs() []string {
	var out []string
	for _, a := range g.IPAddresses {
		if a.Address != "" {
			out = append(out, a.Address)
		}
	}
	for _, field := range []string{g.Inet, g.Inet6} {
		for _, f := range strings.FieldsFunc(field, func(r rune) bool { return r == ' ' || r == ',' }) {
			addr, _, _ := strings.Cut(f, "/")
			if addr != "" {
				out = append(out, addr)
			}
		}
	}
	return out
}

// guestConfig is GET /nodes/{node}/{qemu|lxc}/{vmid}/config. Values are strings or numbers.
type guestConfig map[string]any

// str returns a config value as string ("" if unset).
func (c guestConfig) str(key string) string {
	switch v := c[key].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

package nmap

import (
	"strings"

	"netscope/internal/plugins/nmap/nmapxml"

	"netscope/internal/plugin"
)

// deviceTypeMap translates an nmap device class (osclass type or service devicetype,
// lower-cased) into a NetScope device type. "general purpose" and anything unknown map to
// "" (no guess).
var deviceTypeMap = map[string]string{
	"router":           "router",
	"broadband router": "router",
	"switch":           "switch",
	"wap":              "access-point",
	"printer":          "printer",
	"webcam":           "camera",
	"media device":     "media-player",
	"storage-misc":     "nas",
	"phone":            "phone",
	"firewall":         "firewall",
	"print server":     "printer",
	"specialized":      "",
	"general purpose":  "",
}

func mapDeviceType(nmapType string) string {
	return deviceTypeMap[strings.ToLower(strings.TrimSpace(nmapType))]
}

// osInventory is the structured "inventory" section derived from an nmap host scan.
type osInventory struct {
	UptimeSeconds int64        `json:"uptimeSeconds,omitempty"`
	LastBoot      string       `json:"lastBoot,omitempty"`
	Distance      int          `json:"distance,omitempty"`
	OSMatches     []osMatchOut `json:"osMatches,omitempty"`
	Traceroute    []hopOut     `json:"traceroute,omitempty"`
}

type osMatchOut struct {
	Name     string `json:"name"`
	Accuracy int    `json:"accuracy"`
}

type hopOut struct {
	TTL  int    `json:"ttl"`
	IP   string `json:"ip"`
	RTT  string `json:"rtt,omitempty"`
	Host string `json:"host,omitempty"`
}

// buildObservation turns one scanned host into an Observation. minOSAccuracy filters OS
// matches. present tells whether the host counts as actively present.
func buildObservation(run *nmapxml.Run, h *nmapxml.Host, minOSAccuracy int, present bool) *plugin.Observation {
	obs := &plugin.Observation{
		IP:      h.IPv4(),
		Present: present,
		Raw:     h.Raw,
	}
	if mac, vendor := h.MAC(); mac != "" {
		obs.MACs = []string{mac}
		obs.Vendor = vendor
	}
	if obs.IP != "" {
		obs.Target = obs.IP
	}

	// Ports.
	proto := "tcp"
	for _, si := range run.ScanInfo {
		if si.Protocol == "udp" {
			proto = "udp"
		}
	}
	ports := make([]plugin.Port, 0, len(h.Ports))
	var typeGuesses []string
	for _, p := range h.Ports {
		if p.State.State != "open" && p.State.State != "open|filtered" {
			continue
		}
		port := plugin.Port{
			Port:  p.PortID,
			Proto: p.Protocol,
			State: p.State.State,
		}
		if p.Service != nil {
			s := p.Service
			port.Service = s.Name
			port.Product = s.Product
			port.Version = s.Version
			port.ExtraInfo = s.ExtraInfo
			port.Tunnel = s.Tunnel
			port.CPEs = cleanStrings(s.CPEs)
			if s.DeviceType != "" {
				typeGuesses = append(typeGuesses, s.DeviceType)
			}
		}
		if len(p.Scripts) > 0 {
			port.Scripts = map[string]string{}
			for _, sc := range p.Scripts {
				if sc.ID != "" {
					port.Scripts[sc.ID] = sc.Output
				}
			}
		}
		ports = append(ports, port)
	}
	scan := &plugin.PortScan{Protocol: proto, Ports: ports}
	// A timed-out host was not fully scanned, so its scanned range must not close ports.
	if !h.TimedOut {
		scan.Scanned = run.Scanned(proto)
	}
	obs.Ports = scan

	// OS: best match at or above the accuracy threshold.
	if h.OS != nil {
		var best *nmapxml.OSMatch
		for i := range h.OS.Matches {
			m := &h.OS.Matches[i]
			if m.Accuracy < minOSAccuracy {
				continue
			}
			if best == nil || m.Accuracy > best.Accuracy {
				best = m
			}
		}
		if best != nil {
			os := &plugin.OSInfo{Name: best.Name, Accuracy: best.Accuracy}
			if len(best.Classes) > 0 {
				c := best.Classes[0]
				os.Family = c.OSFamily
				os.Vendor = c.Vendor
				os.Generation = c.OSGen
				os.Type = c.Type
			}
			var cpes []string
			for _, c := range best.Classes {
				cpes = append(cpes, c.CPEs...)
			}
			os.CPEs = cleanStrings(cpes)
			obs.OS = os
			for _, c := range best.Classes {
				typeGuesses = append(typeGuesses, c.Type)
			}
		}
	}

	// Device type: first classification that maps to a known NetScope type.
	for _, g := range typeGuesses {
		if t := mapDeviceType(g); t != "" {
			obs.DeviceType = t
			break
		}
	}

	// Inventory: uptime, distance, OS matches, traceroute.
	inv := osInventory{}
	if h.Uptime != nil {
		inv.UptimeSeconds = h.Uptime.Seconds
		inv.LastBoot = h.Uptime.LastBoot
	}
	if h.Distance != nil {
		inv.Distance = h.Distance.Value
	}
	if h.OS != nil {
		for _, m := range h.OS.Matches {
			inv.OSMatches = append(inv.OSMatches, osMatchOut{Name: m.Name, Accuracy: m.Accuracy})
		}
	}
	if h.Trace != nil {
		for _, hop := range h.Trace.Hops {
			inv.Traceroute = append(inv.Traceroute, hopOut{TTL: hop.TTL, IP: hop.IPAddr, RTT: hop.RTT, Host: hop.Host})
		}
	}
	if inv.UptimeSeconds != 0 || inv.Distance != 0 || len(inv.OSMatches) > 0 || len(inv.Traceroute) > 0 {
		obs.Inventory = inv
	}
	return obs
}

func cleanStrings(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

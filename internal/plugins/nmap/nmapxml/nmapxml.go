// Package nmapxml is a streaming parser for nmap's XML output (`-oX -`). It decodes the
// run header and each <host> element as soon as it is complete, so callers can write a
// result per host while nmap is still running. It also captures the raw XML fragment of
// each host and turns a <scaninfo> services list into inclusive port ranges.
package nmapxml

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"netscope/internal/plugin"
)

// Run is the run-level information of an nmap scan (everything before the hosts).
type Run struct {
	Scanner   string     `xml:"scanner,attr"`
	Args      string     `xml:"args,attr"`
	Version   string     `xml:"version,attr"`
	Start     int64      `xml:"start,attr"`
	ScanInfo  []ScanInfo `xml:"-"`
	Finished  bool       `xml:"-"`
	Exit      string     `xml:"-"` // success | error
	ErrorMsg  string     `xml:"-"`
	HostsUp   int        `xml:"-"`
	HostsDown int        `xml:"-"`
}

// ScanInfo describes what was scanned for one protocol.
type ScanInfo struct {
	Type        string `xml:"type,attr"`
	Protocol    string `xml:"protocol,attr"`
	NumServices int    `xml:"numservices,attr"`
	Services    string `xml:"services,attr"`
}

// Scanned returns the scanned ports of the given protocol ("tcp"/"udp") as inclusive
// ranges, taken from the matching <scaninfo services="…">. It returns nil if that
// protocol was not scanned.
func (r *Run) Scanned(proto string) []plugin.PortRange {
	for _, si := range r.ScanInfo {
		if strings.EqualFold(si.Protocol, proto) {
			return ParseRanges(si.Services)
		}
	}
	return nil
}

// Status is a host's up/down status.
type Status struct {
	State  string `xml:"state,attr"`
	Reason string `xml:"reason,attr"`
}

// Address is one address of a host (ipv4, ipv6 or mac).
type Address struct {
	Addr     string `xml:"addr,attr"`
	AddrType string `xml:"addrtype,attr"`
	Vendor   string `xml:"vendor,attr"`
}

// Hostname is a name reported for a host.
type Hostname struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
}

// Service is nmap's service/version detection result for a port.
type Service struct {
	Name       string   `xml:"name,attr"`
	Product    string   `xml:"product,attr"`
	Version    string   `xml:"version,attr"`
	ExtraInfo  string   `xml:"extrainfo,attr"`
	Tunnel     string   `xml:"tunnel,attr"`
	Method     string   `xml:"method,attr"`
	Conf       int      `xml:"conf,attr"`
	OSType     string   `xml:"ostype,attr"`
	DeviceType string   `xml:"devicetype,attr"`
	CPEs       []string `xml:"cpe"`
}

// PortState is the state of a port.
type PortState struct {
	State  string `xml:"state,attr"`
	Reason string `xml:"reason,attr"`
}

// Script is an NSE script result attached to a port.
type Script struct {
	ID     string `xml:"id,attr"`
	Output string `xml:"output,attr"`
}

// Port is one probed port of a host.
type Port struct {
	Protocol string    `xml:"protocol,attr"`
	PortID   int       `xml:"portid,attr"`
	State    PortState `xml:"state"`
	Service  *Service  `xml:"service"`
	Scripts  []Script  `xml:"script"`
}

// OSClass is one OS classification of an osmatch.
type OSClass struct {
	Type     string   `xml:"type,attr"`
	Vendor   string   `xml:"vendor,attr"`
	OSFamily string   `xml:"osfamily,attr"`
	OSGen    string   `xml:"osgen,attr"`
	Accuracy int      `xml:"accuracy,attr"`
	CPEs     []string `xml:"cpe"`
}

// OSMatch is one OS guess with an accuracy.
type OSMatch struct {
	Name     string    `xml:"name,attr"`
	Accuracy int       `xml:"accuracy,attr"`
	Classes  []OSClass `xml:"osclass"`
}

// OS holds the OS detection results of a host.
type OS struct {
	Matches []OSMatch `xml:"osmatch"`
}

// Uptime is the host's uptime guess.
type Uptime struct {
	Seconds  int64  `xml:"seconds,attr"`
	LastBoot string `xml:"lastboot,attr"`
}

// Hop is one traceroute hop.
type Hop struct {
	TTL    int    `xml:"ttl,attr"`
	IPAddr string `xml:"ipaddr,attr"`
	RTT    string `xml:"rtt,attr"`
	Host   string `xml:"host,attr"`
}

// Host is one scanned host.
type Host struct {
	StartTime int64      `xml:"starttime,attr"`
	EndTime   int64      `xml:"endtime,attr"`
	TimedOut  bool       `xml:"timedout,attr"`
	Status    Status     `xml:"status"`
	Addresses []Address  `xml:"address"`
	Hostnames []Hostname `xml:"hostnames>hostname"`
	Ports     []Port     `xml:"ports>port"`
	OS        *OS        `xml:"os"`
	Uptime    *Uptime    `xml:"uptime"`
	Distance  *struct {
		Value int `xml:"value,attr"`
	} `xml:"distance"`
	Trace *struct {
		Port  int    `xml:"port,attr"`
		Proto string `xml:"proto,attr"`
		Hops  []Hop  `xml:"hop"`
	} `xml:"trace"`

	// Raw is the original <host>…</host> XML fragment.
	Raw string `xml:"-"`
}

// IPv4 returns the first IPv4 address of the host ("" if none).
func (h *Host) IPv4() string {
	for _, a := range h.Addresses {
		if a.AddrType == "ipv4" {
			return a.Addr
		}
	}
	return ""
}

// MAC returns the host's MAC address and its vendor ("" if none).
func (h *Host) MAC() (mac, vendor string) {
	for _, a := range h.Addresses {
		if a.AddrType == "mac" {
			return a.Addr, a.Vendor
		}
	}
	return "", ""
}

// Up reports whether the host status is "up".
func (h *Host) Up() bool { return h.Status.State == "up" }

// Parse reads nmap XML from r and calls onHost for every complete <host> element while
// decoding. The run header (scanner, args, scaninfo) is filled before the first host, so
// onHost may read it via the passed *Run. Parsing stops and returns the error if onHost
// returns one. The returned *Run is also populated with the run stats at the end.
func Parse(r io.Reader, onHost func(run *Run, h *Host) error) (*Run, error) {
	run := &Run{}
	var tee bytes.Buffer
	dec := xml.NewDecoder(io.TeeReader(r, &tee))
	dec.Strict = false
	for {
		off := dec.InputOffset()
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return run, nil
		}
		if err != nil {
			return run, fmt.Errorf("nmap-XML: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "nmaprun":
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "scanner":
					run.Scanner = a.Value
				case "args":
					run.Args = a.Value
				case "version":
					run.Version = a.Value
				case "start":
					run.Start, _ = strconv.ParseInt(a.Value, 10, 64)
				}
			}
		case "scaninfo":
			var si ScanInfo
			if err := dec.DecodeElement(&si, &se); err != nil {
				return run, fmt.Errorf("nmap-XML scaninfo: %w", err)
			}
			run.ScanInfo = append(run.ScanInfo, si)
		case "finished":
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "exit":
					run.Exit = a.Value
				case "errormsg":
					run.ErrorMsg = a.Value
				}
			}
			run.Finished = true
		case "hosts":
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "up":
					run.HostsUp, _ = strconv.Atoi(a.Value)
				case "down":
					run.HostsDown, _ = strconv.Atoi(a.Value)
				}
			}
		case "host":
			var h Host
			if err := dec.DecodeElement(&h, &se); err != nil {
				return run, fmt.Errorf("nmap-XML host: %w", err)
			}
			end := dec.InputOffset()
			if b := tee.Bytes(); off >= 0 && int(end) <= len(b) && off < end {
				h.Raw = strings.TrimSpace(string(b[off:end]))
			}
			if err := onHost(run, &h); err != nil {
				return run, err
			}
		}
	}
}

// ParseRanges turns an nmap services list ("1,3-4,22,8000-8100") into inclusive ranges.
// Malformed entries are skipped.
func ParseRanges(s string) []plugin.PortRange {
	var out []plugin.PortRange
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if from, to, ok := strings.Cut(part, "-"); ok {
			f, err1 := strconv.Atoi(strings.TrimSpace(from))
			t, err2 := strconv.Atoi(strings.TrimSpace(to))
			if err1 == nil && err2 == nil && f >= 0 && t >= f && t <= 65535 {
				out = append(out, plugin.PortRange{From: f, To: t})
			}
			continue
		}
		if p, err := strconv.Atoi(part); err == nil && p >= 0 && p <= 65535 {
			out = append(out, plugin.PortRange{From: p, To: p})
		}
	}
	return out
}

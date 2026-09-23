package upnp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/textproto"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	fetchTimeout = 3 * time.Second
	maxDescSize  = 512 << 10
	maxDepth     = 8 // nesting of embedded devices that is evaluated
)

// ssdpResponse is the relevant part of an M-SEARCH response.
type ssdpResponse struct {
	Location string `json:"location"`
	Server   string `json:"server,omitempty"`
	ST       string `json:"st,omitempty"`
	USN      string `json:"usn,omitempty"`
}

// parseSSDP parses an HTTP-over-UDP M-SEARCH response ("HTTP/1.1 200 OK" + headers).
func parseSSDP(b []byte) (ssdpResponse, bool) {
	r := textproto.NewReader(bufio.NewReader(bytes.NewReader(b)))
	status, err := r.ReadLine()
	if err != nil {
		return ssdpResponse{}, false
	}
	proto, code, _ := strings.Cut(status, " ")
	if !strings.HasPrefix(strings.ToUpper(proto), "HTTP/") || !strings.HasPrefix(strings.TrimSpace(code), "200") {
		return ssdpResponse{}, false
	}
	h, err := r.ReadMIMEHeader()
	if err != nil && len(h) == 0 {
		return ssdpResponse{}, false
	}
	resp := ssdpResponse{
		Location: strings.TrimSpace(h.Get("Location")),
		Server:   strings.TrimSpace(h.Get("Server")),
		ST:       strings.TrimSpace(h.Get("St")),
		USN:      strings.TrimSpace(h.Get("Usn")),
	}
	return resp, resp.Location != "" || resp.USN != ""
}

// Device is a UPnP device from a description document (root or embedded).
type Device struct {
	DeviceType       string    `xml:"deviceType" json:"deviceType"`
	FriendlyName     string    `xml:"friendlyName" json:"friendlyName,omitempty"`
	Manufacturer     string    `xml:"manufacturer" json:"manufacturer,omitempty"`
	ManufacturerURL  string    `xml:"manufacturerURL" json:"manufacturerURL,omitempty"`
	ModelName        string    `xml:"modelName" json:"modelName,omitempty"`
	ModelNumber      string    `xml:"modelNumber" json:"modelNumber,omitempty"`
	ModelDescription string    `xml:"modelDescription" json:"modelDescription,omitempty"`
	ModelURL         string    `xml:"modelURL" json:"modelURL,omitempty"`
	SerialNumber     string    `xml:"serialNumber" json:"serialNumber,omitempty"`
	UDN              string    `xml:"UDN" json:"udn,omitempty"`
	PresentationURL  string    `xml:"presentationURL" json:"presentationURL,omitempty"`
	Services         []Service `xml:"serviceList>service" json:"services,omitempty"`
	Devices          []Device  `xml:"deviceList>device" json:"devices,omitempty"`
}

// Service is a service of a UPnP device.
type Service struct {
	ServiceType string `xml:"serviceType" json:"serviceType"`
	ServiceID   string `xml:"serviceId" json:"serviceId,omitempty"`
}

type descRoot struct {
	XMLName xml.Name `xml:"root"`
	Device  Device   `xml:"device"`
}

// parseDescription parses a device description document.
func parseDescription(b []byte) (*Device, error) {
	d := xml.NewDecoder(bytes.NewReader(b))
	d.Strict = false
	d.CharsetReader = charsetReader
	var root descRoot
	if err := d.Decode(&root); err != nil {
		return nil, fmt.Errorf("ungültige Gerätebeschreibung: %w", err)
	}
	clean(&root.Device, 0)
	if root.Device.DeviceType == "" && root.Device.FriendlyName == "" && root.Device.UDN == "" {
		return nil, errors.New("Gerätebeschreibung ohne <device>")
	}
	return &root.Device, nil
}

// charsetReader supports the non-UTF-8 encodings seen in device descriptions.
func charsetReader(label string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "utf-8", "utf8", "us-ascii", "ascii", "":
		return input, nil
	case "iso-8859-1", "iso8859-1", "latin1", "latin-1", "windows-1252", "cp1252":
		b, err := io.ReadAll(input)
		if err != nil {
			return nil, err
		}
		runes := make([]rune, len(b))
		for i, c := range b {
			runes[i] = rune(c)
		}
		return strings.NewReader(string(runes)), nil
	}
	// Unknown encodings are read as UTF-8: most fields are ASCII anyway.
	return input, nil
}

// clean trims all fields and drops devices nested deeper than maxDepth.
func clean(d *Device, depth int) {
	for _, f := range []*string{&d.DeviceType, &d.FriendlyName, &d.Manufacturer, &d.ManufacturerURL, &d.ModelName,
		&d.ModelNumber, &d.ModelDescription, &d.ModelURL, &d.SerialNumber, &d.UDN, &d.PresentationURL} {
		*f = strings.TrimFunc(*f, unicode.IsSpace)
	}
	for i := range d.Services {
		d.Services[i].ServiceType = strings.TrimSpace(d.Services[i].ServiceType)
		d.Services[i].ServiceID = strings.TrimSpace(d.Services[i].ServiceID)
	}
	if depth >= maxDepth {
		d.Devices = nil
		return
	}
	for i := range d.Devices {
		clean(&d.Devices[i], depth+1)
	}
}

// deviceCategory maps UPnP device type names to device types.
var deviceCategory = map[string]string{
	"internetgatewaydevice": "router",
	"wandevice":             "router",
	"wanconnectiondevice":   "router",
	"wlanaccesspointdevice": "access-point",
	"mediarenderer":         "media-player",
	"mediaserver":           "media-player",
	"dial":                  "media-player",
	"zoneplayer":            "speaker",
	"tvdevice":              "tv",
	"remotecontrolreceiver": "tv",
	"printer":               "printer",
	"printerbasic":          "printer",
	"printerenhanced":       "printer",
	"scanner":               "printer",
	"digitalsecuritycamera": "camera",
	"binarylight":           "smart-home",
	"dimmablelight":         "smart-home",
	"hvac_system":           "smart-home",
	"hvac_zonethermostat":   "smart-home",
	"solarprotectionblind":  "smart-home",
}

// typeName extracts "MediaRenderer" from "urn:schemas-upnp-org:device:MediaRenderer:1".
func typeName(urn string) string {
	parts := strings.Split(urn, ":")
	if len(parts) >= 5 && strings.EqualFold(parts[0], "urn") {
		return parts[3]
	}
	return ""
}

// category returns the device type of d, looking at embedded devices if the root type
// is not conclusive.
func category(d *Device) string {
	return categoryDepth(d, 0)
}

func categoryDepth(d *Device, depth int) string {
	if c := deviceCategory[strings.ToLower(typeName(d.DeviceType))]; c != "" {
		return c
	}
	if depth >= maxDepth {
		return ""
	}
	for i := range d.Devices {
		if c := categoryDepth(&d.Devices[i], depth+1); c != "" {
			return c
		}
	}
	return ""
}

// categoryRank orders root devices of one host: the most specific hardware role first.
var categoryRank = map[string]int{
	"router": 0, "access-point": 1, "printer": 2, "tv": 3, "camera": 4, "speaker": 5, "media-player": 6, "smart-home": 7,
}

func rank(c string) int {
	if r, ok := categoryRank[c]; ok {
		return r
	}
	return len(categoryRank)
}

// modelOf combines modelName and modelNumber unless the number is already part of the
// name ("FRITZ!Box 7590" / "7590avm").
func modelOf(d *Device) string {
	name, num := d.ModelName, d.ModelNumber
	if name == "" {
		return num
	}
	if num == "" {
		return name
	}
	ln, lnum := strings.ToLower(name), strings.ToLower(num)
	if strings.Contains(ln, lnum) {
		return name
	}
	for _, tok := range strings.FieldsFunc(ln, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len(tok) >= 3 && strings.ContainsAny(tok, "0123456789") && strings.Contains(lnum, tok) {
			return name
		}
	}
	return name + " " + num
}

// description is a fetched and parsed description document of one host.
type description struct {
	Location string  `json:"location"`
	Device   *Device `json:"device"`
}

// primary chooses the root device that describes the host best.
func primary(descs []description) *description {
	if len(descs) == 0 {
		return nil
	}
	sorted := append([]description(nil), descs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		ri, rj := rank(category(sorted[i].Device)), rank(category(sorted[j].Device))
		if ri != rj {
			return ri < rj
		}
		if sorted[i].Device.DeviceType != sorted[j].Device.DeviceType {
			return sorted[i].Device.DeviceType < sorted[j].Device.DeviceType
		}
		return sorted[i].Location < sorted[j].Location
	})
	return &sorted[0]
}

var errForeignLocation = errors.New("LOCATION verweist nicht auf das antwortende Gerät")

// checkLocation allows only http(s) URLs whose host is the responding address itself
// (protection against SSRF through forged SSDP responses).
func checkLocation(loc string, responder netip.Addr) (*url.URL, error) {
	u, err := url.Parse(loc)
	if err != nil {
		return nil, fmt.Errorf("ungültige LOCATION %q", loc)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("LOCATION %q: nur http und https erlaubt", loc)
	}
	ip, err := netip.ParseAddr(u.Hostname())
	if err != nil || ip.Unmap() != responder.Unmap() || u.User != nil {
		return nil, errForeignLocation
	}
	return u, nil
}

// newHTTPClient returns the client for description downloads: no proxy, no redirects,
// short timeouts, self-signed certificates accepted (read-only metadata of LAN devices).
func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: fetchTimeout,
		Transport: &http.Transport{
			Proxy:                  nil,
			DialContext:            (&net.Dialer{Timeout: fetchTimeout}).DialContext,
			TLSClientConfig:        &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // device metadata only
			TLSHandshakeTimeout:    fetchTimeout,
			ResponseHeaderTimeout:  fetchTimeout,
			DisableKeepAlives:      true,
			MaxResponseHeaderBytes: 64 << 10,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// fetchDescription downloads and parses the description at loc (which must point to
// responder).
func fetchDescription(ctx context.Context, client *http.Client, loc string, responder netip.Addr) (*Device, []byte, error) {
	u, err := checkLocation(loc, responder)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "NetScope/1.0 UPnP/1.1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("HTTP-Status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxDescSize+1))
	if err != nil {
		return nil, nil, err
	}
	if len(b) > maxDescSize {
		return nil, nil, fmt.Errorf("Gerätebeschreibung größer als %d KB", maxDescSize>>10)
	}
	dev, err := parseDescription(b)
	if err != nil {
		return nil, nil, err
	}
	return dev, b, nil
}

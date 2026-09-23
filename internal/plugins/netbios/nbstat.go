package netbios

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// NetBIOS name flags (RFC 1002, section 4.2.18).
const (
	flagGroup      = 0x8000
	flagDeregister = 0x1000
	flagConflict   = 0x0800
	flagActive     = 0x0400
	flagPermanent  = 0x0200
	typeNBSTAT     = 0x0021
)

// request builds a NODE STATUS REQUEST for the wildcard name "*".
func request(id uint16) []byte {
	b := make([]byte, 0, 50)
	b = binary.BigEndian.AppendUint16(b, id)
	b = append(b,
		0x00, 0x00, // flags: query, no broadcast
		0x00, 0x01, // QDCOUNT
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // AN, NS, AR
		0x20, // encoded name length
	)
	var name [16]byte
	name[0] = '*'
	for _, c := range name {
		b = append(b, 'A'+c>>4, 'A'+c&0x0f)
	}
	return append(b, 0x00, 0x00, typeNBSTAT, 0x00, 0x01) // end of name, NBSTAT, IN
}

// NameEntry is one entry of a node's name table.
type NameEntry struct {
	Name   string `json:"name"`
	Suffix string `json:"suffix"` // e.g. "0x20"
	Group  bool   `json:"group"`
	Type   string `json:"type"` // B, P, M or H node
	Active bool   `json:"active"`
	// Flags lists further states: "permanent", "conflict", "deregistering".
	Flags []string `json:"flags,omitempty"`

	suffix byte
}

// Status is a parsed NODE STATUS RESPONSE.
type Status struct {
	ID    uint16      `json:"-"`
	Names []NameEntry `json:"names"`
	MAC   string      `json:"mac,omitempty"` // unit ID, empty if zero (Samba)
}

var errShort = errors.New("NBSTAT-Antwort zu kurz")

// skipName skips an encoded (possibly compressed) name starting at off.
func skipName(b []byte, off int) (int, error) {
	for {
		if off >= len(b) {
			return 0, errShort
		}
		l := int(b[off])
		switch {
		case l == 0:
			return off + 1, nil
		case l&0xC0 == 0xC0:
			if off+2 > len(b) {
				return 0, errShort
			}
			return off + 2, nil
		case l&0xC0 != 0:
			return 0, errors.New("ungültiges Namensformat")
		}
		off += 1 + l
	}
}

// parseStatus parses a NODE STATUS RESPONSE.
func parseStatus(b []byte) (*Status, error) {
	if len(b) < 12 {
		return nil, errShort
	}
	flags := binary.BigEndian.Uint16(b[2:4])
	if flags&0x8000 == 0 {
		return nil, errors.New("keine Antwort")
	}
	if rcode := flags & 0x000F; rcode != 0 {
		return nil, fmt.Errorf("Fehlercode %d", rcode)
	}
	qd := int(binary.BigEndian.Uint16(b[4:6]))
	an := int(binary.BigEndian.Uint16(b[6:8]))
	if an < 1 {
		return nil, errors.New("Antwort ohne Namenstabelle")
	}
	off := 12
	var err error
	for range qd {
		if off, err = skipName(b, off); err != nil {
			return nil, err
		}
		off += 4
	}
	if off, err = skipName(b, off); err != nil {
		return nil, err
	}
	if off+10 > len(b) {
		return nil, errShort
	}
	if typ := binary.BigEndian.Uint16(b[off : off+2]); typ != typeNBSTAT {
		return nil, fmt.Errorf("unerwarteter Record-Typ 0x%04x", typ)
	}
	rdlen := int(binary.BigEndian.Uint16(b[off+8 : off+10]))
	off += 10
	if off+rdlen > len(b) {
		// some stacks announce more statistics than they send
		rdlen = len(b) - off
	}
	rdata := b[off : off+rdlen]
	if len(rdata) < 1 {
		return nil, errShort
	}
	n := int(rdata[0])
	if 1+n*18 > len(rdata) {
		return nil, errShort
	}
	st := &Status{ID: binary.BigEndian.Uint16(b[0:2])}
	for i := range n {
		e := rdata[1+i*18 : 1+(i+1)*18]
		f := binary.BigEndian.Uint16(e[16:18])
		entry := NameEntry{
			Name:   decodeName(e[:15]),
			suffix: e[15],
			Suffix: fmt.Sprintf("0x%02X", e[15]),
			Group:  f&flagGroup != 0,
			Type:   [4]string{"B", "P", "M", "H"}[(f>>13)&0x3],
			Active: f&flagActive != 0,
		}
		if f&flagPermanent != 0 {
			entry.Flags = append(entry.Flags, "permanent")
		}
		if f&flagConflict != 0 {
			entry.Flags = append(entry.Flags, "conflict")
		}
		if f&flagDeregister != 0 {
			entry.Flags = append(entry.Flags, "deregistering")
		}
		st.Names = append(st.Names, entry)
	}
	if unit := rdata[1+n*18:]; len(unit) >= 6 {
		mac := unit[:6]
		zero := true
		for _, c := range mac {
			if c != 0 {
				zero = false
			}
		}
		if !zero {
			st.MAC = fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
		}
	}
	return st, nil
}

// decodeName converts a padded 15-byte NetBIOS name (OEM code page, treated as
// Latin-1) to a string; control characters become ".".
func decodeName(b []byte) string {
	for len(b) > 0 && (b[len(b)-1] == 0x20 || b[len(b)-1] == 0x00) {
		b = b[:len(b)-1]
	}
	var sb strings.Builder
	for _, c := range b {
		switch {
		case c < 0x20 || c == 0x7F:
			sb.WriteByte('.')
		case c < 0x80:
			sb.WriteByte(c)
		default:
			sb.WriteRune(rune(c))
		}
	}
	return sb.String()
}

func (e NameEntry) special() bool { return strings.Contains(e.Name, "__MSBROWSE__") }

// Hostname returns the workstation name (unique <00>, falling back to the unique <20>
// server name).
func (s *Status) Hostname() string {
	for _, want := range []byte{0x00, 0x20} {
		for _, e := range s.Names {
			if !e.Group && e.suffix == want && e.Name != "" && !e.special() {
				return e.Name
			}
		}
	}
	return ""
}

// Domain returns the workgroup or domain (group <00>, falling back to group <1E>).
func (s *Status) Domain() string {
	for _, want := range []byte{0x00, 0x1E} {
		for _, e := range s.Names {
			if e.Group && e.suffix == want && e.Name != "" && !e.special() {
				return e.Name
			}
		}
	}
	return ""
}

// Table renders the name table like nmblookup -A.
func (s *Status) Table() string {
	var b strings.Builder
	for _, e := range s.Names {
		kind := "-      "
		if e.Group {
			kind = "<GROUP>"
		}
		state := ""
		if e.Active {
			state = " <ACTIVE>"
		}
		for _, f := range e.Flags {
			state += " <" + strings.ToUpper(f) + ">"
		}
		fmt.Fprintf(&b, "%-15s <%02x> %s %s%s\n", e.Name, e.suffix, kind, e.Type, state)
	}
	if s.MAC != "" {
		fmt.Fprintf(&b, "MAC Address = %s\n", strings.ToUpper(strings.ReplaceAll(s.MAC, ":", "-")))
	}
	return b.String()
}

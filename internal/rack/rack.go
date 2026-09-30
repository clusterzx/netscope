// Package rack documents the physical setup: racks with height units, the devices and
// passive elements (patch panels, shelves …) mounted in them, the ports of these elements
// and the patch cables between them.
//
// Ports come from the inventory where possible (SNMP interfaces, ports named by imports and
// the topology) and are otherwise counted by hand. What is plugged into a port is detected
// automatically (switch-port and LLDP relations of other sources) or set by hand: a device
// directly on the port, or a patch cable – through patch panels – to another port. The
// connections set in the racks are kept as relations with source "rack" (protected, like
// manual edges), so the topology shows them too and does not re-derive those devices.
package rack

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"netscope/internal/bus"
	"netscope/internal/db"
	"netscope/internal/plugin"
)

// Source is the relation source of connections set in the racks.
const Source = "rack"

// Kinds of rack items.
const (
	KindDevice       = "device"
	KindPatchPanel   = "patch_panel"
	KindShelf        = "shelf"
	KindBlank        = "blank"
	KindCableManager = "cable_manager"
	KindPDU          = "pdu"
	KindOther        = "other"
)

// Kinds lists the kinds of rack items.
var Kinds = []string{KindDevice, KindPatchPanel, KindShelf, KindBlank, KindCableManager, KindPDU, KindOther}

// Limits.
const (
	MaxHeight = 60
	MaxPorts  = 128
	// Columns is the width grid of a rack: items are 6 (full), 3 (half) or 2 (third) wide.
	Columns = 6
)

// Service manages the racks.
type Service struct {
	db  *db.DB
	bus *bus.Bus
}

// New creates the service (b may be nil).
func New(d *db.DB, b *bus.Bus) *Service { return &Service{db: d, bus: b} }

// Rack is a rack.
type Rack struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Location  string    `json:"location"`
	Width     string    `json:"width"`     // 19 | 10 (inch)
	Height    int       `json:"height"`    // height units
	Numbering string    `json:"numbering"` // bottom (U1 at the bottom) | top
	Notes     string    `json:"notes"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// RackInput creates or changes a rack.
type RackInput struct {
	Name      string `json:"name"`
	Location  string `json:"location,omitempty"`
	Width     string `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	Numbering string `json:"numbering,omitempty"`
	Notes     string `json:"notes,omitempty"`
}

// Summary is a rack in the list.
type Summary struct {
	Rack
	Items   int `json:"items"`
	Devices int `json:"devices"`
	Offline int `json:"offline"`
	// UsedUnits counts the height units with at least one item (either face).
	UsedUnits int `json:"usedUnits"`
}

// DeviceRef is a device as shown in a rack.
type DeviceRef struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type,omitempty"`
	IP     string `json:"ip,omitempty"`
	Vendor string `json:"vendor,omitempty"`
	Model  string `json:"model,omitempty"`
	Online bool   `json:"online"`
}

// Item is an element mounted in a rack.
type Item struct {
	ID       int64      `json:"id"`
	RackID   int64      `json:"rackId"`
	Kind     string     `json:"kind"`
	DeviceID int64      `json:"deviceId,omitempty"`
	Device   *DeviceRef `json:"device,omitempty"`
	// DeviceName is the name of the device when it was mounted (shown when the device was
	// deleted from the inventory since).
	DeviceName string `json:"deviceName,omitempty"`
	Label      string `json:"label"`
	Position   int    `json:"position"` // lowest height unit, 1 = bottom of the rack
	Height     int    `json:"height"`
	Face       string `json:"face"` // front | rear
	FullDepth  bool   `json:"fullDepth"`
	Col        int    `json:"col"`  // 0..5 (sixths of the width)
	Cols       int    `json:"cols"` // 6 | 3 | 2
	PortCount  int    `json:"portCount"`
	PortPrefix string `json:"portPrefix"`
	Ports      []Port `json:"ports"`
}

// ItemInput mounts or changes an item.
type ItemInput struct {
	// RackID moves the item into another rack (changes only; 0 = stays).
	RackID     int64  `json:"rackId,omitempty"`
	Kind       string `json:"kind"`
	DeviceID   int64  `json:"deviceId,omitempty"`
	Label      string `json:"label,omitempty"`
	Position   int    `json:"position"`
	Height     int    `json:"height,omitempty"`
	Face       string `json:"face,omitempty"`
	FullDepth  bool   `json:"fullDepth,omitempty"`
	Col        int    `json:"col,omitempty"`
	Cols       int    `json:"cols,omitempty"`
	PortCount  int    `json:"portCount,omitempty"`
	PortPrefix string `json:"portPrefix,omitempty"`
}

// View is a rack with its items.
type View struct {
	Rack
	Items []Item `json:"items"`
}

func trimmed(s *string, max int) {
	*s = strings.TrimSpace(*s)
	if r := []rune(*s); len(r) > max {
		*s = string(r[:max])
	}
}

func (in *RackInput) normalize() error {
	trimmed(&in.Name, 100)
	trimmed(&in.Location, 200)
	in.Notes = strings.TrimSpace(in.Notes)
	var errs []plugin.FieldError
	if in.Name == "" {
		errs = append(errs, plugin.FieldError{Field: "name", Message: "Name fehlt"})
	}
	if in.Width == "" {
		in.Width = "19"
	}
	if in.Width != "19" && in.Width != "10" {
		errs = append(errs, plugin.FieldError{Field: "width", Message: "19 oder 10 Zoll erwartet"})
	}
	if in.Height == 0 {
		in.Height = 42
	}
	if in.Height < 1 || in.Height > MaxHeight {
		errs = append(errs, plugin.FieldError{Field: "height", Message: fmt.Sprintf("1 bis %d Höheneinheiten erwartet", MaxHeight)})
	}
	if in.Numbering == "" {
		in.Numbering = "bottom"
	}
	if in.Numbering != "bottom" && in.Numbering != "top" {
		errs = append(errs, plugin.FieldError{Field: "numbering", Message: "bottom oder top erwartet"})
	}
	if len(errs) > 0 {
		return &plugin.ValidationError{Errors: errs}
	}
	return nil
}

const rackCols = "id, name, location, width, height, numbering, notes, created_at, updated_at"

func scanRack(sc interface{ Scan(...any) error }) (Rack, error) {
	var (
		r        Rack
		cat, uat int64
	)
	err := sc.Scan(&r.ID, &r.Name, &r.Location, &r.Width, &r.Height, &r.Numbering, &r.Notes, &cat, &uat)
	r.CreatedAt, r.UpdatedAt = db.Time(cat), db.Time(uat)
	return r, err
}

// List returns all racks with their occupancy.
func (s *Service) List(ctx context.Context) ([]Summary, error) {
	rows, err := s.db.R.QueryContext(ctx, "SELECT "+rackCols+" FROM racks ORDER BY name COLLATE NOCASE, id")
	if err != nil {
		return nil, err
	}
	out := []Summary{}
	idx := map[int64]int{}
	for rows.Next() {
		r, err := scanRack(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		idx[r.ID] = len(out)
		out = append(out, Summary{Rack: r})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	irows, err := s.db.R.QueryContext(ctx, `SELECT i.rack_id, i.position, i.height, i.device_id IS NOT NULL, IFNULL(d.online, 1)
		FROM rack_items i LEFT JOIN devices d ON d.id = i.device_id`)
	if err != nil {
		return nil, err
	}
	defer irows.Close()
	used := map[int64]map[int]bool{}
	for irows.Next() {
		var (
			rack           int64
			pos, h         int
			device, online bool
		)
		if err := irows.Scan(&rack, &pos, &h, &device, &online); err != nil {
			return nil, err
		}
		i, ok := idx[rack]
		if !ok {
			continue
		}
		out[i].Items++
		if device {
			out[i].Devices++
			if !online {
				out[i].Offline++
			}
		}
		if used[rack] == nil {
			used[rack] = map[int]bool{}
		}
		for u := pos; u < pos+h; u++ {
			used[rack][u] = true
		}
	}
	for id, us := range used {
		out[idx[id]].UsedUnits = len(us)
	}
	return out, irows.Err()
}

// Get returns one rack.
func (s *Service) Get(ctx context.Context, id int64) (*Rack, error) {
	r, err := scanRack(s.db.R.QueryRowContext(ctx, "SELECT "+rackCols+" FROM racks WHERE id = ?", id))
	if err != nil {
		return nil, db.NotFound(err)
	}
	return &r, nil
}

// Create adds a rack.
func (s *Service) Create(ctx context.Context, in RackInput) (*Rack, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	now := db.Now()
	res, err := s.db.W.ExecContext(ctx, `INSERT INTO racks(name, location, width, height, numbering, notes, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, in.Name, in.Location, in.Width, in.Height, in.Numbering, in.Notes, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.Get(ctx, id)
}

// Update changes a rack. A lower rack must still hold all its items.
func (s *Service) Update(ctx context.Context, id int64, in RackInput) (*Rack, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var top sql.NullInt64
		if err := tx.QueryRowContext(ctx, "SELECT MAX(position + height - 1) FROM rack_items WHERE rack_id = ?", id).Scan(&top); err != nil {
			return err
		}
		if top.Valid && int(top.Int64) > in.Height {
			return plugin.FieldErr("height", fmt.Sprintf("Das Rack ist bis HE %d belegt", top.Int64))
		}
		res, err := tx.ExecContext(ctx, `UPDATE racks SET name = ?, location = ?, width = ?, height = ?, numbering = ?, notes = ?, updated_at = ?
			WHERE id = ?`, in.Name, in.Location, in.Width, in.Height, in.Numbering, in.Notes, db.Now(), id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return db.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Delete removes a rack with everything mounted in it (and the cables of these items).
func (s *Service) Delete(ctx context.Context, id int64) (*Rack, error) {
	r, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.W.ExecContext(ctx, "DELETE FROM racks WHERE id = ?", id); err != nil {
		return nil, err
	}
	return r, s.Sync(ctx)
}

// View returns a rack with its items and their ports.
func (s *Service) View(ctx context.Context, id int64) (*View, error) {
	r, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	st, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	v := &View{Rack: *r, Items: []Item{}}
	for _, it := range st.items {
		if it.RackID == id {
			v.Items = append(v.Items, st.item(it))
		}
	}
	return v, nil
}

// ---------------------------------------------------------------- items

func (in *ItemInput) normalize() error {
	trimmed(&in.Label, 100)
	trimmed(&in.PortPrefix, 20)
	var errs []plugin.FieldError
	add := func(f, m string) { errs = append(errs, plugin.FieldError{Field: f, Message: m}) }
	switch in.Kind {
	case KindDevice:
		if in.DeviceID <= 0 {
			add("deviceId", "Gerät fehlt")
		}
	case KindPatchPanel, KindShelf, KindBlank, KindCableManager, KindPDU, KindOther:
		in.DeviceID = 0
	default:
		add("kind", "Unbekannte Art")
	}
	if in.Height == 0 {
		in.Height = 1
	}
	if in.Height < 1 || in.Height > MaxHeight {
		add("height", fmt.Sprintf("1 bis %d Höheneinheiten erwartet", MaxHeight))
	}
	if in.Position < 1 {
		add("position", "Höheneinheit ab 1 erwartet")
	}
	if in.Face == "" {
		in.Face = "front"
	}
	if in.Face != "front" && in.Face != "rear" {
		add("face", "front oder rear erwartet")
	}
	if in.Cols == 0 {
		in.Cols = Columns
	}
	switch {
	case in.Cols != 6 && in.Cols != 3 && in.Cols != 2:
		add("cols", "Volle, halbe oder drittel Breite erwartet")
	case in.Col < 0 || in.Col+in.Cols > Columns || in.Col%in.Cols != 0:
		add("col", "Ungültige Spalte für diese Breite")
	}
	if in.PortCount < 0 || in.PortCount > MaxPorts {
		add("portCount", fmt.Sprintf("0 bis %d Ports erwartet", MaxPorts))
	}
	if len(errs) > 0 {
		return &plugin.ValidationError{Errors: errs}
	}
	return nil
}

func overlaps(a1, a2, b1, b2 int) bool { return a1 < b2 && b1 < a2 }

// checkPlace verifies that an item fits into the rack without colliding (self = the item
// being moved, 0 for a new one).
func checkPlace(ctx context.Context, tx *sql.Tx, rackID, self int64, in ItemInput) error {
	var height int
	if err := tx.QueryRowContext(ctx, "SELECT height FROM racks WHERE id = ?", rackID).Scan(&height); err != nil {
		return db.NotFound(err)
	}
	if in.Position+in.Height-1 > height {
		return plugin.FieldErr("position", fmt.Sprintf("Das Rack hat nur %d Höheneinheiten", height))
	}
	rows, err := tx.QueryContext(ctx, `SELECT i.id, i.kind, i.label, i.device_name, IFNULL(d.display_name, ''), IFNULL(d.hostname, ''),
		i.position, i.height, i.face, i.full_depth, i.col, i.cols FROM rack_items i LEFT JOIN devices d ON d.id = i.device_id
		WHERE i.rack_id = ? AND i.id <> ?`, rackID, self)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id                             int64
			kind, label, dname, disp, host string
			pos, h, col, cols              int
			face                           string
			full                           bool
		)
		if err := rows.Scan(&id, &kind, &label, &dname, &disp, &host, &pos, &h, &face, &full, &col, &cols); err != nil {
			return err
		}
		sameSide := face == in.Face || full || in.FullDepth
		if sameSide && overlaps(pos, pos+h, in.Position, in.Position+in.Height) && overlaps(col, col+cols, in.Col, in.Col+in.Cols) {
			name := firstNonEmpty(label, disp, host, dname, kindName(kind))
			return plugin.FieldErr("position", fmt.Sprintf("Der Platz ist belegt (%s)", name))
		}
	}
	return rows.Err()
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func kindName(kind string) string {
	switch kind {
	case KindPatchPanel:
		return "Patchfeld"
	case KindShelf:
		return "Fachboden"
	case KindBlank:
		return "Blende"
	case KindCableManager:
		return "Kabelführung"
	case KindPDU:
		return "Steckdosenleiste"
	case KindDevice:
		return "Gerät"
	}
	return "Element"
}

// deviceName returns the display name of a device (and whether it exists).
func deviceName(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id int64) (string, bool, error) {
	var disp, host, ip, mac string
	err := q.QueryRowContext(ctx, "SELECT display_name, hostname, primary_ip, primary_mac FROM devices WHERE id = ?", id).Scan(&disp, &host, &ip, &mac)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return firstNonEmpty(disp, host, ip, mac, fmt.Sprintf("Gerät %d", id)), true, nil
}

func (s *Service) checkDevice(ctx context.Context, tx *sql.Tx, self, device int64) (string, error) {
	name, ok, err := deviceName(ctx, tx, device)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", plugin.FieldErr("deviceId", "Gerät existiert nicht")
	}
	var rackName string
	err = tx.QueryRowContext(ctx, "SELECT r.name FROM rack_items i JOIN racks r ON r.id = i.rack_id WHERE i.device_id = ? AND i.id <> ?", device, self).Scan(&rackName)
	switch {
	case err == nil:
		return "", plugin.FieldErr("deviceId", fmt.Sprintf("Das Gerät ist bereits in Rack %s eingebaut", rackName))
	case !errors.Is(err, sql.ErrNoRows):
		return "", err
	}
	return name, nil
}

func nullID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}

// AddItem mounts an item in a rack.
func (s *Service) AddItem(ctx context.Context, rackID int64, in ItemInput) (int64, error) {
	if err := in.normalize(); err != nil {
		return 0, err
	}
	var id int64
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		if err := checkPlace(ctx, tx, rackID, 0, in); err != nil {
			return err
		}
		var name string
		if in.DeviceID > 0 {
			n, err := s.checkDevice(ctx, tx, 0, in.DeviceID)
			if err != nil {
				return err
			}
			name = n
		}
		now := db.Now()
		res, err := tx.ExecContext(ctx, `INSERT INTO rack_items(rack_id, kind, device_id, device_name, label, position, height, face, full_depth,
			col, cols, port_count, port_prefix, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			rackID, in.Kind, nullID(in.DeviceID), name, in.Label, in.Position, in.Height, in.Face, db.Bool(in.FullDepth),
			in.Col, in.Cols, in.PortCount, in.PortPrefix, now, now)
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	if err != nil {
		return 0, err
	}
	return id, s.Sync(ctx)
}

// ItemRef identifies an item for messages.
type ItemRef struct {
	ID     int64  `json:"id"`
	RackID int64  `json:"rackId"`
	Name   string `json:"name"`
}

// itemRef returns rack and name of an item.
func (s *Service) itemRef(ctx context.Context, id int64) (*ItemRef, error) {
	var (
		ref                                ItemRef
		kind, label, dname, disp, host, ip string
	)
	err := s.db.R.QueryRowContext(ctx, `SELECT i.id, i.rack_id, i.kind, i.label, i.device_name, IFNULL(d.display_name, ''), IFNULL(d.hostname, ''),
		IFNULL(d.primary_ip, '') FROM rack_items i LEFT JOIN devices d ON d.id = i.device_id WHERE i.id = ?`, id).
		Scan(&ref.ID, &ref.RackID, &kind, &label, &dname, &disp, &host, &ip)
	if err != nil {
		return nil, db.NotFound(err)
	}
	ref.Name = firstNonEmpty(label, disp, host, ip, dname, kindName(kind))
	return &ref, nil
}

// Item returns the reference of an item (rack and name).
func (s *Service) Item(ctx context.Context, id int64) (*ItemRef, error) { return s.itemRef(ctx, id) }

// UpdateItem changes or moves an item. Without RackID it stays in its rack.
func (s *Service) UpdateItem(ctx context.Context, id int64, in ItemInput) error {
	if err := in.normalize(); err != nil {
		return err
	}
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var rackID int64
		if err := tx.QueryRowContext(ctx, "SELECT rack_id FROM rack_items WHERE id = ?", id).Scan(&rackID); err != nil {
			return db.NotFound(err)
		}
		if in.RackID > 0 {
			rackID = in.RackID
		}
		if err := checkPlace(ctx, tx, rackID, id, in); err != nil {
			return err
		}
		name := ""
		if in.DeviceID > 0 {
			n, err := s.checkDevice(ctx, tx, id, in.DeviceID)
			if err != nil {
				return err
			}
			name = n
		}
		_, err := tx.ExecContext(ctx, `UPDATE rack_items SET rack_id = ?, kind = ?, device_id = ?, device_name = ?, label = ?, position = ?, height = ?,
			face = ?, full_depth = ?, col = ?, cols = ?, port_count = ?, port_prefix = ?, updated_at = ? WHERE id = ?`,
			rackID, in.Kind, nullID(in.DeviceID), name, in.Label, in.Position, in.Height, in.Face, db.Bool(in.FullDepth),
			in.Col, in.Cols, in.PortCount, in.PortPrefix, db.Now(), id)
		return err
	})
	if err != nil {
		return err
	}
	return s.Sync(ctx)
}

// DeleteItem removes an item with its port data and cables.
func (s *Service) DeleteItem(ctx context.Context, id int64) (*ItemRef, error) {
	ref, err := s.itemRef(ctx, id)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.W.ExecContext(ctx, "DELETE FROM rack_items WHERE id = ?", id); err != nil {
		return nil, err
	}
	return ref, s.Sync(ctx)
}

// ---------------------------------------------------------------- ports and cables

// PortInput sets the data of a port.
type PortInput struct {
	Port  string `json:"port"`
	Label string `json:"label,omitempty"`
	// DeviceID is the device plugged in directly (0 = none).
	DeviceID int64 `json:"deviceId,omitempty"`
}

// SetPort sets label and device of a port.
func (s *Service) SetPort(ctx context.Context, itemID int64, in PortInput) error {
	trimmed(&in.Label, 100)
	st, err := s.load(ctx)
	if err != nil {
		return err
	}
	it := st.byID[itemID]
	if it == nil {
		return db.ErrNotFound
	}
	port, ok := st.canonical(it, in.Port)
	if !ok {
		return plugin.FieldErr("port", "Unbekannter Port")
	}
	if in.DeviceID > 0 {
		if _, ok, err := deviceName(ctx, s.db.R, in.DeviceID); err != nil {
			return err
		} else if !ok {
			return plugin.FieldErr("deviceId", "Gerät existiert nicht")
		}
		if in.DeviceID == it.DeviceID {
			return plugin.FieldErr("deviceId", "Ein Gerät kann nicht an einem eigenen Port stecken")
		}
		if n := len(st.cablesAt(itemID, port)); n > 0 && (it.Kind == KindDevice || n > 1) {
			return plugin.FieldErr("deviceId", "An diesem Port steckt ein Patchkabel – erst das Kabel entfernen")
		}
	}
	if in.Label == "" && in.DeviceID <= 0 {
		_, err = s.db.W.ExecContext(ctx, "DELETE FROM rack_ports WHERE item_id = ? AND port = ?", itemID, port)
	} else {
		_, err = s.db.W.ExecContext(ctx, `INSERT INTO rack_ports(item_id, port, label, device_id) VALUES (?,?,?,?)
			ON CONFLICT(item_id, port) DO UPDATE SET label = excluded.label, device_id = excluded.device_id`, itemID, port, in.Label, nullID(in.DeviceID))
	}
	if err != nil {
		return err
	}
	return s.Sync(ctx)
}

// End is one end of a cable.
type End struct {
	ItemID int64  `json:"itemId"`
	Port   string `json:"port"`
}

// CableInput creates a patch cable.
type CableInput struct {
	A     End    `json:"a"`
	B     End    `json:"b"`
	Label string `json:"label,omitempty"`
	Color string `json:"color,omitempty"`
}

// Cable is a patch cable.
type Cable struct {
	ID    int64  `json:"id"`
	A     End    `json:"a"`
	B     End    `json:"b"`
	Label string `json:"label"`
	Color string `json:"color"`
}

// Colors are the cable colors offered.
var Colors = []string{"", "blue", "green", "yellow", "red", "orange", "white", "grey", "black", "purple", "pink"}

// AddCable connects two ports. A port of a device takes one cable, a port of a patch panel
// two (front and back) – or one when a device is plugged into it.
func (s *Service) AddCable(ctx context.Context, in CableInput) (int64, error) {
	trimmed(&in.Label, 100)
	valid := false
	for _, c := range Colors {
		valid = valid || c == in.Color
	}
	if !valid {
		return 0, plugin.FieldErr("color", "Unbekannte Farbe")
	}
	st, err := s.load(ctx)
	if err != nil {
		return 0, err
	}
	ends := [2]*End{&in.A, &in.B}
	for i, e := range ends {
		field := []string{"a", "b"}[i]
		it := st.byID[e.ItemID]
		if it == nil {
			return 0, plugin.FieldErr(field, "Element existiert nicht")
		}
		port, ok := st.canonical(it, e.Port)
		if !ok {
			return 0, plugin.FieldErr(field, fmt.Sprintf("Unbekannter Port %s", e.Port))
		}
		e.Port = port
		limit := 1
		if it.Kind != KindDevice {
			limit = 2
			if pd := st.ports[portKey{it.ID, port}]; pd != nil && pd.device > 0 {
				limit = 1
			}
		} else if pd := st.ports[portKey{it.ID, port}]; pd != nil && pd.device > 0 {
			return 0, plugin.FieldErr(field, fmt.Sprintf("An Port %s steckt direkt ein Gerät – erst die Zuordnung lösen", port))
		}
		if len(st.cablesAt(it.ID, port)) >= limit {
			return 0, plugin.FieldErr(field, fmt.Sprintf("Port %s ist bereits belegt", port))
		}
	}
	if in.A == in.B {
		return 0, plugin.FieldErr("b", "Ein Kabel braucht zwei verschiedene Ports")
	}
	res, err := s.db.W.ExecContext(ctx, `INSERT INTO rack_cables(a_item, a_port, b_item, b_port, label, color, created_at) VALUES (?,?,?,?,?,?,?)`,
		in.A.ItemID, in.A.Port, in.B.ItemID, in.B.Port, in.Label, in.Color, db.Now())
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, s.Sync(ctx)
}

// DeleteCable removes a patch cable.
func (s *Service) DeleteCable(ctx context.Context, id int64) (*Cable, error) {
	var c Cable
	err := s.db.R.QueryRowContext(ctx, "SELECT id, a_item, a_port, b_item, b_port, label, color FROM rack_cables WHERE id = ?", id).
		Scan(&c.ID, &c.A.ItemID, &c.A.Port, &c.B.ItemID, &c.B.Port, &c.Label, &c.Color)
	if err != nil {
		return nil, db.NotFound(err)
	}
	if _, err := s.db.W.ExecContext(ctx, "DELETE FROM rack_cables WHERE id = ?", id); err != nil {
		return nil, err
	}
	return &c, s.Sync(ctx)
}

// Adopt takes over the connections detected on the ports of an item: every free port with
// exactly one detected device gets that device. Returns the number of ports set.
func (s *Service) Adopt(ctx context.Context, itemID int64) (int, error) {
	st, err := s.load(ctx)
	if err != nil {
		return 0, err
	}
	it := st.byID[itemID]
	if it == nil {
		return 0, db.ErrNotFound
	}
	if it.Kind != KindDevice || it.DeviceID == 0 {
		return 0, errors.New("Nur Geräte haben erkannte Verbindungen")
	}
	n := 0
	for _, p := range st.item(it).Ports {
		if p.Device != nil || len(p.Cables) > 0 || len(p.Detected) != 1 || p.DetectedMore > 0 {
			continue
		}
		if _, err := s.db.W.ExecContext(ctx, `INSERT INTO rack_ports(item_id, port, label, device_id) VALUES (?,?,'',?)
			ON CONFLICT(item_id, port) DO UPDATE SET device_id = excluded.device_id`, itemID, p.Name, p.Detected[0].Device.ID); err != nil {
			return n, err
		}
		n++
	}
	if n == 0 {
		return 0, nil
	}
	return n, s.Sync(ctx)
}

// Mount is where a device is mounted.
type Mount struct {
	RackID   int64  `json:"rackId"`
	RackName string `json:"rackName"`
	ItemID   int64  `json:"itemId"`
	Position int    `json:"position"`
	Height   int    `json:"height"`
	Face     string `json:"face"`
	// Unit is the height unit as labelled in the rack (numbering from the top or bottom).
	Unit int `json:"unit"`
}

// Link is a port a device is connected to in a rack.
type Link struct {
	RackID   int64  `json:"rackId"`
	RackName string `json:"rackName"`
	ItemID   int64  `json:"itemId"`
	ItemName string `json:"itemName"`
	Port     string `json:"port"`
}

// DeviceInfo tells where a device is in the racks.
type DeviceInfo struct {
	Mount *Mount `json:"mount"`
	Links []Link `json:"links"`
}

// Device returns where a device is mounted and which rack ports lead to it.
func (s *Service) Device(ctx context.Context, id int64) (*DeviceInfo, error) {
	st, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	out := &DeviceInfo{Links: []Link{}}
	for _, it := range st.items {
		r := st.racks[it.RackID]
		if it.DeviceID == id && r != nil {
			unit := it.Position
			if r.Numbering == "top" {
				unit = r.Height - (it.Position + it.Height - 1) + 1
			}
			out.Mount = &Mount{RackID: r.ID, RackName: r.Name, ItemID: it.ID, Position: it.Position, Height: it.Height, Face: it.Face, Unit: unit}
		}
	}
	for _, it := range st.items {
		if it.Kind != KindDevice || it.DeviceID == 0 {
			continue
		}
		r := st.racks[it.RackID]
		for _, p := range st.portNames(it) {
			if d := st.endpoint(it.ID, p.Name); d != nil && d.ID == id && r != nil {
				out.Links = append(out.Links, Link{RackID: r.ID, RackName: r.Name, ItemID: it.ID, ItemName: st.itemName(it), Port: p.Name})
			}
		}
	}
	return out, nil
}

// publish announces changed devices on the bus.
func (s *Service) publish(ids map[int64]bool) {
	if s.bus == nil {
		return
	}
	for id := range ids {
		s.bus.Publish(bus.TopicDevice, "updated", map[string]any{"id": id})
	}
}

package api

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"netscope/internal/inventory"
	"netscope/internal/plugin"
	"netscope/internal/rack"
)

func TestRackWorkflow(t *testing.T) {
	h := newHarness(t)
	h.login(t)
	ctx := context.Background()
	sw, err := h.inv.Observe(ctx, "arpscan", 0, &plugin.Observation{MACs: []string{"aa:bb:cc:dd:ee:10"}, IP: "192.168.1.2", Present: true})
	if err != nil {
		t.Fatal(err)
	}
	pc, err := h.inv.Observe(ctx, "arpscan", 0, &plugin.Observation{MACs: []string{"aa:bb:cc:dd:ee:11"}, IP: "192.168.1.30", Present: true})
	if err != nil {
		t.Fatal(err)
	}
	h.do(t, "PATCH", "/api/v1/devices/"+strconv.FormatInt(sw, 10), map[string]any{"displayName": "core-sw", "type": "switch"}, csrf, "1")

	resp, body := h.do(t, "POST", "/api/v1/racks", map[string]any{"name": "", "height": 99}, csrf, "1")
	if resp.StatusCode != 400 || !strings.Contains(string(body), `"field":"height"`) {
		t.Fatalf("invalid rack: %d %s", resp.StatusCode, body)
	}
	resp, body = h.do(t, "POST", "/api/v1/racks", map[string]any{"name": "Keller", "height": 12}, csrf, "1")
	if resp.StatusCode != 201 {
		t.Fatalf("create rack: %d %s", resp.StatusCode, body)
	}
	var rk rack.Rack
	_ = json.Unmarshal(body, &rk)
	rid := strconv.FormatInt(rk.ID, 10)

	add := func(in map[string]any) int64 {
		t.Helper()
		resp, body := h.do(t, "POST", "/api/v1/racks/"+rid+"/items", in, csrf, "1")
		if resp.StatusCode != 201 {
			t.Fatalf("add item: %d %s", resp.StatusCode, body)
		}
		var id idResponse
		_ = json.Unmarshal(body, &id)
		return id.ID
	}
	swItem := add(map[string]any{"kind": "device", "deviceId": sw, "position": 12, "portCount": 8})
	panel := add(map[string]any{"kind": "patch_panel", "label": "PP", "position": 11, "portCount": 24})
	resp, body = h.do(t, "POST", "/api/v1/racks/"+rid+"/items", map[string]any{"kind": "blank", "position": 12}, csrf, "1", "Accept-Language", "en")
	if resp.StatusCode != 400 || !strings.Contains(string(body), "The space is taken (core-sw)") {
		t.Fatalf("occupied: %d %s", resp.StatusCode, body)
	}

	resp, body = h.do(t, "POST", "/api/v1/rack-cables", map[string]any{"a": map[string]any{"itemId": swItem, "port": "4"},
		"b": map[string]any{"itemId": panel, "port": "4"}, "color": "blue"}, csrf, "1")
	if resp.StatusCode != 201 {
		t.Fatalf("cable: %d %s", resp.StatusCode, body)
	}
	resp, body = h.do(t, "PUT", "/api/v1/rack-items/"+strconv.FormatInt(panel, 10)+"/port", map[string]any{"port": "4", "label": "Dose 4", "deviceId": pc}, csrf, "1")
	if resp.StatusCode != 200 {
		t.Fatalf("port: %d %s", resp.StatusCode, body)
	}

	// the rack shows the way; the topology the protected connection
	_, body = h.do(t, "GET", "/api/v1/racks/"+rid, nil)
	var v rack.View
	_ = json.Unmarshal(body, &v)
	if len(v.Items) != 2 {
		t.Fatalf("view: %s", body)
	}
	_, body = h.do(t, "GET", "/api/v1/topology", nil)
	var g inventory.Graph
	_ = json.Unmarshal(body, &g)
	var edge *inventory.GraphEdge
	for i, e := range g.Edges {
		if e.Origin == rack.Source {
			edge = &g.Edges[i]
		}
	}
	if edge == nil || edge.Source != "d"+strconv.FormatInt(sw, 10) || edge.Target != "d"+strconv.FormatInt(pc, 10) || edge.ParentPort != "4" || !edge.Protected {
		t.Fatalf("topology edge: %+v", g.Edges)
	}
	resp, body = h.do(t, "DELETE", "/api/v1/topology/edges/"+strconv.FormatInt(edge.RelationID, 10), nil, csrf, "1")
	if resp.StatusCode != 400 || !strings.Contains(string(body), "Rack") {
		t.Fatalf("delete rack edge in topology: %d %s", resp.StatusCode, body)
	}

	_, body = h.do(t, "GET", "/api/v1/devices/"+strconv.FormatInt(pc, 10)+"/rack", nil)
	var info rack.DeviceInfo
	_ = json.Unmarshal(body, &info)
	if info.Mount != nil || len(info.Links) != 1 || info.Links[0].ItemName != "core-sw" {
		t.Fatalf("device rack: %s", body)
	}

	_, body = h.do(t, "GET", "/api/v1/racks", nil)
	var list []rack.Summary
	_ = json.Unmarshal(body, &list)
	if len(list) != 1 || list[0].Items != 2 || list[0].Devices != 1 || list[0].UsedUnits != 2 {
		t.Fatalf("list: %s", body)
	}
	_, body = h.do(t, "GET", "/api/v1/audit?entity=rack", nil)
	for _, want := range []string{"rack.create", "rack.item_create", "rack.cable_create", "rack.port_update", "Dose"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("audit %s missing: %s", want, body)
		}
	}

	// deleting the rack removes the connection
	if resp, body := h.do(t, "DELETE", "/api/v1/racks/"+rid, nil, csrf, "1"); resp.StatusCode != 200 {
		t.Fatalf("delete: %d %s", resp.StatusCode, body)
	}
	rels, _ := h.inv.DeviceRelations(ctx, pc)
	if len(rels) != 0 {
		t.Fatalf("relations after deleting the rack: %+v", rels)
	}
}

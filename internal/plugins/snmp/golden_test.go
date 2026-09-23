package snmp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The topology processor tests read the Inventory JSON of real switch recordings from
// ../topology/testdata. This test keeps those files identical to the decoder output;
// NETSCOPE_UPDATE_GOLDEN=1 rewrites them.
func TestTopologyGoldenInventories(t *testing.T) {
	for rec, golden := range map[string]string{
		"librenms-procurve.snmprec":        "procurve.inventory.json",
		"librenms-routeros-crs317.snmprec": "routeros-crs317.inventory.json",
	} {
		inv := DecodeInventory(loadSnmprec(t, rec))
		inv.Walked = []string{TableSystem, TableInterfaces, TableARP, TableFDB, TableLLDP}
		want, err := json.MarshalIndent(inv, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, '\n')
		path := filepath.Join("..", "topology", "testdata", golden)
		if os.Getenv("NETSCOPE_UPDATE_GOLDEN") == "1" {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v (run with NETSCOPE_UPDATE_GOLDEN=1)", path, err)
		}
		if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), want) {
			t.Errorf("%s is outdated – run with NETSCOPE_UPDATE_GOLDEN=1", path)
		}
	}
}

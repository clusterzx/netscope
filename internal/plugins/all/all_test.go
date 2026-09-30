package all

import (
	"testing"

	"netscope/internal/plugin"
)

// Every system the setup wizard offers in its sources step (a plugin with a category)
// has a connection test, and every scanner of the scanner step names its load.
func TestCategoriesAndLoads(t *testing.T) {
	categories := map[string]int{}
	scanners := 0
	for _, p := range plugin.All() {
		info := p.Info()
		if info.Category != "" {
			categories[info.Category]++
			if _, ok := p.(plugin.ConnectionTester); !ok {
				t.Errorf("%s (category %s) has no connection test", info.ID, info.Category)
			}
		}
		if info.Load != "" {
			scanners++
			if info.Kind != plugin.KindScanner {
				t.Errorf("%s: only scanners have a load", info.ID)
			}
			if info.Category != "" {
				t.Errorf("%s: a scanner of the scanner step must not be a source too", info.ID)
			}
		}
		// importers that read other systems with credentials belong to a tab
		if info.Kind == plugin.KindImporter && info.Category == "" {
			for _, f := range p.Schema().Fields {
				if f.Type == plugin.FieldCredentialRef {
					t.Errorf("%s reads with credentials but has no category", info.ID)
				}
			}
		}
	}
	for _, c := range plugin.Categories {
		if categories[c.ID] == 0 {
			t.Errorf("category %s has no plugin", c.ID)
		}
	}
	if scanners != 10 {
		t.Errorf("%d scanners with a load, want 10", scanners)
	}
}

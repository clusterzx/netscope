package proxmox

import "testing"

func TestNormalizeSecret(t *testing.T) {
	const id, uuid = "netscope@pve!netscope", "0f3a3c3e-1111-2222-3333-444455556666"
	for _, in := range []string{
		uuid,
		" " + uuid + "\n",
		`"` + uuid + `"`,
		id + "=" + uuid,
		"PVEAPIToken=" + id + "=" + uuid,
	} {
		if got := normalizeSecret(id, in); got != uuid {
			t.Errorf("normalizeSecret(%q) = %q", in, got)
		}
	}
	if got := normalizeSecret(id, "  "); got != "" {
		t.Errorf("blank secret: %q", got)
	}
}

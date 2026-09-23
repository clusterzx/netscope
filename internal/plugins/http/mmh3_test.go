package http

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMurmur3Vectors(t *testing.T) {
	// Reference values computed with Python mmh3 (mmh3.hash, signed 32-bit, seed 0).
	cases := []struct {
		in   string
		want int32
	}{
		{"", 0},
		{"hello", 613153351},
		{"The quick brown fox jumps over the lazy dog", 776992547},
	}
	for _, c := range cases {
		if got := int32(murmur3x86_32([]byte(c.in), 0)); got != c.want {
			t.Errorf("murmur3(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestFaviconHashReference(t *testing.T) {
	// Reference values computed with Python: mmh3.hash(base64.encodebytes(icon)) and md5.
	cases := []struct {
		file    string
		wantH   int32
		wantMD5 string
	}{
		{"favicon-luci.png", -918516514, "e981412241f097482d8efbae5a31516e"},
		{"favicon-adguard.svg", 1518287672, "08ea9a5ba1aab87d89c2fee9fbd5181d"},
		{"favicon-adguard.png", -635881733, "1b786be7a46bd96a503a81b7faf86263"},
	}
	for _, c := range cases {
		icon, err := os.ReadFile(filepath.Join("testdata", c.file))
		if err != nil {
			t.Fatalf("read %s: %v", c.file, err)
		}
		h, md5hex := faviconHash(icon)
		if h != c.wantH {
			t.Errorf("%s hash = %d, want %d", c.file, h, c.wantH)
		}
		if md5hex != c.wantMD5 {
			t.Errorf("%s md5 = %s, want %s", c.file, md5hex, c.wantMD5)
		}
	}
}

func TestFaviconHashEmpty(t *testing.T) {
	h, _ := faviconHash(nil)
	if h != 0 {
		t.Errorf("empty favicon hash = %d, want 0", h)
	}
}

func TestPyBase64Wrapping(t *testing.T) {
	// 60 bytes → 80 base64 chars → wrapped as 76 + "\n" + 4 + "\n".
	data := make([]byte, 60)
	got := pyBase64(data)
	if len(got) != 82 || got[76] != '\n' || got[len(got)-1] != '\n' {
		t.Errorf("pyBase64 wrapping wrong: len=%d", len(got))
	}
}

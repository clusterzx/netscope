package cve

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realMeta is the .meta file of the 2024 feed as published by the NVD.
const realMeta = `lastModifiedDate:2026-09-22T03:01:27-04:00
size:291865315
zipSize:24992675
gzSize:24992539
sha256:E253D61A65698F3D0DF062D38682752911385D4F4530C61CD6564BB4C92D7F2E
`

func TestParseMeta(t *testing.T) {
	m, err := parseMeta(strings.NewReader(strings.ReplaceAll(realMeta, "\n", "\r\n")))
	if err != nil {
		t.Fatal(err)
	}
	if m.LastModified != "2026-09-22T03:01:27-04:00" || m.Size != 291865315 || m.GzSize != 24992539 ||
		m.SHA256 != "e253d61a65698f3d0df062d38682752911385d4f4530c61cd6564bb4c92d7f2e" {
		t.Fatalf("meta %+v", m)
	}
	if _, err := parseMeta(strings.NewReader("<html>not found</html>")); err == nil {
		t.Fatal("expected error for a page without checksum")
	}
}

func parseFixture(t *testing.T, name string) map[string]*cveRecord {
	t.Helper()
	rd, err := openFeed(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer rd.Close()
	out := map[string]*cveRecord{}
	total := -1
	n, err := parseFeed(context.Background(), rd, func(v int) { total = v }, func(r *cveRecord) error {
		out[r.ID] = r
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != len(out) || total != n {
		t.Fatalf("%s: parsed %d, totalResults %d, unique %d", name, n, total, len(out))
	}
	return out
}

func TestParseFeedFixture(t *testing.T) {
	recs := parseFixture(t, "nvdcve-2.0-2024.json.gz")
	if len(recs) != 16 {
		t.Fatalf("got %d records", len(recs))
	}
	r := recs["CVE-2024-6387"]
	if r == nil || r.Score == nil || *r.Score != 8.1 || r.CVSSVersion != "3.1" || r.Severity != "high" ||
		r.Vector != "CVSS:3.1/AV:N/AC:H/PR:N/UI:N/S:U/C:H/I:H/A:H" {
		t.Fatalf("CVE-2024-6387 metrics: %+v", r)
	}
	if !strings.HasPrefix(r.Description, "A security regression (CVE-2006-5051) was discovered in OpenSSH") {
		t.Errorf("description %q", r.Description)
	}
	if len(r.Refs) != maxRefs || r.Refs[0].URL == "" {
		t.Errorf("refs: %d", len(r.Refs))
	}
	if strings.Join(r.CWEs, ",") != "CWE-364,CWE-362" {
		t.Errorf("cwes %v", r.CWEs)
	}
	if r.Published != 1719839706467 { // 2024-07-01T13:15:06.467Z
		t.Errorf("published %d", r.Published)
	}
	// the OpenSSH criteria as published by the NVD
	var ssh []string
	for _, m := range r.Matches {
		if m.Product == "openssh" {
			ssh = append(ssh, m.Version+":"+m.Update+" ["+m.StartIncl+","+m.StartExcl+","+m.EndIncl+","+m.EndExcl+"]")
		}
	}
	want := "*:* [,,,4.4]|*:* [8.6,,9.8,]|4.4:- [,,,]|8.5:p1 [,,,]|8.6:- [,,,]"
	if strings.Join(ssh, "|") != want {
		t.Errorf("openssh criteria:\n got %s\nwant %s", strings.Join(ssh, "|"), want)
	}
	for _, m := range r.Matches {
		if m.Vendor == "sonicwall" && strings.HasPrefix(m.Product, "sma_6200") && m.Part == "h" {
			t.Error("non-vulnerable hardware entry stored")
		}
	}
	// NVD (Primary) score wins over the CNA (Secondary) score of the same version
	if g := recs["CVE-2024-9264"]; g.Score == nil || *g.Score != 8.8 || g.CVSSVersion != "3.1" {
		t.Errorf("CVE-2024-9264 score %+v", g)
	}
	// CVSS v4.0 only
	if v4 := recs["CVE-2024-4622"]; v4.Score == nil || *v4.Score != 8.3 || v4.CVSSVersion != "4.0" || v4.Severity != "high" {
		t.Errorf("CVE-2024-4622 %+v", v4)
	}
	if !recs["CVE-2024-0069"].Rejected {
		t.Error("CVE-2024-0069 must be rejected")
	}
	if n := len(recs["CVE-2024-5535"].Matches); n != 0 {
		t.Errorf("deferred CVE has %d criteria", n)
	}
	var escaped bool
	for _, m := range recs["CVE-2024-35779"].Matches {
		if m.Product == "page_builder:_live_composer" && m.EndIncl == "1.5.42" {
			escaped = true
		}
	}
	if !escaped {
		t.Errorf("escaped criteria %+v", recs["CVE-2024-35779"].Matches)
	}
	old := parseFixture(t, "nvdcve-2.0-2021.json.gz")
	// v3.1 preferred over v2
	if n := old["CVE-2021-23017"]; n.Score == nil || *n.Score != 7.7 || n.CVSSVersion != "3.1" {
		t.Errorf("CVE-2021-23017 %+v", n)
	}
}

func TestUnescapeBounds(t *testing.T) {
	if got := unescapeFS(`11.6\(1\)`); got != "11.6(1)" {
		t.Errorf("got %q", got)
	}
	if got := unescapeFS("4.32.1F"); got != "4.32.1f" {
		t.Errorf("got %q", got)
	}
}

func TestParseFeedStructure(t *testing.T) {
	// key order is not fixed; unknown nested values are skipped token by token
	doc := `{"vulnerabilities":[{"cve":{"id":"CVE-2099-0001","vulnStatus":"Analyzed","lastModified":"2099-01-01T00:00:00.000",
		"metrics":{"cvssMetricV2":[{"type":"Primary","baseSeverity":"HIGH","cvssData":{"version":"2.0","vectorString":"AV:N/AC:L/Au:N/C:C/I:C/A:C","baseScore":10.0}}]},
		"configurations":[{"nodes":[{"operator":"OR","cpeMatch":[{"vulnerable":true,"criteria":"cpe:2.3:a:acme:widget:1.0:*:*:*:*:*:*:*"},
		{"vulnerable":true,"criteria":"cpe:2.3:a:acme:widget:1.0:*:*:*:*:*:*:*"},{"vulnerable":false,"criteria":"cpe:2.3:o:acme:os:-:*:*:*:*:*:*:*"}]}]}]}},
		{"cve":{"id":"not-a-cve"}}],
		"extra":{"nested":[1,{"a":[true,null]}]},"totalResults":2,"format":"NVD_CVE"}`
	var got []*cveRecord
	total := 0
	n, err := parseFeed(context.Background(), strings.NewReader(doc), func(v int) { total = v }, func(r *cveRecord) error {
		got = append(got, r)
		return nil
	})
	if err != nil || n != 1 || total != 2 {
		t.Fatalf("n=%d total=%d err=%v", n, total, err)
	}
	r := got[0]
	if r.Score == nil || *r.Score != 10 || r.CVSSVersion != "2.0" || r.Severity != "high" || len(r.Matches) != 1 {
		t.Fatalf("record %+v", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := parseFeed(ctx, strings.NewReader(doc), nil, func(*cveRecord) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled parse: %v", err)
	}
	if _, err := parseFeed(context.Background(), strings.NewReader(`{"vulnerabilities":[{"cve":`), nil, func(*cveRecord) error { return nil }); err == nil {
		t.Fatal("truncated feed must fail")
	}
}

func TestVerifyFeed(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`{"vulnerabilities":[]}`)
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(content)
	zw.Close()
	path := filepath.Join(dir, "f.json.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	if err := verifyFeed(context.Background(), path, hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	var ce *checksumError
	if err := verifyFeed(context.Background(), path, strings.Repeat("0", 64)); !errors.As(err, &ce) {
		t.Fatalf("mismatch: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes()[:buf.Len()-6], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyFeed(context.Background(), path, hex.EncodeToString(sum[:])); !errors.As(err, &ce) {
		t.Fatalf("truncated: %v", err)
	}
}

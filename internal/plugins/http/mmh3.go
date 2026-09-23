package http

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
)

// murmur3x86_32 computes the 32-bit x86 MurmurHash3 of data with the given seed. This is
// the hash Shodan uses for favicons.
func murmur3x86_32(data []byte, seed uint32) uint32 {
	const (
		c1 = 0xcc9e2d51
		c2 = 0x1b873593
	)
	h := seed
	nblocks := len(data) / 4
	for i := 0; i < nblocks; i++ {
		k := binary.LittleEndian.Uint32(data[i*4:])
		k *= c1
		k = (k << 15) | (k >> 17)
		k *= c2
		h ^= k
		h = (h << 13) | (h >> 19)
		h = h*5 + 0xe6546b64
	}
	var k uint32
	tail := data[nblocks*4:]
	switch len(tail) & 3 {
	case 3:
		k ^= uint32(tail[2]) << 16
		fallthrough
	case 2:
		k ^= uint32(tail[1]) << 8
		fallthrough
	case 1:
		k ^= uint32(tail[0])
		k *= c1
		k = (k << 15) | (k >> 17)
		k *= c2
		h ^= k
	}
	h ^= uint32(len(data))
	h ^= h >> 16
	h *= 0x85ebca6b
	h ^= h >> 13
	h *= 0xc2b2ae35
	h ^= h >> 16
	return h
}

// pyBase64 replicates Python's base64.encodebytes: standard base64 with a newline after
// every 76 output characters and a trailing newline.
func pyBase64(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	enc := base64.StdEncoding.EncodeToString(data)
	out := make([]byte, 0, len(enc)+len(enc)/76+1)
	for i := 0; i < len(enc); i += 76 {
		end := i + 76
		if end > len(enc) {
			end = len(enc)
		}
		out = append(out, enc[i:end]...)
		out = append(out, '\n')
	}
	return out
}

// faviconHash returns the Shodan-compatible favicon hash (mmh3 of the Python-base64 of the
// icon, as a signed int32) and the md5 hex of the raw icon.
func faviconHash(icon []byte) (int32, string) {
	h := int32(murmur3x86_32(pyBase64(icon), 0))
	sum := md5.Sum(icon)
	return h, hex.EncodeToString(sum[:])
}

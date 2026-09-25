package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
)

// softKey is a software authenticator (ES256, attestation "none").
type softKey struct {
	t       *testing.T
	rp      RP
	key     *ecdsa.PrivateKey
	id      []byte
	counter uint32
}

var b64u = base64.RawURLEncoding

func newSoftKey(t *testing.T, rp RP) *softKey {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	return &softKey{t: t, rp: rp, key: key, id: id}
}

func (k *softKey) clientData(typ string, challenge []byte) []byte {
	b, _ := json.Marshal(map[string]string{"type": typ, "challenge": b64u.EncodeToString(challenge), "origin": k.rp.Origin})
	return b
}

func (k *softKey) authData(flags byte, attested []byte) []byte {
	h := sha256.Sum256([]byte(k.rp.ID))
	out := append(h[:], flags)
	out = binary.BigEndian.AppendUint32(out, k.counter)
	return append(out, attested...)
}

// register answers navigator.credentials.create.
func (k *softKey) register(challenge []byte) []byte {
	pub, err := k.key.PublicKey.Bytes() // 0x04 || X || Y
	if err != nil {
		k.t.Fatal(err)
	}
	cose, err := webauthncbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: pub[1:33], -3: pub[33:65]})
	if err != nil {
		k.t.Fatal(err)
	}
	attested := make([]byte, 16) // AAGUID
	attested = binary.BigEndian.AppendUint16(attested, uint16(len(k.id)))
	attested = append(append(attested, k.id...), cose...)
	att, err := webauthncbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": k.authData(0x45, attested)})
	if err != nil {
		k.t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"id": b64u.EncodeToString(k.id), "rawId": b64u.EncodeToString(k.id), "type": "public-key",
		"response": map[string]string{"clientDataJSON": b64u.EncodeToString(k.clientData("webauthn.create", challenge)),
			"attestationObject": b64u.EncodeToString(att)}})
	return b
}

// sign answers navigator.credentials.get.
func (k *softKey) sign(challenge, userHandle []byte) []byte {
	k.counter++
	cd := k.clientData("webauthn.get", challenge)
	ad := k.authData(0x05, nil)
	h := sha256.Sum256(cd)
	digest := sha256.Sum256(append(slices.Clone(ad), h[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, k.key, digest[:])
	if err != nil {
		k.t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"id": b64u.EncodeToString(k.id), "rawId": b64u.EncodeToString(k.id), "type": "public-key",
		"response": map[string]string{"clientDataJSON": b64u.EncodeToString(cd), "authenticatorData": b64u.EncodeToString(ad),
			"signature": b64u.EncodeToString(sig), "userHandle": b64u.EncodeToString(userHandle)}})
	return b
}

func TestPasskeys(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	_, _, _ = s.EnsureAdmin(ctx, "correct-horse-battery")
	uid, _ := s.AdminID(ctx)
	rp := RP{ID: "netscope.test", Origin: "https://netscope.test"}
	if _, err := s.BeginPasskeyRegistration(ctx, uid, RP{}); !errors.Is(err, ErrPasskeysUnavailable) {
		t.Fatalf("passkeys without relying party: %v", err)
	}

	creation, err := s.BeginPasskeyRegistration(ctx, uid, rp)
	if err != nil {
		t.Fatal(err)
	}
	k := newSoftKey(t, rp)
	pk, codes, err := s.FinishPasskeyRegistration(ctx, uid, rp, " Laptop ", k.register(creation.Response.Challenge))
	if err != nil || pk.Name != "Laptop" || pk.RPID != "netscope.test" || len(codes) != recoveryCodeCount {
		t.Fatalf("register: %+v %d codes %v", pk, len(codes), err)
	}
	var handle []byte
	_ = s.db.R.QueryRowContext(ctx, "SELECT webauthn_id FROM users WHERE id = ?", uid).Scan(&handle)

	// on another host name the passkey is not offered
	res, err := s.Login(ctx, "admin", "correct-horse-battery", "other.test", "1.1.1.1", "")
	if err != nil || !slices.Equal(res.Methods, []string{"recovery"}) {
		t.Fatalf("login on another host: %+v %v", res, err)
	}
	res, err = s.Login(ctx, "admin", "correct-horse-battery", rp.ID, "1.1.1.1", "")
	if err != nil || !slices.Equal(res.Methods, []string{"passkey", "recovery"}) {
		t.Fatalf("login: %+v %v", res, err)
	}
	assertion, err := s.BeginPasskeyLogin(ctx, res.Challenge, rp)
	if err != nil {
		t.Fatal(err)
	}
	signed := k.sign(assertion.Response.Challenge, handle)
	out, err := s.FinishPasskeyLogin(ctx, res.Challenge, rp, signed, "1.1.1.1", "")
	if err != nil || out.Token == "" {
		t.Fatalf("passkey login: %+v %v", out, err)
	}

	// an old assertion does not open a new login
	res, _ = s.Login(ctx, "admin", "correct-horse-battery", rp.ID, "1.1.1.1", "")
	if _, err := s.BeginPasskeyLogin(ctx, res.Challenge, rp); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinishPasskeyLogin(ctx, res.Challenge, rp, signed, "1.1.1.1", ""); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("replayed assertion: %v", err)
	}
	list, _ := s.Passkeys(ctx, uid)
	if len(list) != 1 || list[0].LastUsedAt == nil {
		t.Fatalf("passkey use not recorded: %+v", list)
	}

	// a required second factor keeps its last passkey
	_, _ = s.UpdateRole(ctx, 1, RoleInput{Name: "Administrator", Require2FA: true})
	if err := s.DeletePasskey(ctx, uid, pk.ID); err == nil {
		t.Fatal("last required factor removed")
	}
	_, _ = s.UpdateRole(ctx, 1, RoleInput{Name: "Administrator"})
	if err := s.DeletePasskey(ctx, uid, pk.ID); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.MFA(ctx, uid); len(st.Passkeys) != 0 || st.RecoveryCodes != 0 {
		t.Fatalf("after removing the last factor: %+v", st)
	}
}

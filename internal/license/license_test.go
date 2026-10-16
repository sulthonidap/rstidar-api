package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"
)

func sign(t *testing.T, priv ed25519.PrivateKey, info Info) string {
	t.Helper()
	body, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	return prefix + b64.EncodeToString(body) + "." + b64.EncodeToString(ed25519.Sign(priv, body))
}

func TestParse(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	info := Info{No: "MS-1", RS: "RS A", Issued: "2026-10-03"}
	good := sign(t, priv, info)

	got, err := parse(good, pub)
	if err != nil || got != info {
		t.Fatalf("valid key: got %+v, %v", got, err)
	}
	if _, err := parse("  "+good+"\n", pub); err != nil {
		t.Errorf("surrounding spaces should be ignored: %v", err)
	}

	for name, key := range map[string]string{
		"other signer":  sign(t, otherPriv, info),
		"no prefix":     good[len(prefix):],
		"no signature":  good[:len(good)-87],
		"edited body":   sign(t, priv, info)[:len(prefix)] + "x" + good[len(prefix)+1:],
		"garbage":       "MSND-abc.def",
		"missing rs":    sign(t, priv, Info{No: "MS-1"}),
		"empty":         "",
		"tampered tail": good[:len(good)-1] + flip(good[len(good)-1]),
	} {
		if _, err := parse(key, pub); err == nil {
			t.Errorf("%s: expected invalid", name)
		}
	}
}

func flip(c byte) string {
	if c == 'A' {
		return "B"
	}
	return "A"
}

func TestExpired(t *testing.T) {
	now := time.Date(2026, 10, 3, 15, 0, 0, 0, time.Local)
	cases := map[string]bool{"": false, "2026-10-03": false, "2026-10-04": false, "2026-10-02": true}
	for exp, want := range cases {
		if got := (Info{Expires: exp}).Expired(now); got != want {
			t.Errorf("expires %q: got %v, want %v", exp, got, want)
		}
	}
}

func TestEmbeddedPublicKey(t *testing.T) {
	if _, err := Parse("MSND-x.y"); err != ErrInvalid {
		t.Fatalf("embedded public key should load and reject a bad key, got %v", err)
	}
}

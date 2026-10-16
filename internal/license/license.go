// Package license verifies MedSend license keys offline.
//
// A key is "MSND-" + base64url(JSON payload) + "." + base64url(Ed25519
// signature of that JSON). Keys are issued with the separate license
// generator, which holds the private key; only the public key lives here.
package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// publicKey pairs with the generator's keys/private.key.
const publicKey = "QphoO0ECBNJF17XP1BV7MwlOwX0mf4hxy45QsYBoCAo"

const (
	prefix  = "MSND-"
	dateFmt = "2006-01-02"
)

// Strict: a key with altered padding bits is rejected, not silently accepted.
var b64 = base64.RawURLEncoding.Strict()

// Info is what a license says.
type Info struct {
	No      string `json:"no"`            // license number, e.g. MS-2026-001
	RS      string `json:"rs"`            // the RS it is issued to
	Issued  string `json:"iat"`           // YYYY-MM-DD
	Expires string `json:"exp,omitempty"` // YYYY-MM-DD; empty = never
}

// Expired: past its last valid day (a license without an end date never expires).
func (i Info) Expired(now time.Time) bool {
	if i.Expires == "" {
		return false
	}
	end, err := time.ParseInLocation(dateFmt, i.Expires, now.Location())
	return err == nil && now.After(end.AddDate(0, 0, 1))
}

var ErrInvalid = errors.New("kode lisensi tidak valid")

// Parse checks a key's signature and returns what it says.
func Parse(key string) (Info, error) {
	pub, err := b64.DecodeString(publicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return Info{}, errors.New("kunci publik lisensi rusak")
	}
	return parse(key, ed25519.PublicKey(pub))
}

func parse(key string, pub ed25519.PublicKey) (Info, error) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(key), prefix)
	if !ok {
		return Info{}, ErrInvalid
	}
	b, s, ok := strings.Cut(rest, ".")
	if !ok {
		return Info{}, ErrInvalid
	}
	body, err := b64.DecodeString(b)
	if err != nil {
		return Info{}, ErrInvalid
	}
	sig, err := b64.DecodeString(s)
	if err != nil || !ed25519.Verify(pub, body, sig) {
		return Info{}, ErrInvalid
	}
	var info Info
	if err := json.Unmarshal(body, &info); err != nil || info.No == "" || info.RS == "" {
		return Info{}, ErrInvalid
	}
	return info, nil
}

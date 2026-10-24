package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"tailscale.com/ipn"
	"tailscale.com/ipn/store/mem"
)

func newKey(t *testing.T) string {
	k := make([]byte, 32)
	rand.Read(k)
	return base64.StdEncoding.EncodeToString(k)
}

func TestEncStore(t *testing.T) {
	raw := new(mem.Store)
	key := newKey(t)
	s, err := newEncStore(raw, key, "app:tsnet")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WriteState("a", []byte("secret")); err != nil {
		t.Fatal(err)
	}
	stored, _ := raw.ReadState("a")
	if bytes.Contains(stored, []byte("secret")) || !bytes.HasPrefix(stored, encMagic) {
		t.Fatalf("value not encrypted: %q", stored)
	}
	if got, err := s.ReadState("a"); err != nil || string(got) != "secret" {
		t.Fatalf("roundtrip: %q, %v", got, err)
	}

	// wrong key
	other, _ := newEncStore(raw, newKey(t), "app:tsnet")
	if _, err := other.ReadState("a"); err == nil || !strings.Contains(err.Error(), "wrong BOTBOARD_STATE_KEY") {
		t.Fatalf("wrong key: %v", err)
	}
	// value moved to another id or app must not decrypt
	raw.WriteState("b", stored)
	if _, err := s.ReadState("b"); err == nil {
		t.Fatal("ciphertext accepted under a different id")
	}
	moved, _ := newEncStore(raw, key, "other:tsnet")
	if _, err := moved.ReadState("a"); err == nil {
		t.Fatal("ciphertext accepted under a different scope")
	}
	// ciphertext without a key configured
	off, _ := newEncStore(raw, "", "app:tsnet")
	if _, err := off.ReadState("a"); err == nil || !strings.Contains(err.Error(), "set BOTBOARD_STATE_KEY") {
		t.Fatalf("encrypted state without key: %v", err)
	}
	// plaintext state with a key configured
	raw.WriteState("p", []byte("plain"))
	if _, err := s.ReadState("p"); err == nil || !strings.Contains(err.Error(), "not encrypted") {
		t.Fatalf("plaintext with key: %v", err)
	}
	// off: plain passthrough; delete; missing
	if err := off.WriteState("p2", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if got, _ := off.ReadState("p2"); string(got) != "x" {
		t.Fatal("passthrough broken")
	}
	s.WriteState("a", nil)
	if _, err := s.ReadState("a"); err != ipn.ErrStateNotExist {
		t.Fatalf("after delete: %v", err)
	}
	if _, err := newEncStore(raw, "short", "x"); err == nil {
		t.Fatal("bad key accepted")
	}
}

package main

import (
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"tailscale.com/ipn"
)

func TestRedisStore(t *testing.T) {
	mr := miniredis.RunT(t)
	s, err := newRedisStore("redis://"+mr.Addr(), "botboard:tsnet")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadState("k"); !errors.Is(err, ipn.ErrStateNotExist) {
		t.Fatalf("want ErrStateNotExist, got %v", err)
	}
	if err := s.WriteState("k", []byte("v")); err != nil {
		t.Fatal(err)
	}
	if b, err := s.ReadState("k"); err != nil || string(b) != "v" {
		t.Fatalf("got %q, %v", b, err)
	}
	if err := s.WriteState("k", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadState("k"); !errors.Is(err, ipn.ErrStateNotExist) {
		t.Fatalf("want ErrStateNotExist after delete, got %v", err)
	}
}

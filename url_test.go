package main

import "testing"

func TestCleanURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/o/r/pull/1": "https://github.com/o/r/pull/1",
		"http://x/y":                    "http://x/y",
		"javascript:alert(1)":           "",
		"ftp://x/y":                     "",
		"/relative":                     "",
		"":                              "",
	} {
		if got := cleanURL(in); got != want {
			t.Errorf("cleanURL(%q)=%q want %q", in, got, want)
		}
	}
}

func TestCardURLKeptOnUpdate(t *testing.T) {
	s := NewStore()
	s.UpsertCard(Card{ID: "a", Agent: "x", URL: "https://e.com/1"})
	if c := s.UpsertCard(Card{ID: "a", Activity: "hi"}); c.URL != "https://e.com/1" {
		t.Errorf("url lost: %q", c.URL)
	}
}

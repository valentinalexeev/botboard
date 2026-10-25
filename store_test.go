package main

import (
	"testing"
	"time"
)

func TestExpireCards(t *testing.T) {
	s := NewStore()
	s.UpsertCard(Card{Agent: "old", Task: "a"})
	s.UpsertCard(Card{Agent: "new", Task: "b"})
	s.mu.Lock()
	for id, c := range s.cards {
		if c.Agent == "old" {
			c.Updated = time.Now().Add(-48 * time.Hour)
			s.cards[id] = c
		}
	}
	s.mu.Unlock()

	if n := s.ExpireCards(24 * time.Hour); n != 1 {
		t.Fatalf("expired %d, want 1", n)
	}
	cards := s.Snapshot().Cards
	if len(cards) != 1 || cards[0].Agent != "new" {
		t.Fatalf("unexpected cards after expiry: %+v", cards)
	}
}

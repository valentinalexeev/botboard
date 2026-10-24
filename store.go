package main

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const maxNotes = 200

// Card is the "what am I doing right now" record of one agent.
type Card struct {
	ID       string    `json:"id"`
	Agent    string    `json:"agent"`
	Project  string    `json:"project"`
	Task     string    `json:"task"`
	Phase    string    `json:"phase"`
	Activity string    `json:"activity"`
	URL      string    `json:"url,omitempty"`
	Updated  time.Time `json:"updated"`
}

// Note is a twitter-style one-way message.
type Note struct {
	ID      uint64    `json:"id"`
	Agent   string    `json:"agent"`
	Text    string    `json:"text"`
	Project string    `json:"project,omitempty"`
	URL     string    `json:"url,omitempty"`
	Time    time.Time `json:"time"`
}

// Event is what is pushed to websocket clients.
type Event struct {
	Type string `json:"type"` // "card", "card_removed", "note"
	Card *Card  `json:"card,omitempty"`
	Note *Note  `json:"note,omitempty"`
	ID   string `json:"id,omitempty"`
}

type Snapshot struct {
	Cards []Card `json:"cards"`
	Notes []Note `json:"notes"` // newest first
}

// Store keeps everything in memory only.
type Store struct {
	mu     sync.Mutex
	cards  map[string]Card
	notes  []Note // oldest first
	nextID uint64
	subs   map[chan Event]struct{}
}

func NewStore() *Store {
	return &Store{cards: map[string]Card{}, subs: map[chan Event]struct{}{}}
}

// cardKey identifies a card when the publisher gives no explicit id.
func cardKey(agent, project, task string) string {
	return strings.ToLower(agent + "|" + project + "|" + task)
}

// UpsertCard creates or updates a card. Empty fields on an update keep the old value.
func (s *Store) UpsertCard(c Card) Card {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.ID == "" {
		c.ID = cardKey(c.Agent, c.Project, c.Task)
	}
	if old, ok := s.cards[c.ID]; ok {
		if c.Agent == "" {
			c.Agent = old.Agent
		}
		if c.Project == "" {
			c.Project = old.Project
		}
		if c.Task == "" {
			c.Task = old.Task
		}
		if c.Phase == "" {
			c.Phase = old.Phase
		}
		if c.Activity == "" {
			c.Activity = old.Activity
		}
		if c.URL == "" {
			c.URL = old.URL
		}
	}
	c.Updated = time.Now()
	s.cards[c.ID] = c
	s.broadcast(Event{Type: "card", Card: &c})
	return c
}

func (s *Store) DeleteCard(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.cards[id]; !ok {
		return false
	}
	delete(s.cards, id)
	s.broadcast(Event{Type: "card_removed", ID: id})
	return true
}

func (s *Store) AddNote(n Note) Note {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	n.ID = s.nextID
	n.Time = time.Now()
	s.notes = append(s.notes, n)
	if len(s.notes) > maxNotes {
		s.notes = append([]Note(nil), s.notes[len(s.notes)-maxNotes:]...)
	}
	s.broadcast(Event{Type: "note", Note: &n})
	return n
}

func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := Snapshot{Cards: make([]Card, 0, len(s.cards)), Notes: make([]Note, 0, len(s.notes))}
	for _, c := range s.cards {
		snap.Cards = append(snap.Cards, c)
	}
	sort.Slice(snap.Cards, func(i, j int) bool { return snap.Cards[i].Updated.After(snap.Cards[j].Updated) })
	for i := len(s.notes) - 1; i >= 0; i-- {
		snap.Notes = append(snap.Notes, s.notes[i])
	}
	return snap
}

// Subscribe returns a channel of events plus the snapshot taken atomically with it.
func (s *Store) Subscribe() (chan Event, Snapshot, func()) {
	ch := make(chan Event, 64)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	snap := s.Snapshot()
	return ch, snap, func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}
}

// broadcast must be called with s.mu held. A client that cannot keep up is
// disconnected (its channel closed) and resyncs from a snapshot on reconnect.
func (s *Store) broadcast(e Event) {
	for ch := range s.subs {
		select {
		case ch <- e:
		default:
			delete(s.subs, ch)
			close(ch)
		}
	}
}

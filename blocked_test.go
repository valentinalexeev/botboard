package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPostBlockedCard(t *testing.T) {
	srv := &server{store: NewStore()}
	post := func(body string) int {
		w := httptest.NewRecorder()
		srv.handlePostCard(w, httptest.NewRequest("POST", "/api/cards", strings.NewReader(body)))
		return w.Code
	}
	// Earlier url/activity are not enough: blocking must name the decision URL and reason itself.
	if code := post(`{"id":"c","agent":"a","task":"t","url":"https://e.com/pr/1","activity":"coding"}`); code != http.StatusOK {
		t.Fatalf("plain card: %d", code)
	}
	for _, body := range []string{
		`{"id":"c","agent":"a","phase":"blocked"}`,
		`{"id":"c","agent":"a","phase":"blocked","activity":"need a decision on the schema"}`,
		`{"id":"c","agent":"a","phase":"Blocked","url":"https://e.com/issue/7"}`,
		`{"id":"c","agent":"a","phase":"blocked","url":"javascript:alert(1)","activity":"x"}`,
	} {
		if code := post(body); code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", body, code)
		}
	}
	if code := post(`{"id":"c","agent":"a","phase":"blocked","url":"https://e.com/issue/7","activity":"which DB? asked in the issue"}`); code != http.StatusOK {
		t.Fatalf("valid blocked card: %d", code)
	}
	c := srv.store.Snapshot().Cards[0]
	if c.Phase != "blocked" || c.URL != "https://e.com/issue/7" || c.Task != "t" {
		t.Errorf("unexpected card: %+v", c)
	}
	// Leaving the blocked phase needs nothing extra.
	if code := post(`{"id":"c","agent":"a","phase":"build"}`); code != http.StatusOK {
		t.Errorf("unblock: %d", code)
	}
}

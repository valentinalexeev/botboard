// botboard: a tiny in-memory dashboard where agents publish what they are doing.
// It joins your tailnet as its own node (tsnet), so no separate Tailscale daemon is needed.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"tailscale.com/client/local"
	"tailscale.com/tsnet"
)

//go:embed web
var webFS embed.FS

type server struct {
	store *Store
	lc    *local.Client // nil in -local mode

	fadeAfter, expireAfter time.Duration // 0 = off; sent to the dashboard
}

func main() {
	var (
		hostname    = flag.String("hostname", env("TS_HOSTNAME", "botboard"), "tailnet hostname (env TS_HOSTNAME)")
		stateDir    = flag.String("state-dir", os.Getenv("TS_STATE_DIR"), "tsnet state dir (env TS_STATE_DIR; default: user config dir)")
		redisURL    = flag.String("redis-url", os.Getenv("REDIS_URL"), "keep tailnet state in this Redis instead of on disk (env REDIS_URL)")
		redisKey    = flag.String("redis-key", os.Getenv("REDIS_KEY"), "Redis hash key for the state (env REDIS_KEY; default <FLY_APP_NAME or hostname>:tsnet)")
		control     = flag.String("control-url", os.Getenv("TS_CONTROL_URL"), "coordination server URL, e.g. https://headscale.example.com (default: Tailscale; env TS_CONTROL_URL)")
		useTLS      = flag.Bool("tls", os.Getenv("BOTBOARD_TLS") == "1", "serve HTTPS on :443 with a Tailscale cert (needs HTTPS enabled in the tailnet)")
		fadeAfter   = flag.Duration("fade-after", envDuration("BOTBOARD_FADE_AFTER", 2*time.Hour), "dim cards not updated for this long; 0 disables (env BOTBOARD_FADE_AFTER)")
		expireAfter = flag.Duration("expire-after", envDuration("BOTBOARD_EXPIRE_AFTER", 24*time.Hour), "remove cards not updated for this long; 0 disables (env BOTBOARD_EXPIRE_AFTER)")
		local_      = flag.String("local", os.Getenv("BOTBOARD_LOCAL"), "skip Tailscale and listen on this address instead, e.g. :8080 (for development)")
	)
	flag.Parse()

	srv := &server{store: NewStore(), fadeAfter: *fadeAfter, expireAfter: *expireAfter}
	if *expireAfter > 0 {
		go func() {
			for range time.Tick(time.Minute) {
				srv.store.ExpireCards(*expireAfter)
			}
		}()
	}
	mux := http.NewServeMux()
	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("GET /", http.FileServerFS(sub))
	mux.HandleFunc("GET /agents.md", srv.handleAgentsDoc)
	mux.HandleFunc("GET /llms.txt", srv.handleAgentsDoc)
	mux.HandleFunc("GET /ws", srv.handleWS)
	mux.HandleFunc("GET /api/state", srv.handleState)
	mux.HandleFunc("POST /api/cards", srv.handlePostCard)
	mux.HandleFunc("DELETE /api/cards/{id...}", srv.handleDeleteCard)
	mux.HandleFunc("POST /api/notes", srv.handlePostNote)

	var ln net.Listener
	var err error
	if *local_ != "" {
		ln, err = net.Listen("tcp", *local_)
		log.Printf("listening on %s (no tailscale)", *local_)
	} else {
		ts := &tsnet.Server{Hostname: *hostname, Dir: *stateDir, ControlURL: *control} // TS_AUTHKEY is read from the environment
		defer ts.Close()
		if *redisURL != "" {
			key := *redisKey
			if key == "" {
				// Shared Redis rule: every key is prefixed with the app name (<app>:<key>).
				key = env("FLY_APP_NAME", *hostname) + ":tsnet"
			}
			rs, rerr := newRedisStore(*redisURL, key)
			if rerr != nil {
				log.Fatalf("redis state store: %v", rerr)
			}
			// Optional at-rest encryption (BOTBOARD_STATE_KEY); off when unset.
			if ts.Store, err = newEncStore(rs, os.Getenv("BOTBOARD_STATE_KEY"), key); err != nil {
				log.Fatal(err)
			}
		} else if os.Getenv("BOTBOARD_STATE_KEY") != "" {
			log.Print("warning: BOTBOARD_STATE_KEY only applies to Redis state (REDIS_URL); ignored")
		}
		if srv.lc, err = ts.LocalClient(); err != nil {
			log.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		st, upErr := ts.Up(ctx)
		cancel()
		if upErr != nil {
			log.Fatalf("tailscale up: %v (set TS_AUTHKEY, or follow the login URL in the logs)", upErr)
		}
		if *useTLS && *control != "" {
			log.Print("warning: -tls relies on Tailscale-issued certs, which headscale does not provide; expect cert errors")
		}
		if *useTLS {
			ln, err = ts.ListenTLS("tcp", ":443")
		} else {
			ln, err = ts.Listen("tcp", ":80")
		}
		if err == nil {
			name := strings.TrimSuffix(st.Self.DNSName, ".")
			scheme := "http"
			if *useTLS {
				scheme = "https"
			}
			log.Printf("botboard is up on your tailnet: %s://%s/", scheme, name)
		}
	}
	if err != nil {
		log.Fatal(err)
	}

	hs := &http.Server{Handler: mux}
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		<-c
		hs.Close()
	}()
	if err := hs.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envDuration(k string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(os.Getenv(k)); err == nil {
		return d
	}
	return def
}

// whoami returns the tailnet identity of the caller (node name), or "" if unknown.
func (s *server) whoami(r *http.Request) string {
	if s.lc == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	who, err := s.lc.WhoIs(ctx, r.RemoteAddr)
	if err != nil || who.Node == nil {
		return ""
	}
	return strings.SplitN(who.Node.ComputedName, ".", 2)[0]
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// cleanURL keeps only absolute http(s) URLs (no javascript: etc.); anything else becomes "".
func cleanURL(raw string) string {
	raw = clip(raw, 500)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return raw
}

func (s *server) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.Snapshot())
}

func (s *server) handlePostCard(w http.ResponseWriter, r *http.Request) {
	var c Card
	if !decode(w, r, &c) {
		return
	}
	c.ID, c.Agent = clip(c.ID, 100), clip(c.Agent, 60)
	c.Project, c.Task = clip(c.Project, 100), clip(c.Task, 200)
	c.Phase, c.Activity = strings.ToLower(clip(c.Phase, 30)), clip(c.Activity, 300)
	c.URL = cleanURL(c.URL)
	if c.Agent == "" {
		c.Agent = s.whoami(r)
	}
	if c.Agent == "" {
		http.Error(w, "agent is required", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, s.store.UpsertCard(c))
}

func (s *server) handleDeleteCard(w http.ResponseWriter, r *http.Request) {
	if !s.store.DeleteCard(r.PathValue("id")) {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handlePostNote(w http.ResponseWriter, r *http.Request) {
	var n Note
	if !decode(w, r, &n) {
		return
	}
	n.Agent, n.Project, n.Text = clip(n.Agent, 60), clip(n.Project, 100), clip(n.Text, 500)
	n.URL = cleanURL(n.URL)
	if n.Agent == "" {
		n.Agent = s.whoami(r)
	}
	if n.Agent == "" || n.Text == "" {
		http.Error(w, "agent and text are required", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, s.store.AddNote(n))
}

func (s *server) handleWS(w http.ResponseWriter, r *http.Request) {
	// Same-origin only (default check); the page is served by this server.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()

	ch, snap, unsub := s.store.Subscribe()
	defer unsub()

	ctx := conn.CloseRead(r.Context()) // we only write; this also handles pings/close
	if err := wsjson.Write(ctx, conn, map[string]any{"type": "snapshot", "cards": snap.Cards, "notes": snap.Notes,
		"fade_after": s.fadeAfter.Seconds(), "expire_after": s.expireAfter.Seconds()}); err != nil {
		return
	}
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-ch:
			if !ok {
				conn.Close(websocket.StatusTryAgainLater, "too slow")
				return
			}
			if err := wsjson.Write(ctx, conn, e); err != nil {
				return
			}
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// handleAgentsDoc serves the agent instructions with the URL the caller used filled in.
func (s *server) handleAgentsDoc(w http.ResponseWriter, r *http.Request) {
	doc, err := webFS.ReadFile("web/agents.md")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	base := scheme + "://" + r.Host
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Write([]byte(strings.ReplaceAll(string(doc), "{{BASE}}", base)))
}

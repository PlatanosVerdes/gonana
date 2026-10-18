// Package botstats keeps what is worth knowing about a bot's use: who talks to it, how much, and
// what it turned away. It serves that as the JSON muninn reads, so a bot needs three calls and a
// listener to show up on its page.
//
//	rec, _ := botstats.Open("/data/bot-stats.json")
//	go rec.Loop(ctx)
//	rec.Message(chatID, firstName, username) // on every update
//	rec.Rejected()                            // when a rate limit turns one away
//	http.ListenAndServe(":9180", rec.Handler(about))
//
// Serve it on a port of its own that is never published: the people list holds names.
package botstats

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Stats is the /bot-stats document. Platform and Bot say which bot it is ("telegram",
// "TorreFlightsBot"); Extra is the bot's own figures, shown as they come.
type Stats struct {
	Platform string   `json:"platform"`
	Bot      string   `json:"bot"`
	Name     string   `json:"name,omitempty"`
	Users    Users    `json:"users"`
	People   []Person `json:"people"`
	Traffic  Traffic  `json:"traffic"`
	Extra    []Extra  `json:"extra,omitempty"`
}

type Users struct {
	Total     int `json:"total"`
	Active24h int `json:"active_24h"`
	Active7d  int `json:"active_7d"`
	New24h    int `json:"new_24h"`
}

type Person struct {
	ID        string    `json:"id"`
	Name      string    `json:"name,omitempty"`
	Username  string    `json:"username,omitempty"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Messages  int       `json:"messages"`
}

// Traffic is what reached the bot: PerMinute holds the last 60 minutes, oldest first.
type Traffic struct {
	PerMinute   []int `json:"per_minute"`
	Messages24h int   `json:"messages_24h"`
	Rejected24h int   `json:"rejected_24h"`
}

type Extra struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// About is what only the bot knows: which bot it is, and its own figures at this moment.
type About struct {
	Platform string
	Bot      string
	Name     string
	Extra    []Extra
}

const day = 24 * 60 // minutes

// Recorder counts a bot's traffic by minute over the last day and keeps everyone who wrote to it.
// The people are saved to disk; the minute counts start over when the bot restarts.
type Recorder struct {
	mu       sync.Mutex
	path     string
	now      func() time.Time
	people   map[string]*Person
	messages [day]int
	rejected [day]int
	minute   int64 // the minute the counters were last moved to
	dirty    bool
}

// Open loads the people saved at path, if any, and records from now on.
func Open(path string) (*Recorder, error) {
	r := &Recorder{path: path, now: time.Now, people: map[string]*Person{}}
	r.minute = r.now().Unix() / 60
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	var saved []Person
	if err := json.Unmarshal(raw, &saved); err != nil {
		return nil, err
	}
	for i := range saved {
		p := saved[i]
		r.people[p.ID] = &p
	}
	return r, nil
}

// Message counts one update from a person, and refreshes their name: people rename themselves.
func (r *Recorder) Message(id, name, username string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	r.advance(now)
	r.messages[r.minute%day]++
	p := r.people[id]
	if p == nil {
		p = &Person{ID: id, FirstSeen: now}
		r.people[id] = p
	}
	if name != "" {
		p.Name = name
	}
	if username != "" {
		p.Username = username
	}
	p.LastSeen = now
	p.Messages++
	r.dirty = true
}

// Rejected counts one request turned away, by a rate limit or as abuse.
func (r *Recorder) Rejected() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.advance(r.now())
	r.rejected[r.minute%day]++
}

// advance moves the minute ring to now, emptying the minutes nothing happened in.
func (r *Recorder) advance(now time.Time) {
	cur := now.Unix() / 60
	if cur <= r.minute {
		return
	}
	for m := r.minute + 1; m <= cur && m <= r.minute+day; m++ {
		r.messages[m%day], r.rejected[m%day] = 0, 0
	}
	r.minute = cur
}

// Snapshot is the document as of now.
func (r *Recorder) Snapshot(about About) Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	r.advance(now)

	s := Stats{Platform: about.Platform, Bot: about.Bot, Name: about.Name, Extra: about.Extra, People: []Person{}}
	for _, p := range r.people {
		s.People = append(s.People, *p)
		s.Users.Total++
		if now.Sub(p.LastSeen) < 24*time.Hour {
			s.Users.Active24h++
		}
		if now.Sub(p.LastSeen) < 7*24*time.Hour {
			s.Users.Active7d++
		}
		if now.Sub(p.FirstSeen) < 24*time.Hour {
			s.Users.New24h++
		}
	}
	sort.Slice(s.People, func(i, j int) bool { return s.People[i].LastSeen.After(s.People[j].LastSeen) })

	s.Traffic.PerMinute = make([]int, 60)
	for i := 0; i < 60; i++ {
		s.Traffic.PerMinute[i] = r.messages[(r.minute-59+int64(i))%day]
	}
	for i := 0; i < day; i++ {
		s.Traffic.Messages24h += r.messages[i]
		s.Traffic.Rejected24h += r.rejected[i]
	}
	return s
}

// Save writes the people to disk when someone wrote since the last save.
func (r *Recorder) Save() error {
	r.mu.Lock()
	if !r.dirty {
		r.mu.Unlock()
		return nil
	}
	list := make([]Person, 0, len(r.people))
	for _, p := range r.people {
		list = append(list, *p)
	}
	r.dirty = false
	r.mu.Unlock()

	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(r.path), "."+filepath.Base(r.path)+".tmp")
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

// Loop saves once a minute, at most, until ctx ends, and once more on the way out. One write a
// minute rather than one per message: a busy bot on an SD card should not wear it out.
func (r *Recorder) Loop(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = r.Save()
			return
		case <-tick.C:
			_ = r.Save()
		}
	}
}

// Handler serves the document as JSON at any path; about is asked on every request.
func (r *Recorder) Handler(about func() About) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(r.Snapshot(about()))
	})
}

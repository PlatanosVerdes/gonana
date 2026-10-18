package botstats

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

var start = time.Date(2026, 10, 18, 12, 0, 30, 0, time.UTC)

func newRecorder(t *testing.T) (*Recorder, *time.Time) {
	t.Helper()
	r, err := Open(filepath.Join(t.TempDir(), "stats.json"))
	if err != nil {
		t.Fatal(err)
	}
	clock := start
	r.now = func() time.Time { return clock }
	r.minute = clock.Unix() / 60
	return r, &clock
}

func TestCountsPeopleAndTraffic(t *testing.T) {
	r, clock := newRecorder(t)
	r.Message("1", "Ana", "ana")
	r.Message("1", "", "")
	r.Message("2", "Bob", "")
	r.Rejected()

	*clock = clock.Add(3 * time.Minute)
	r.Message("2", "Bobby", "bobby")

	s := r.Snapshot(About{Platform: "telegram", Bot: "TestBot"})
	if s.Users.Total != 2 || s.Users.Active24h != 2 || s.Users.New24h != 2 {
		t.Errorf("users %+v", s.Users)
	}
	if s.Traffic.Messages24h != 4 || s.Traffic.Rejected24h != 1 {
		t.Errorf("traffic %+v", s.Traffic)
	}
	if got := s.Traffic.PerMinute; len(got) != 60 || got[59] != 1 || got[56] != 3 {
		t.Errorf("per minute ends %v, want 3 messages three minutes ago and 1 now", got[55:])
	}
	if s.People[0].ID != "2" || s.People[0].Name != "Bobby" || s.People[0].Username != "bobby" || s.People[0].Messages != 2 {
		t.Errorf("latest person %+v, want Bob renamed Bobby with 2 messages", s.People[0])
	}
	if s.People[1].Name != "Ana" || s.People[1].Messages != 2 {
		t.Errorf("an empty name on a later message wiped %+v", s.People[1])
	}
}

func TestOldTrafficAndActivityAgeOut(t *testing.T) {
	r, clock := newRecorder(t)
	r.Message("1", "Ana", "")
	r.Rejected()

	*clock = clock.Add(25 * time.Hour)
	s := r.Snapshot(About{})
	if s.Traffic.Messages24h != 0 || s.Traffic.Rejected24h != 0 {
		t.Errorf("a day later the counters still hold %+v", s.Traffic)
	}
	if s.Users.Total != 1 || s.Users.Active24h != 0 || s.Users.Active7d != 1 || s.Users.New24h != 0 {
		t.Errorf("users a day later %+v", s.Users)
	}

	*clock = clock.Add(7 * 24 * time.Hour)
	if s := r.Snapshot(About{}); s.Users.Total != 1 || s.Users.Active7d != 0 {
		t.Errorf("users a week later %+v", s.Users)
	}
}

func TestPeopleSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r.Message("7", "Eve", "eve")
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s := again.Snapshot(About{})
	if s.Users.Total != 1 || s.People[0].Name != "Eve" || s.People[0].Messages != 1 {
		t.Errorf("after a restart %+v", s.People)
	}
	if s.Traffic.Messages24h != 0 {
		t.Error("the minute counts are meant to start over")
	}
}

func TestHandlerServesTheDocument(t *testing.T) {
	r, _ := newRecorder(t)
	r.Message("1", "Ana", "ana")
	rec := httptest.NewRecorder()
	r.Handler(func() About {
		return About{Platform: "telegram", Bot: "TestBot", Name: "Test", Extra: []Extra{{"Flights followed", "3"}}}
	}).ServeHTTP(rec, httptest.NewRequest("GET", "/bot-stats", nil))

	var s Stats
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	if s.Platform != "telegram" || s.Bot != "TestBot" || len(s.Extra) != 1 || s.Users.Total != 1 {
		t.Errorf("document %+v", s)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Error("not served as JSON")
	}
}

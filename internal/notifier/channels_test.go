package notifier

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/elqsar/pganalyzer/internal/config"
)

func sampleAlert() Message {
	return Message{
		Kind: KindAlert, Instance: "db:5432/app", Title: "1 new critical",
		Items: []Item{{
			Event: EventOpened, Severity: "critical", RuleID: "slow_query",
			Title: "Slow query: SELECT * FROM a WHERE x < 1 & y > 2",
			URL:   "https://pga.example.com/suggestions/1",
		}},
		URL: "https://pga.example.com/suggestions",
	}
}

func TestSlackChannel(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	chans, err := NewChannels([]config.ChannelConfig{{Type: config.ChannelSlack, URL: srv.URL + "/services/SECRET"}}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(chans[0].Name(), "SECRET") {
		t.Errorf("channel name exposes the webhook path: %s", chans[0].Name())
	}
	if err := chans[0].Send(context.Background(), sampleAlert()); err != nil {
		t.Fatal(err)
	}

	text, _ := got["text"].(string)
	for _, want := range []string{
		"*pganalyzer · db:5432/app* 1 new critical",
		"🔴 CRITICAL: <https://pga.example.com/suggestions/1|Slow query: SELECT * FROM a WHERE x &lt; 1 &amp; y &gt; 2>",
		"<https://pga.example.com/suggestions|Open dashboard>",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("slack text missing %q:\n%s", want, text)
		}
	}
}

func TestWebhookChannel(t *testing.T) {
	var got Message
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	chans, err := NewChannels([]config.ChannelConfig{{
		Type: config.ChannelWebhook, URL: srv.URL, Headers: map[string]string{"Authorization": "Bearer t0ken"},
	}}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := chans[0].Send(context.Background(), sampleAlert()); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer t0ken" {
		t.Errorf("Authorization = %q", auth)
	}
	if got.Kind != KindAlert || len(got.Items) != 1 || got.Items[0].RuleID != "slow_query" {
		t.Errorf("payload = %+v", got)
	}
	if !strings.Contains(got.Text, "[Slow query: SELECT * FROM a WHERE x < 1 & y > 2](https://pga.example.com/suggestions/1)") {
		t.Errorf("markdown text = %q", got.Text)
	}
}

func TestChannelErrorsHideURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid_token", http.StatusForbidden)
	}))
	defer srv.Close()

	chans, _ := NewChannels([]config.ChannelConfig{{Type: config.ChannelSlack, URL: srv.URL + "/services/SECRET"}}, srv.Client())
	err := chans[0].Send(context.Background(), sampleAlert())
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "invalid_token") {
		t.Fatalf("err = %v, want status and body", err)
	}

	// A connection failure must not echo the URL either.
	srv.Close()
	err = chans[0].Send(context.Background(), sampleAlert())
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err = %v, want an error without the URL", err)
	}
}

func TestSendTestRequiresEveryChannel(t *testing.T) {
	ok := &recorder{}
	bad := &recorder{fail: true}
	if err := SendTest(context.Background(), []Channel{ok, bad}, "db", ""); err == nil {
		t.Fatal("SendTest succeeded although a channel failed")
	}
	if len(ok.sent) != 1 || ok.sent[0].Kind != KindTest {
		t.Errorf("working channel got %+v", ok.sent)
	}
}

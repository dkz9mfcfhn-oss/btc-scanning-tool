package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type ntfyRoundTripFunc func(*http.Request) (*http.Response, error)

func (f ntfyRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestNTFYPublishesChineseMessageAndChecksAcknowledgement(t *testing.T) {
	for _, tc := range []struct {
		reply string
		ok    bool
	}{
		{`{"id":"test","topic":"private_alarms"}`, true},
		{`{"id":"test","topic":"unexpected"}`, false},
	} {
		client := &http.Client{Transport: ntfyRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			defer r.Body.Close()
			var payload map[string]string
			if r.Method != "POST" || r.URL.String() != "https://ntfy.sh/" ||
				r.Header.Get("Authorization") != "Bearer tk_0123456789abcdefghijkl" ||
				json.NewDecoder(r.Body).Decode(&payload) != nil || payload["topic"] != "private_alarms" ||
				payload["title"] != "掉线提醒" || payload["message"] != "第一行\n第二行" {
				t.Fatalf("unexpected ntfy request: method %s, payload %v", r.Method, payload)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.reply))}, nil
		})}
		if err := publishNTFY(context.Background(), client, "private_alarms tk_0123456789abcdefghijkl", "掉线提醒", "第一行\n第二行"); (err == nil) != tc.ok {
			t.Fatalf("acknowledgement %s: %v", tc.reply, err)
		}
	}
	if _, _, err := parseNTFY("https://example.com/anything"); err == nil {
		t.Fatal("accepted arbitrary server URL")
	}
}

func TestNTFYMorningReportRetriesIndependently(t *testing.T) {
	a, err := newApp(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.db.Close()
	if err := a.setNTFY("   RandomTopic042619  "); err != nil {
		t.Fatal(err)
	}
	a.pushURL = "https://example.test/test-key"
	a.devices["192.168.1.2"] = &Device{IP: "192.168.1.2"}
	a.market = marketQuote{}
	bark, android := 0, 0
	a.dailySender = func(_ context.Context, endpoint, _, _ string) error {
		switch endpoint {
		case a.pushURL:
			bark++
		case "RandomTopic042619":
			android++
			if android == 1 {
				return errors.New("retry Android")
			}
		default:
			t.Fatalf("unexpected endpoint: %q", endpoint)
		}
		return nil
	}
	now := time.Now().UTC().Truncate(24 * time.Hour)
	a.maybeDailyReport(context.Background(), now)
	a.jobs.Wait()
	a.maybeDailyReport(context.Background(), now.Add(5*time.Minute))
	a.jobs.Wait()
	if bark != 1 || android != 2 {
		t.Fatalf("send counts: Bark %d, Android %d", bark, android)
	}
	a.ntfySettings = ""
	a.loadNTFY()
	a.loadDailyReport()
	if a.ntfySettings != "RandomTopic042619" || a.ntfyDailyState.Next != 1 || a.dailyState.Next != 1 {
		t.Fatal("channel settings or completed delivery state were not restored")
	}
}

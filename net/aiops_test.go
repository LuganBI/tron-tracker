package net

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReportAIOpsAlertSendsOneFatalEventPerApp(t *testing.T) {
	var events []aiopsEvent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
			t.Errorf("content type = %q, want application/json", got)
		}

		var event aiopsEvent
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Errorf("decode event: %v", err)
		}
		events = append(events, event)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"success","message":"receive event success","code":"200"}`))
	}))
	defer server.Close()

	previousEndpoint := aiopsEventEndpoint
	aiopsEventEndpoint = server.URL
	t.Cleanup(func() { aiopsEventEndpoint = previousEndpoint })

	alert := AIOpsAlert{
		EventID:      "tx-hash",
		EventType:    "trigger",
		AlarmName:    "fatal transaction",
		AlarmContent: "details",
		Priority:     5,
	}
	if err := ReportAIOpsAlert([]string{"app-one", "app-two"}, alert); err != nil {
		t.Fatalf("ReportAIOpsAlert() error = %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
	if events[0].App != "app-one" || events[1].App != "app-two" {
		t.Fatalf("apps = %q, %q, want app-one/app-two", events[0].App, events[1].App)
	}
	for _, event := range events {
		if event.EventID != "tx-hash" || event.EventType != "trigger" || event.Priority != 5 {
			t.Errorf("event = %#v, want trigger tx-hash at priority 5", event)
		}
	}
}

func TestReportAIOpsAlertDoesNothingWithoutApps(t *testing.T) {
	if err := ReportAIOpsAlert(nil, AIOpsAlert{}); err != nil {
		t.Fatalf("ReportAIOpsAlert() error = %v", err)
	}
}

func TestReportAIOpsAlertDoesNotLeakAppKeyInError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"result":"failed","message":"upstream failed"}`))
	}))
	defer server.Close()

	previousEndpoint := aiopsEventEndpoint
	aiopsEventEndpoint = server.URL
	t.Cleanup(func() { aiopsEventEndpoint = previousEndpoint })

	const secret = "secret-app-key"
	err := ReportAIOpsAlert([]string{secret}, AIOpsAlert{EventID: "tx"})
	if err == nil {
		t.Fatal("ReportAIOpsAlert() error = nil, want HTTP error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked app key: %v", err)
	}
}

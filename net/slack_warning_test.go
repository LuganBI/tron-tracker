package net

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tron-tracker/config"
)

func TestReportOnChainMonitorAndWarningMessageToSlack(t *testing.T) {
	monitorMessages := make(chan SlackMessage, 1)
	warningMessages := make(chan SlackMessage, 1)
	newSlackServer := func(messages chan<- SlackMessage) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var msg SlackMessage
			if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
				t.Errorf("decode Slack message: %v", err)
			}
			messages <- msg
			w.WriteHeader(http.StatusOK)
		}))
	}

	monitorServer := newSlackServer(monitorMessages)
	defer monitorServer.Close()
	warningServer := newSlackServer(warningMessages)
	defer warningServer.Close()

	previousConfigs := configs
	configs = &config.NetConfig{WarningWebhook: warningServer.URL}
	t.Cleanup(func() { configs = previousConfigs })

	msg := SlackMessage{
		Text: "combined internal transaction alert",
		Blocks: []SlackBlock{{
			Type: "header",
			Text: &SlackTextObject{Type: "plain_text", Text: "Alert"},
		}},
	}
	ReportOnChainMonitorAndWarningMessageToSlack(monitorServer.URL, msg)

	monitorMsg := <-monitorMessages
	if strings.Contains(monitorMsg.Text, "<!channel>") {
		t.Fatalf("dedicated monitor message unexpectedly mentions channel: %#v", monitorMsg)
	}

	warningMsg := <-warningMessages
	if !strings.Contains(warningMsg.Text, "<!channel>") {
		t.Fatalf("warning message text does not mention channel: %#v", warningMsg)
	}
	if len(warningMsg.Blocks) != 2 || warningMsg.Blocks[1].Text == nil || warningMsg.Blocks[1].Text.Text != "<!channel>" {
		t.Fatalf("warning message blocks do not visibly mention channel: %#v", warningMsg.Blocks)
	}
}

func TestReportOnChainMonitorAndWarningDeduplicatesSameWebhook(t *testing.T) {
	messages := make(chan SlackMessage, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg SlackMessage
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			t.Errorf("decode Slack message: %v", err)
		}
		messages <- msg
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	previousConfigs := configs
	configs = &config.NetConfig{WarningWebhook: server.URL}
	t.Cleanup(func() { configs = previousConfigs })

	ReportOnChainMonitorAndWarningMessageToSlack(server.URL, SlackMessage{Text: "alert"})
	if len(messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(messages))
	}
	if msg := <-messages; !strings.Contains(msg.Text, "<!channel>") {
		t.Fatalf("deduplicated warning message does not mention channel: %#v", msg)
	}
}

func TestReportWarningMessageToSlackDoesNotAddChannelMention(t *testing.T) {
	messages := make(chan SlackMessage, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg SlackMessage
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			t.Errorf("decode Slack message: %v", err)
		}
		messages <- msg
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	previousConfigs := configs
	configs = &config.NetConfig{WarningWebhook: server.URL}
	t.Cleanup(func() { configs = previousConfigs })

	ReportWarningMessageToSlack(SlackMessage{Text: "simple internal transaction alert"})
	msg := <-messages
	if msg.Text != "simple internal transaction alert" {
		t.Fatalf("warning message = %q, want unchanged simple message", msg.Text)
	}
	if strings.Contains(msg.Text, "<!channel>") || len(msg.Blocks) != 0 {
		t.Fatalf("simple warning unexpectedly notifies channel: %#v", msg)
	}
}

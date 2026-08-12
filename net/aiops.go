package net

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

const defaultAIOpsEventEndpoint = "https://caweb.aiops.com/alert/api/event/"

var (
	aiopsEventEndpoint = defaultAIOpsEventEndpoint
	aiopsClient        = resty.New().
				SetTimeout(10 * time.Second).
				SetRetryCount(2).
				SetRetryWaitTime(500 * time.Millisecond).
				SetRetryMaxWaitTime(2 * time.Second)
)

type AIOpsAlert struct {
	EventID      string         `json:"eventId"`
	EventType    string         `json:"eventType"`
	AlarmName    string         `json:"alarmName"`
	AlarmContent string         `json:"alarmContent"`
	EntityName   string         `json:"entityName,omitempty"`
	EntityID     string         `json:"entityId,omitempty"`
	Priority     int            `json:"priority"`
	Service      string         `json:"service,omitempty"`
	Contexts     []AIOpsContext `json:"contexts,omitempty"`
}

type AIOpsContext struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Href string `json:"href,omitempty"`
}

type aiopsEvent struct {
	App string `json:"app"`
	AIOpsAlert
}

type aiopsResponse struct {
	Result  string `json:"result"`
	Message string `json:"message"`
	Code    any    `json:"code"`
}

// ReportAIOpsAlert sends the same event to every configured AIOps application.
// Phone calls are controlled by each application's notification policy.
func ReportAIOpsAlert(appKeys []string, alert AIOpsAlert) error {
	if len(appKeys) == 0 {
		return nil
	}

	var failures []string
	for i, appKey := range appKeys {
		appKey = strings.TrimSpace(appKey)
		if appKey == "" {
			continue
		}

		var result aiopsResponse
		resp, err := aiopsClient.R().
			SetHeader("Content-Type", "application/json").
			SetBody(aiopsEvent{App: appKey, AIOpsAlert: alert}).
			SetResult(&result).
			Post(aiopsEventEndpoint)
		if err != nil {
			failures = append(failures, fmt.Sprintf("app %d: %v", i+1, err))
			continue
		}
		if resp.IsError() {
			failures = append(failures, fmt.Sprintf("app %d: HTTP %d", i+1, resp.StatusCode()))
			continue
		}
		if !strings.EqualFold(result.Result, "success") {
			message := strings.TrimSpace(result.Message)
			if message == "" {
				message = "unknown response"
			}
			failures = append(failures, fmt.Sprintf("app %d: %s", i+1, message))
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("report AIOps alert: %s", strings.Join(failures, "; "))
	}
	return nil
}

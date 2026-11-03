package agent

import "testing"

func TestWithParamsWinOverRequestVars(t *testing.T) {
	vars := map[string]any{
		"ticketId": "TCK-FROM-REQUEST",
		"helpdesk.support_create_ticket.ticketId": "TCK-CREATED",
		"serviceName": "billing",
	}
	params := buildCallParams(vars, map[string]string{
		"ticketId": `{{ index . "helpdesk.support_create_ticket.ticketId" }}`,
		"reason":   "Service {{ .serviceName }} restarted",
	})
	if params["ticketId"] != "TCK-CREATED" {
		t.Fatalf("ticketId = %v, want the ticket the pipeline created", params["ticketId"])
	}
	if params["reason"] != "Service billing restarted" {
		t.Fatalf("reason = %v", params["reason"])
	}
	if params["serviceName"] != "billing" {
		t.Fatalf("vars without a with_param must pass through, got %v", params["serviceName"])
	}
}

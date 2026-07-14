package main

import (
	"log"
	"net/http"

	mockBanking "github.com/ccastromar/aos-agent-orchestration-system/internal/mocks/banking"
	mockCRM "github.com/ccastromar/aos-agent-orchestration-system/internal/mocks/crm"
	mockCustomerSupport "github.com/ccastromar/aos-agent-orchestration-system/internal/mocks/customer-support"
	mockDevops "github.com/ccastromar/aos-agent-orchestration-system/internal/mocks/devops"
	mockHelpdesk "github.com/ccastromar/aos-agent-orchestration-system/internal/mocks/helpdesk"
	mockHumanResources "github.com/ccastromar/aos-agent-orchestration-system/internal/mocks/human-resources"
	mockLogistics "github.com/ccastromar/aos-agent-orchestration-system/internal/mocks/logistics"
	mockOpenAPI "github.com/ccastromar/aos-agent-orchestration-system/internal/mocks/openapi"
)

var listenAndServe = http.ListenAndServe

func buildMux() *http.ServeMux {
	mux := http.NewServeMux()
	// register endpoints for each Domain
	mockBanking.RegisterHandlers(mux)
	mockDevops.RegisterHandlers(mux)
	mockCRM.RegisterHandlers(mux)
	mockHelpdesk.RegisterHandlers(mux)
	mockLogistics.RegisterHandlers(mux)
	mockHumanResources.RegisterHandlers(mux)
	mockCustomerSupport.RegisterHandlers(mux)
	mockOpenAPI.RegisterHandlers(mux)
	return mux
}

func main() {
	mux := buildMux()
	log.Println("[MOCK SERVER] listening on :9000")
	err := listenAndServe(":9000", mux)
	if err != nil {
		return
	}
}

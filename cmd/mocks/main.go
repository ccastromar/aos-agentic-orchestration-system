package main

import (
	"log"
	"net/http"
	"os"

	mockBanking "github.com/ccastromar/aos-agentic-orchestration-system/mocks/banking"
	mockCRM "github.com/ccastromar/aos-agentic-orchestration-system/mocks/crm"
	mockCustomerSupport "github.com/ccastromar/aos-agentic-orchestration-system/mocks/customer-support"
	mockDevops "github.com/ccastromar/aos-agentic-orchestration-system/mocks/devops"
	mockHelpdesk "github.com/ccastromar/aos-agentic-orchestration-system/mocks/helpdesk"
	mockHumanResources "github.com/ccastromar/aos-agentic-orchestration-system/mocks/human-resources"
	mockLogistics "github.com/ccastromar/aos-agentic-orchestration-system/mocks/logistics"
	mockOpenAPI "github.com/ccastromar/aos-agentic-orchestration-system/mocks/openapi"
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
	port := os.Getenv("AOS_MOCK_PORT")
	if port == "" {
		port = "9000"
	}
	mux := buildMux()
	log.Printf("[MOCK SERVER] listening on :%s", port)
	err := listenAndServe(":"+port, mux)
	if err != nil {
		return
	}
}

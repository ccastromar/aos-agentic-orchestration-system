package main

import (
	"log"
	"net/http"
	"os"

	mockA2A "github.com/ccastromar/aos-agentic-orchestration-system/mocks/a2a"
	mockBanking "github.com/ccastromar/aos-agentic-orchestration-system/mocks/banking"
	mockBlockchain "github.com/ccastromar/aos-agentic-orchestration-system/mocks/blockchain"
	mockCRM "github.com/ccastromar/aos-agentic-orchestration-system/mocks/crm"
	mockCustody "github.com/ccastromar/aos-agentic-orchestration-system/mocks/custody"
	mockCustomerSupport "github.com/ccastromar/aos-agentic-orchestration-system/mocks/customer-support"
	mockDevops "github.com/ccastromar/aos-agentic-orchestration-system/mocks/devops"
	mockGovernanceDemo "github.com/ccastromar/aos-agentic-orchestration-system/mocks/governance-demo"
	mockHelpdesk "github.com/ccastromar/aos-agentic-orchestration-system/mocks/helpdesk"
	mockHumanResources "github.com/ccastromar/aos-agentic-orchestration-system/mocks/human-resources"
	mockLogistics "github.com/ccastromar/aos-agentic-orchestration-system/mocks/logistics"
	mockMCP "github.com/ccastromar/aos-agentic-orchestration-system/mocks/mcp"
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
	mockMCP.RegisterHandlers(mux)
	mockA2A.RegisterHandlers(mux)
	mockCustody.RegisterHandlers(mux)
	mockBlockchain.RegisterHandlers(mux)
	mockGovernanceDemo.RegisterHandlers(mux)
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

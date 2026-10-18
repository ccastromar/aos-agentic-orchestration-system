package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/app"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/audit"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/logx"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/tools"
	"github.com/joho/godotenv"
)

// runner is the minimal interface our app must satisfy for running.
type runner interface{ Run(context.Context) error }

// appCtor is a constructor indirection to enable testing without launching the real app.
var appCtor = func() (runner, error) { return app.New() }

// fatalf indirection allows testing fatal paths without exiting the test process.
var fatalf = log.Fatalf

func run(ctx context.Context) {
	a, err := appCtor()
	if err != nil {
		fatalf("error initializing app: %v", err)
		return
	}
	if err := a.Run(ctx); err != nil {
		fatalf("error running app: %v", err)
		return
	}
}

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// load .env always if exists
	_ = godotenv.Load()

	// CLI flags
	port := flag.String("port", "", "HTTP port to listen on")
	vFlag := flag.Bool("version", false, "Print version information")
	verifyAudit := flag.String("verify-audit", "", "Verify a JSONL audit chain and exit")
	verifyTrace := flag.String("verify-trace", "", "Verify one execution's pipeline manifest and step evidence (requires -verify-audit)")
	flag.Parse()

	if *vFlag {
		fmt.Printf("AOS Engine v%s\nCommit: %s\nBuilt: %s\n", version, commit, date)
		os.Exit(0)
	}
	if *verifyAudit != "" {
		if *verifyTrace != "" {
			verification, err := audit.VerifyExecutionFile(*verifyAudit, *verifyTrace)
			if err != nil {
				log.Fatalf("execution verification failed: %v", err)
			}
			_ = json.NewEncoder(os.Stdout).Encode(verification)
			return
		}
		verification, err := audit.VerifyFile(*verifyAudit)
		if err != nil {
			log.Fatalf("audit verification failed: %v", err)
		}
		_ = json.NewEncoder(os.Stdout).Encode(verification)
		return
	}
	if *verifyTrace != "" {
		log.Fatal("-verify-trace requires -verify-audit <journal.jsonl>")
	}

	env, err := config.LoadEnv()
	if err != nil {
		log.Fatalf("Error loading environment variables: %v", err)
	}

	// apply runtime options: CLI flag > env var > default (internal)
	if *port != "" {
		app.SetHTTPPort(*port)
	} else if env.Port != 0 {
		app.SetHTTPPort(fmt.Sprintf("%d", env.Port))
	}

	if os.Getenv("ALLOW_LOCAL_TOOLS") == "true" {
		logx.Warn("App", "ALLOW_LOCAL_TOOLS is true. SSRF protection is disabled!")
		tools.SkipSSRF = true
	} else if raw := strings.TrimSpace(os.Getenv("AOS_TOOL_INTERNAL_HOSTS")); raw != "" {
		// Narrow exemption: only these tool hosts may resolve to internal IPs.
		hosts := strings.Split(raw, ",")
		tools.SetInternalHosts(hosts)
		logx.Info("App", "SSRF protection on; internal tool hosts allowed: %s", raw)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	run(ctx)
}

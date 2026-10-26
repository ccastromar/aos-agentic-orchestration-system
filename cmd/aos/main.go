package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/auth"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/app"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/audit"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/logx"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/telemetry"
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

// deploymentEnvironment is deployment.environment.name: the one OpenTelemetry is given
// (OTEL_RESOURCE_ATTRIBUTES), so logs and traces agree, or APP_ENV.
func deploymentEnvironment(appEnv string) string {
	for _, pair := range strings.Split(os.Getenv("OTEL_RESOURCE_ATTRIBUTES"), ",") {
		if key, value, ok := strings.Cut(strings.TrimSpace(pair), "="); ok && key == "deployment.environment.name" {
			return value
		}
	}
	return appEnv
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
	logx.Configure(logx.Config{
		Format:      os.Getenv("LOG_FORMAT"),
		Level:       env.LogLevel,
		Development: auth.IsDevelopmentEnvironment(env.AppEnv),
		Service:     os.Getenv("OTEL_SERVICE_NAME"),
		Version:     version,
		Environment: deploymentEnvironment(env.AppEnv),
	})

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
	shutdownTracing, err := telemetry.Setup(ctx, version)
	if err != nil {
		log.Fatalf("Error starting OpenTelemetry: %v", err)
	}
	if telemetry.Enabled() {
		logx.Info("App", "OpenTelemetry tracing on (OTLP)")
	}
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(flushCtx)
	}()
	run(ctx)
}

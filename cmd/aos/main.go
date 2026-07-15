package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/app"
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

func main() {
	// load .env always if exists
	_ = godotenv.Load()

	// CLI flags
	port := flag.String("port", "", "HTTP port to listen on")
	flag.Parse()

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
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	run(ctx)
}

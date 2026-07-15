package guard

import (
    "testing"

    "github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
)

func TestValidateIntentPermissions_DangerousNotAllowed(t *testing.T) {
    tools := map[string]config.Tool{
        "safe":      {Name: "safe", Mode: "read"},
        "dangerous": {Name: "dangerous", Mode: "dangerous"},
    }
    pipeline := config.Pipeline{Steps: []config.PipelineStep{{Tool: "safe"}, {Tool: "dangerous"}}}
    intent := config.Intent{Type: "x", AllowDangerous: false}

    if err := ValidateIntentPermissions(intent, pipeline, tools); err == nil {
        t.Fatalf("expected error when dangerous tool present but intent doesn't allow")
    }
}

func TestValidateIntentPermissions_DangerousAllowed(t *testing.T) {
    tools := map[string]config.Tool{
        "dangerous": {Name: "dangerous", Mode: "dangerous"},
    }
    pipeline := config.Pipeline{Steps: []config.PipelineStep{{Tool: "dangerous"}}}
    intent := config.Intent{Type: "x", AllowDangerous: true}

    if err := ValidateIntentPermissions(intent, pipeline, tools); err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
}

func TestValidateDangerousParams(t *testing.T) {
    t.Run("no check when not dangerous", func(t *testing.T) {
        intent := config.Intent{AllowDangerous: false}
        if err := ValidateDangerousParams(intent, map[string]string{}); err != nil {
            t.Fatalf("unexpected: %v", err)
        }
    })

    t.Run("missing amount", func(t *testing.T) {
        intent := config.Intent{AllowDangerous: true, RequiresAmount: true}
        if err := ValidateDangerousParams(intent, map[string]string{}); err == nil {
            t.Fatalf("expected error for missing amount")
        }
    })

    t.Run("invalid amount", func(t *testing.T) {
        intent := config.Intent{AllowDangerous: true, RequiresAmount: true}
        if err := ValidateDangerousParams(intent, map[string]string{"amount": "abc"}); err == nil {
            t.Fatalf("expected error for invalid amount")
        }
    })

    t.Run("amount over max", func(t *testing.T) {
        intent := config.Intent{AllowDangerous: true, RequiresAmount: true, MaxAmount: 10}
        if err := ValidateDangerousParams(intent, map[string]string{"amount": "11"}); err == nil {
            t.Fatalf("expected error for amount over max")
        }
    })

    t.Run("missing phone", func(t *testing.T) {
        intent := config.Intent{AllowDangerous: true, RequiresPhone: true}
        if err := ValidateDangerousParams(intent, map[string]string{}); err == nil {
            t.Fatalf("expected error for missing phone")
        }
    })

    t.Run("invalid phone", func(t *testing.T) {
        intent := config.Intent{AllowDangerous: true, RequiresPhone: true}
        if err := ValidateDangerousParams(intent, map[string]string{"toPhone": "abc"}); err == nil {
            t.Fatalf("expected error for invalid phone")
        }
    })

    t.Run("happy path", func(t *testing.T) {
        intent := config.Intent{AllowDangerous: true, RequiresAmount: true, RequiresPhone: true, MaxAmount: 100}
        params := map[string]string{"amount": "42.5", "toPhone": "+34123456789"}
        if err := ValidateDangerousParams(intent, params); err != nil {
            t.Fatalf("unexpected: %v", err)
        }
    })
}

func TestValidateDangerousChain(t *testing.T) {
    tools := map[string]config.Tool{
        "D": {Name: "D", Mode: "dangerous"},
        "R": {Name: "R", Mode: "read"},
    }
    t.Run("two dangerous in a row fails", func(t *testing.T) {
        p := config.Pipeline{Name: "p", Steps: []config.PipelineStep{{Tool: "D"}, {Tool: "D"}}}
        if err := ValidateDangerousChain(p, tools); err == nil {
            t.Fatalf("expected error on chained dangerous tools")
        }
    })
    t.Run("dangerous then safe passes", func(t *testing.T) {
        p := config.Pipeline{Name: "p", Steps: []config.PipelineStep{{Tool: "D"}, {Tool: "R"}}}
        if err := ValidateDangerousChain(p, tools); err != nil {
            t.Fatalf("unexpected: %v", err)
        }
    })
    t.Run("steps without tool are ignored", func(t *testing.T) {
        p := config.Pipeline{Name: "p", Steps: []config.PipelineStep{{Tool: ""}, {Tool: "D"}, {Tool: ""}}}
        if err := ValidateDangerousChain(p, tools); err != nil {
            t.Fatalf("unexpected: %v", err)
        }
    })
}

func TestValidateAll(t *testing.T) {
    tools := map[string]config.Tool{
        "aml":  {Name: "aml", Mode: "read"},
        "send": {Name: "send", Mode: "dangerous"},
    }
    pipeline := config.Pipeline{Name: "bizum", Steps: []config.PipelineStep{{Tool: "aml"}, {Tool: "send"}}}
    intent := config.Intent{Type: "bizum", AllowDangerous: true, RequiresAmount: true, RequiresPhone: true, MaxAmount: 100}
    params := map[string]string{"amount": "50", "toPhone": "+34123456789"}

    if err := ValidateAll(intent, pipeline, params, tools); err != nil {
        t.Fatalf("unexpected: %v", err)
    }

    // Failing case: over max triggers param validation error
    params["amount"] = "1000"
    if err := ValidateAll(intent, pipeline, params, tools); err == nil {
        t.Fatalf("expected error when amount exceeds max")
    }
}

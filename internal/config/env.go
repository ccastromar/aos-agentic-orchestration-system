package config

import (
	"fmt"
	"os"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type EnvVars struct {
	AppEnv       string        `envconfig:"APP_ENV" default:"dev"`
	Port         int           `envconfig:"PORT" default:"8080"`
	ReadTimeout  time.Duration `envconfig:"READ_TIMEOUT" default:"5s"`
	WriteTimeout time.Duration `envconfig:"WRITE_TIMEOUT" default:"5s"`

	BusWorkers int `envconfig:"BUS_WORKERS" default:"4"`
	BusBuffer  int `envconfig:"BUS_BUFFER"  default:"100"`

	LLMBaseURL string `envconfig:"LLM_BASE_URL" default:"https://api.openai.com/v1"`
	LLMEngine  string `envconfig:"LLM_ENGINE" default:"gpt-4.1"`

	LLMModel   string        `envconfig:"LLM_MODEL" default:"gpt-4.1"`
	LLMTimeout time.Duration `envconfig:"LLM_TIMEOUT" default:"10s"`

	// Ollama (local LLM) configuration
	OllamaBaseURL    string `envconfig:"OLLAMA_BASE_URL" default:"http://localhost:11434"`
	OllamaModel      string `envconfig:"OLLAMA_MODEL" default:"qwen3:0.6b"`
	OllamaEmbedModel string `envconfig:"OLLAMA_EMBED_MODEL" default:"nomic-embed-text"`

	RedisAddr     string `envconfig:"REDIS_ADDR"`
	RedisPassword string `envconfig:"REDIS_PASSWORD"`
	
	QdrantURL     string `envconfig:"QDRANT_URL" default:"http://localhost:6333"`

	LogLevel string `envconfig:"LOG_LEVEL" default:"info"`
	// ---------- AUTH / IDENTITY CONFIG (NEW) ----------
	JWTIssuer   string `envconfig:"AOS_JWT_ISSUER" default:""`
	JWTAudience string `envconfig:"AOS_JWT_AUDIENCE" default:""`
	JWKURL      string `envconfig:"AOS_JWK_URL" default:""`
	IAMURL      string `envconfig:"AOS_IAM_URL" default:"disabled"`
}

func LoadEnv() (*EnvVars, error) {
	var v EnvVars

	if err := envconfig.Process("", &v); err != nil {
		// for tests
		if os.Getenv("APP_ENV") == "test" {
			return &v, nil
		}
		return nil, fmt.Errorf("error loading env: %w", err)
	}

	return &v, nil
}

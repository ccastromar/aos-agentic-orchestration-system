package logx

import (
	"fmt"
	"log"
	"os"
	"time"
)

const (
	Reset = "\033[0m"

	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
)

// colores por nivel
var levelColor = map[string]string{
	"DEBUG": Cyan,
	"INFO":  Blue,
	"WARN":  Yellow,
	"ERROR": Red,
}

// colores por agente (opcional, ajusta como quieras)
var agentColor = map[string]string{
	"Api":       Cyan,
	"Planner":   Blue,
	"Inspector": Magenta,
	"Verifier":  Green,
	"Analyst":   Yellow,
	"Mock":      Cyan,
	"HTTP":      Blue,
	"Config":    Magenta,
	"App":       Green,
}

// detecta color mode
func useColor() bool {
	return os.Getenv("APP_ENV") == "local" || os.Getenv("APP_ENV") == "dev"
}

// --- Public API ---

func Debug(agent, msg string, args ...any) {
	logGeneric("DEBUG", agent, msg, args...)
}

func Info(agent, msg string, args ...any) {
	logGeneric("INFO", agent, msg, args...)
}

func Warn(agent, msg string, args ...any) {
	logGeneric("WARN", agent, msg, args...)
}

func Error(agent, msg string, args ...any) {
	logGeneric("ERROR", agent, msg, args...)
}

// --- Core ---

func logGeneric(level, agent, msg string, args ...any) {
	//t := time.Now().Format("15:04:05.000")
	full := fmt.Sprintf(msg, args...)

	if useColor() {
		lc := levelColor[level]
		ac := agentColor[agent]
		log.Printf("%s[%s]%s %s[%s]%s %s",
			lc, level, Reset,
			ac, agent, Reset,
			full,
		)
	} else {
		log.Printf("[%s] [%s] %s", level, agent, full)
	}
}

func L(id, agent, msg string, args ...any) {
	prefix := fmt.Sprintf("[%s][%s][%s] ",
		time.Now().Format(time.RFC3339),
		agent,
		id,
	)
	log.Printf(prefix+msg, args...)
}

// Versión sin ID (para logs globales de arranque)
func G(agent, msg string, args ...any) {
	prefix := fmt.Sprintf("[%s][%s] ",
		time.Now().Format(time.RFC3339),
		agent,
	)
	log.Printf(prefix+msg, args...)
}

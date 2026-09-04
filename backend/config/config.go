package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config — весь конфиг через переменные окружения с дефолтами.
type Config struct {
	Addr           string
	DataDir        string
	StaticDir      string
	AiderBin       string
	PythonBin      string
	RunnerPath     string
	AllowCORS      []string
	TrustedProxies []string
	LogTail        int
}

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func getint(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getlist(key string) []string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func Load() (Config, error) {
	cfg := Config{
		Addr:           getenv("ADDR", ":8080"),
		DataDir:        getenv("DATA_DIR", "data"),
		StaticDir:      getenv("STATIC_DIR", "../frontend/dist"),
		AiderBin:       getenv("AIDER_BIN", "aider"),
		PythonBin:      getenv("PYTHON_BIN", "python"),
		RunnerPath:     getenv("RUNNER_PATH", "../runner/crew_run.py"),
		AllowCORS:      getlist("ALLOW_CORS"),
		TrustedProxies: getlist("TRUSTED_PROXIES"),
		LogTail:        getint("LOG_TAIL", 1000),
	}
	if cfg.LogTail < 50 {
		return cfg, fmt.Errorf("LOG_TAIL must be >= 50, got %d", cfg.LogTail)
	}
	if !strings.HasPrefix(cfg.Addr, ":") && !strings.Contains(cfg.Addr, ":") {
		return cfg, fmt.Errorf("invalid ADDR %q", cfg.Addr)
	}
	return cfg, nil
}

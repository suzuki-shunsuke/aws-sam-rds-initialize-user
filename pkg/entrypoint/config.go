package entrypoint

import (
	"os"
	"strings"
)

type Config struct {
	EventFilter string
	SQL         string
	Passwords   []string
	Secrets     string
}

func (ep Entrypoint) readConfig() Config {
	cfg := Config{
		EventFilter: os.Getenv("EVENT_FILTER"),
	}
	if s := os.Getenv("PASSWORDS"); s != "" {
		cfg.Passwords = strings.Split(s, " ")
	}

	return cfg
}

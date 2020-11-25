package entrypoint

import (
	"os"
	"strings"
)

type Config struct {
	EventFilter string
	SQL         string
	Users       []string
	Secrets     string
}

func (ep Entrypoint) readConfig() Config {
	cfg := Config{
		EventFilter: os.Getenv("EVENT_FILTER"),
		SQL:         os.Getenv("SQL"),
		Secrets:     os.Getenv("SECRETS"),
	}
	if s := os.Getenv("USERS"); s != "" {
		cfg.Users = strings.Split(s, " ")
	}

	return cfg
}

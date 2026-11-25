package app

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func samplePoolConfig(template, database string) (*pgxpool.Config, error) {
	if !validCorpusURL(template) || !regexp.MustCompile(`^corpus_vn_[0-9]{8}(_[0-9]+)?$`).MatchString(database) {
		return nil, errors.New("invalid sample corpus configuration")
	}
	cfg, err := pgxpool.ParseConfig(strings.Replace(template, "{database}", database, 1))
	if err != nil || cfg.ConnConfig.Database != database {
		return nil, errors.New("invalid sample corpus configuration")
	}
	cfg.MaxConns, cfg.MinConns, cfg.MinIdleConns = 1, 0, 0
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	if cfg.ConnConfig.ConnectTimeout <= 0 || cfg.ConnConfig.ConnectTimeout > 2*time.Second {
		cfg.ConnConfig.ConnectTimeout = 2 * time.Second
	}
	return cfg, nil
}

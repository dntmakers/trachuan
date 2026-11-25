package app

import "testing"

func TestSamplePoolConfig_ReadOnlySingleConnection(t *testing.T) {
	cfg, err := samplePoolConfig("postgres://localhost/{database}", "corpus_vn_20990101")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxConns != 1 || cfg.MinConns != 0 || cfg.MinIdleConns != 0 || cfg.ConnConfig.Database != "corpus_vn_20990101" || cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] != "on" {
		t.Fatalf("unexpected pool configuration: %+v", cfg)
	}
	for _, template := range []string{"postgres://localhost/postgres", "postgres://localhost/{database}?dbname=other", "postgres://localhost/{database}/{database}"} {
		if _, err := samplePoolConfig(template, "corpus_vn_20990101"); err == nil {
			t.Fatalf("accepted %q", template)
		}
	}
}

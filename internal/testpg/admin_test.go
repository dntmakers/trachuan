package testpg

import (
	"context"
	"strings"
	"testing"
)

func TestOpenPruneAdministrator_RejectsNonLoopback(t *testing.T) {
	t.Setenv("CORPUS_TEST_ADMIN_URL", "postgres://test@192.0.2.1/postgres")
	conn, err := OpenPruneAdministrator(context.Background())
	if conn != nil {
		defer func() { _ = conn.Close(context.Background()) }()
	}
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("error = %v, want loopback refusal", err)
	}
}

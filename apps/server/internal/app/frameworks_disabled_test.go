package app

import (
	"testing"

	"danny.vn/trachuan/apps/server/internal/mcpapi"
	"danny.vn/trachuan/apps/server/internal/store"
)

func TestConfigureFrameworks_DisabledKeepsMCPAdmission(t *testing.T) {
	var tools mcpapi.Config
	closeFrameworks := configureFrameworks(t.Context(), config{frameworksMode: "false"}, nil, store.New(nil), nil, &tools)
	defer closeFrameworks()
	if tools.Frameworks || tools.FrameworkEntitled == nil {
		t.Fatal("disabled mode lacks its bounded entitlement admission")
	}
}

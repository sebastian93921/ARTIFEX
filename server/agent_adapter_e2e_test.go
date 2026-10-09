package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestAgentAdapterE2E exercises the stdio MCP process and Pi extension against
// the real HTTP handler and PostgreSQL. Opt-in because it needs npm ci in adapters/agent.
func TestAgentAdapterE2E(t *testing.T) {
	if os.Getenv("ARTIFEX_ADAPTER_E2E") != "1" {
		t.Skip("opt in with ARTIFEX_ADAPTER_E2E=1 and a disposable artifex_adapter_* database")
	}
	// Fail closed: never initialize/migrate the caller's normal application database.
	config, err := pgx.ParseConfig(os.Getenv("ARTIFEX_PG_DSN"))
	if err != nil || !strings.HasPrefix(config.Database, "artifex_adapter_") {
		t.Fatal("ARTIFEX_PG_DSN must explicitly select a disposable artifex_adapter_* database")
	}
	for _, key := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "ARTIFEX_LLM_PROVIDER"} {
		t.Setenv(key, "")
	}
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	var database string
	if err := m.pg.QueryRow("SELECT current_database()").Scan(&database); err != nil || !strings.HasPrefix(database, "artifex_adapter_") {
		t.Fatalf("expected isolated database, got %q: %v", database, err)
	}
	// Only a fresh disposable fixture is accepted; no stored providers or external MCP calls.
	var profiles int
	if err := m.pg.QueryRow("SELECT count(*) FROM llm_profiles").Scan(&profiles); err != nil || profiles != 0 {
		t.Fatalf("fixture must have no LLM profiles: count=%d err=%v", profiles, err)
	}
	if _, err := m.pg.Exec("UPDATE mcp_servers SET enabled=false"); err != nil {
		t.Fatal(err)
	}
	findingID, err := m.pg.AddFinding(0, 0, "fixture", "adapter fixture", "high", "SIMULATED adapter finding", "SIMULATED local evidence; no target was tested.", "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s := New(ctx, m, dir, dir, dir)
	httpServer := httptest.NewServer(s.Handler())
	defer httpServer.Close()
	const password = "artifex-adapter-disposable-test"
	response, err := http.Post(httpServer.URL+"/api/auth/init", "application/json", strings.NewReader(`{"password":"`+password+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("fixture initialization: HTTP %d", response.StatusCode)
	}
	commandCtx, stop := context.WithTimeout(ctx, 60*time.Second)
	defer stop()
	command := exec.CommandContext(commandCtx, "node", "--test", "../adapters/agent/test/e2e.test.js")
	command.Env = append(os.Environ(), "ARTIFEX_E2E_URL="+httpServer.URL,
		"ARTIFEX_E2E_PASSWORD="+password, "ARTIFEX_E2E_FINDING_ID="+strconv.FormatInt(findingID, 10))
	output, err := command.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatalf("adapter E2E failed: %v", err)
	}
}

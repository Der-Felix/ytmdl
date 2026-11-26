package database

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestApplyPoolOptionsTurnsJITOff pins that the application's sessions run
// without PostgreSQL's JIT compiler unless the connection URL asks for it.
func TestApplyPoolOptionsTurnsJITOff(t *testing.T) {
	cfg, err := pgxpool.ParseConfig("postgres://u:p@localhost:1/db")
	if err != nil {
		t.Fatal(err)
	}
	applyPoolOptions(cfg, Options{})
	if got := cfg.ConnConfig.RuntimeParams["jit"]; got != "off" {
		t.Fatalf("jit = %q, want off", got)
	}
	cfg, err = pgxpool.ParseConfig("postgres://u:p@localhost:1/db?jit=on")
	if err != nil {
		t.Fatal(err)
	}
	applyPoolOptions(cfg, Options{})
	if got := cfg.ConnConfig.RuntimeParams["jit"]; got != "on" {
		t.Fatalf("jit = %q, want the URL's on", got)
	}
}

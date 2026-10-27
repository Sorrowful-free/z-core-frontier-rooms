package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/loadgen"
)

// loadgen — нагрузочный клиент флота комнат (план docs/plans/P27-loadgen.md).
// Сервер — внешний адрес (LOADGEN_HTTP_ADDR/LOADGEN_WS_ADDR/LOADGEN_ADMIN_ADDR);
// оркестрация (подъём rooms, env-пресет, снимки /metrics) — scripts/loadtest-run.sh.
//
// SIGINT/SIGTERM → graceful: ramp/sustain прерываются, drain + report всё
// равно отрабатывают (partial report, interrupted=true).
func main() {
	log.SetPrefix("loadgen: ")
	cfg, err := loadgen.LoadConfigFromEnv()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fleet := loadgen.NewFleet(cfg, os.Stdout)
	rep := fleet.Run(ctx)

	if err := rep.Write(cfg.ReportPath); err != nil {
		log.Fatalf("write report %s: %v", cfg.ReportPath, err)
	}
	log.Printf("report written to %s (drain_complete=%t, join_success=%d)",
		cfg.ReportPath, rep.DrainComplete, rep.JoinSuccess)
}

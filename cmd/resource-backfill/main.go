package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"tron-tracker/config"
	"tron-tracker/database"
	trackerlog "tron-tracker/log"
	"tron-tracker/net"
)

// Usage:
//
//	go run ./cmd/resource-backfill -start 260201 -days 181
//	go run ./cmd/resource-backfill -start 260201 -days 181 -config /data/tracker/config.toml
//
// Copies finalized stake rows and the top 1000 delegate rows per date/type into
// the compact read model used by /top_delegate and /top_stake. Safe to stop and
// rerun: each date is rebuilt atomically.
func main() {
	startFlag := flag.String("start", "", "first date to backfill, YYMMDD")
	daysFlag := flag.Int("days", 1, "number of consecutive finalized days")
	configFlag := flag.String("config", "./config.toml", "path to config.toml")
	flag.Parse()

	start, err := time.ParseInLocation("060102", *startFlag, time.Local)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid -start %q (want YYMMDD): %v\n", *startFlag, err)
		os.Exit(2)
	}
	if *daysFlag < 1 {
		fmt.Fprintln(os.Stderr, "-days must be positive")
		os.Exit(2)
	}

	cfg := config.LoadConfigFrom(*configFlag)
	net.Init(&cfg.Net)
	trackerlog.Init(&cfg.Log)

	db := database.New(&cfg.DB)
	if err := db.BackfillResourceTransactions(start, *daysFlag); err != nil {
		fmt.Fprintf(os.Stderr, "resource backfill failed: %v\n", err)
		os.Exit(1)
	}
}

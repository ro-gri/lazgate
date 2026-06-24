package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	hysteriaagent "laz/internal/agent/hysteria2"
	hysteriaconfig "laz/internal/legacy/agent/hysteria2/config"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "/etc/lazgate-agent/config.yaml", "path to lazgate-agent config")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	cfg, err := hysteriaconfig.LoadFile(*configPath)
	if err != nil {
		log.Fatalf("component=hysteria-agent event=config_load status=error error=%q", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := hysteriaagent.Run(ctx, cfg, version); err != nil {
		log.Fatalf("component=hysteria-agent event=run status=error node_id=%q error=%q", cfg.NodeID, err)
	}
}

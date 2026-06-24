package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	amneziaagent "laz/internal/agent/amneziawg"
	amneziaconfig "laz/internal/legacy/agent/amneziawg/config"
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
	cfg, err := amneziaconfig.LoadFile(*configPath)
	if err != nil {
		log.Fatalf("component=amnezia-agent event=config_load status=error error=%q", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := amneziaagent.Run(ctx, cfg, version); err != nil {
		log.Fatalf("component=amnezia-agent event=run status=error node_id=%q error=%q", cfg.NodeID, err)
	}
}

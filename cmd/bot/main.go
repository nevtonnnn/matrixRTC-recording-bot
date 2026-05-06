package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/vlad/matrix-recording-bot/internal/config"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
		os.Exit(1)
	}

	fmt.Printf("Config loaded: homeserver=%s, user=%s\n", cfg.Matrix.Homeserver, cfg.Matrix.UserID)
}

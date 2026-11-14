package main

import (
	"flag"
	"log"
	"net/http"

	"littleprinter/internal/api"
	"littleprinter/internal/bridge"
	"littleprinter/internal/config"
)

func main() {
	configPath := flag.String("config", "config.json", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}

	hub := bridge.NewHub(cfg)
	srv := api.New(cfg, hub)

	log.Printf("littleprinter: listening on %s (claim code configured, waiting for bridge)", cfg.Listen)
	log.Fatal(http.ListenAndServe(cfg.Listen, srv.Routes()))
}

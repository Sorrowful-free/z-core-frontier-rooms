package main

import (
	"log"
	"os"
)

func main() {
	log.Println("z-core-frontier-rooms: starting")
	if err := run(); err != nil {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}

func run() error {
	// TODO: wire HTTP/WS/ENet listeners and room use cases.
	return nil
}

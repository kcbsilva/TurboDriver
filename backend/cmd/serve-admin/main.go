package main

import (
	"flag"
	"log"
	"net/http"
	"os"
)

func main() {
	root := flag.String("root", "./static/admin", "path to admin static files")
	addr := flag.String("addr", ":8090", "listen address")
	flag.Parse()

	if _, err := os.Stat(*root); err != nil {
		log.Fatalf("admin root missing: %v", err)
	}

	fs := http.FileServer(http.Dir(*root))
	http.Handle("/", fs)
	log.Printf("serving admin UI from %s on %s", *root, *addr)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

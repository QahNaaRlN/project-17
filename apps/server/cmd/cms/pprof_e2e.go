//go:build e2e

package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
)

// The load bench profiles the §2 stand's CMS while NFR-005 is measured
// (docs/load-test-report.md). CMS_E2E_PPROF_ADDR opens net/http/pprof on a
// loopback address for `cms serve`. Product builds do not compile this file.
func init() {
	addr := os.Getenv("CMS_E2E_PPROF_ADDR")
	if addr == "" || len(os.Args) < 2 || os.Args[1] != "serve" {
		return
	}
	if host, _, err := net.SplitHostPort(addr); err != nil || host != "127.0.0.1" {
		panic(fmt.Sprintf("CMS_E2E_PPROF_ADDR must be 127.0.0.1:<port>, got %q", addr))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		panic(err)
	}
	go func() { _ = http.Serve(ln, mux) }()
}

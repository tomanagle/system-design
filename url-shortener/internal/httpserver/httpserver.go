package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
)

func MustHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		panic(fmt.Errorf("get hostname: %w", err))
	}
	return hostname
}

// MustListen binds before startup is announced, so a port conflict fails startup.
func MustListen(addr string) net.Listener {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		panic(fmt.Errorf("listen HTTP: %w", err))
	}
	return listener
}

func MustServe(server *http.Server, listener net.Listener) {
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		panic(fmt.Errorf("serve HTTP: %w", err))
	}
}

func MustShutdown(ctx context.Context, server *http.Server) {
	if err := server.Shutdown(ctx); err != nil {
		_ = server.Close()
		panic(fmt.Errorf("shutdown HTTP: %w", err))
	}
}

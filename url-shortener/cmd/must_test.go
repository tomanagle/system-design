package main

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestMustListenPanicsWhenPortIsOccupied(t *testing.T) {
	listener := MustListen("127.0.0.1:0")
	defer listener.Close()
	defer func() {
		if recover() == nil {
			t.Error("expected a panic for an occupied port")
		}
	}()
	other := MustListen(listener.Addr().String())
	defer other.Close()
}

func TestMustServeShutsDownNormally(t *testing.T) {
	listener := MustListen("127.0.0.1:0")
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})}
	defer server.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		MustServe(server, listener)
	}()
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", response.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	MustShutdown(ctx, server)
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("server did not stop")
	}
}

func TestMustServePanicsOnUnexpectedFailure(t *testing.T) {
	listener := MustListen("127.0.0.1:0")
	listener.Close()
	defer func() {
		if recover() == nil {
			t.Error("expected a panic when serving a closed listener")
		}
	}()
	MustServe(&http.Server{}, listener)
}

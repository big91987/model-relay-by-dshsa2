// Command server runs the model gateway.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/big91987/model-relay-by-dshsa2/internal/api"
	"github.com/big91987/model-relay-by-dshsa2/internal/config"
	"github.com/big91987/model-relay-by-dshsa2/internal/store"
)

func main() {
	cfg := config.Load()
	st, err := store.Open(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	server, err := api.New(cfg, st)
	if err != nil {
		log.Fatalf("initialize server: %v", err)
	}

	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Printf("model gateway listening on http://localhost:%s (db=%s, web=%s)", cfg.Port, cfg.DatabasePath, cfg.WebDist)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

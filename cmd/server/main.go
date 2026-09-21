// Command server runs the local offline paleomagnetic workbench.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"polaritytrace/internal/api"
	"polaritytrace/internal/ingest"
	"polaritytrace/internal/store"
	"polaritytrace/internal/webapp"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:5500", "listen address")
	dbPath := flag.String("db", "polaritytrace.db", "SQLite database path")
	noSeed := flag.Bool("no-seed", false, "do not auto-import fixed fixtures into an empty database")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, *dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer st.Close()

	svc := ingest.New(st)
	if !*noSeed {
		n, err := svc.CountSpecimens(ctx)
		if err != nil {
			log.Fatalf("count specimens: %v", err)
		}
		if n == 0 {
			imported, err := svc.SeedFixtures(ctx)
			if err != nil {
				log.Fatalf("seed fixtures: %v", err)
			}
			log.Printf("empty database: imported %d fixed acceptance specimens", imported)
		}
	}

	srv := &http.Server{
		Addr:              *listen,
		Handler:           api.New(svc, webapp.Static()).Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Printf("极性轨迹 workbench listening on http://%s", *listen)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		log.Printf("shutdown: %v", err)
	}
	_ = os.Stdout.Sync()
}

// Command server runs the local paleomagnetic demagnetization workbench.
//
//	go run ./cmd/server --listen 127.0.0.1:5500
package main

import (
	"context"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"paleobench/internal/store"
	"paleobench/internal/web"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:5500", "listen address")
	dbPath := flag.String("db", "paleobench.db", "SQLite database path")
	autoImport := flag.Bool("auto-import", true, "import fixed fixtures on first start when empty")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	st, err := store.Open(ctx, *dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer st.Close()

	svc := web.NewService(st)
	p, err := svc.EnsureProject(ctx)
	if err != nil {
		log.Fatalf("ensure project: %v", err)
	}
	if *autoImport {
		specs, err := st.ListSpecimens(ctx, p.ID)
		if err != nil {
			log.Fatalf("list specimens: %v", err)
		}
		if len(specs) == 0 {
			n, err := svc.ImportFixtures(ctx)
			if err != nil {
				log.Fatalf("auto import: %v", err)
			}
			log.Printf("imported %d fixture levels", n)
		}
	}

	sub, err := fs.Sub(web.Assets, "webroot")
	if err != nil {
		log.Fatal(err)
	}
	srv := &web.Server{Svc: svc, Web: sub}
	mux := srv.NewMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(sub))))

	httpSrv := &http.Server{
		Addr:              *listen,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Printf("极性轨迹 workbench listening on http://%s", *listen)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	<-ctx.Done()
	shutdownCtx, c := context.WithTimeout(context.Background(), 3*time.Second)
	defer c()
	_ = httpSrv.Shutdown(shutdownCtx)
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		h.ServeHTTP(w, r)
		log.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

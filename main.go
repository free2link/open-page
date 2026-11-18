package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const appVersion = "0.1.0"

//go:embed admin
var assets embed.FS

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func probe(address string) error {
	client := http.Client{Timeout: 3 * time.Second}
	res, err := client.Get(address)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("health returned %s", res.Status)
	}
	return nil
}

func (a *Service) handler() http.Handler {
	files, err := fs.Sub(assets, "admin")
	if err != nil {
		panic(err)
	}
	static := http.FileServerFS(files)
	mux := http.NewServeMux()
	a.registerPages(mux)
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusPermanentRedirect)
	})
	mux.HandleFunc("GET /admin/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.StripPrefix("/admin/", static).ServeHTTP(w, r)
	})
	api, mcp := a.api(), a.mcp()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/"):
			api.ServeHTTP(w, r)
		case r.URL.Path == "/mcp":
			mcp.ServeHTTP(w, r)
		default:
			mux.ServeHTTP(w, r)
		}
	})
}

func run() error {
	healthcheck := flag.Bool("healthcheck", false, "check the local OpenPage public listener and exit")
	flag.Parse()
	publicPort := env("OPENPAGE_PORT", "3000")
	if *healthcheck {
		return probe("http://127.0.0.1:" + publicPort + "/healthz")
	}
	port, err := strconv.Atoi(publicPort)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("invalid listener port %q", publicPort)
	}
	origin, err := validOrigin(env("OPENPAGE_URL", env("OPENPAGE_PUBLIC_URL", "http://localhost:"+publicPort)))
	if err != nil {
		return err
	}
	s, err := openStore(env("OPENPAGE_DATA_DIR", "./data"), os.Getenv("OPENPAGE_TOKEN"))
	if err != nil {
		return err
	}
	defer s.close()
	a := Service{store: s, origin: origin}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	listener, err := net.Listen("tcp", ":"+publicPort)
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{
		Handler: a.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
		IdleTimeout: 60 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx },
	}
	log.Printf("OpenPage listening on :%s", publicPort)
	failure := make(chan error, 1)
	go func() { failure <- server.Serve(listener) }()
	select {
	case err = <-failure:
	case <-ctx.Done():
	}
	stop()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if server.Shutdown(shutdown) != nil {
		server.Close()
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func main() {
	log.SetOutput(os.Stdout)
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

// Command redditclone is the Go port of the Reddit clone tutorial app.
//
// Usage:
//
//	redditclone          serve HTTP (default)
//	redditclone serve    same as above
//	redditclone migrate        apply pending migrations
//	redditclone migrate down   roll back the latest migration
//	redditclone migrate status list applied migrations
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"redditclone/internal/config"
	"redditclone/internal/db"
	"redditclone/internal/web"
)

func main() {
	log.SetFlags(0)
	ctx := context.Background()

	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	var err error
	switch cmd {
	case "serve":
		err = runServe(ctx)
	case "migrate":
		sub := ""
		if len(os.Args) > 2 {
			sub = os.Args[2]
		}
		err = runMigrate(ctx, sub)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		fmt.Fprintln(os.Stderr, "usage: redditclone [serve|migrate [down|status]]")
		os.Exit(2)
	}
	if err != nil {
		log.Fatalf("%s: %v", cmd, err)
	}
}

func runMigrate(ctx context.Context, sub string) error {
	cfg, err := config.LoadForMigrate()
	if err != nil {
		return err
	}
	conn, err := db.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	m := db.NewMigrator(conn)
	switch sub {
	case "", "up":
		newly, err := m.Up(ctx)
		if err != nil {
			return err
		}
		if len(newly) == 0 {
			fmt.Println("migrations: already up to date")
		}
		for _, v := range newly {
			fmt.Printf("migrations: applied %s\n", v)
		}
		return nil
	case "down":
		v, err := m.Down(ctx)
		if err != nil {
			return err
		}
		if v == "" {
			fmt.Println("migrations: nothing to roll back")
		} else {
			fmt.Printf("migrations: rolled back %s\n", v)
		}
		return nil
	case "status":
		applied, err := m.Applied(ctx)
		if err != nil {
			return err
		}
		for _, v := range applied {
			fmt.Printf("applied: %s\n", v)
		}
		if len(applied) == 0 {
			fmt.Println("applied: (none)")
		}
		return nil
	}
	return fmt.Errorf("unknown migrate subcommand %q", sub)
}

func runServe(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	conn, err := db.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	srv, err := web.New(cfg, conn)
	if err != nil {
		return err
	}

	httpSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on :%d", cfg.Port)
		errCh <- httpSrv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case <-sigCh:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	}
}

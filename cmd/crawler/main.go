// Command crawler = single binary: serve | worker | migrate | ...
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/hneyra/crawler-go/internal/platform/httpx"
)

// version set by -ldflags at build.
var version = "dev"

const usage = `usage: crawler <command>

commands:
  serve              http api
  migrate up         apply pending migrations
  migrate down-to N  roll back to version N (0 = all). dev only.
  migrate status     print db version
  sync-sites [file]  upsert sites + categories from yaml (default ./sites-config.yml)
  discover CAT_ID    run discovery of one category now (foreground)
  admin create E     create admin user E (password: CRAWLER_ADMIN_PASSWORD or stdin)
  admin set-password E  change password of E, revoke its sessions
  admin promote E    grant admin to user E
  healthcheck [url]  exit 0 if url (default http://127.0.0.1:8080/readyz) answers 2xx
  version
`

func main() {
	// ctx handler: request_id on every log line inside a request.
	log := slog.New(httpx.NewContextHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], log); err != nil {
		var u usageError
		if errors.As(err, &u) {
			fmt.Fprint(os.Stderr, usage)
			os.Exit(2)
		}
		log.Error("exit", "err", err)
		os.Exit(1)
	}
}

type usageError struct{}

func (usageError) Error() string { return "usage" }

func run(ctx context.Context, args []string, log *slog.Logger) error {
	if len(args) == 0 {
		return usageError{}
	}
	switch args[0] {
	case "serve":
		return serve(ctx, log)
	case "migrate":
		return migrate(ctx, log, args[1:])
	case "discover":
		return discoverCmd(ctx, log, args[1:])
	case "admin":
		return admin(ctx, log, args[1:], os.Stdin)
	case "sync-sites":
		return syncSites(ctx, log, args[1:])
	case "healthcheck":
		return healthcheck(ctx, args[1:])
	case "version":
		fmt.Println(version)
		return nil
	default:
		return usageError{}
	}
}

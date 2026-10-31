package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// admin = user management. no sign-up in the ui.
//
//	crawler admin create EMAIL   (password: CRAWLER_ADMIN_PASSWORD or first stdin line). user is admin.
//	crawler admin set-password EMAIL
//	crawler admin promote EMAIL
func admin(ctx context.Context, log *slog.Logger, args []string, stdin io.Reader) error {
	if len(args) != 2 {
		return usageError{}
	}
	cmd, email := args[0], args[1]
	d, closeDB, err := open(ctx, log)
	if err != nil {
		return err
	}
	defer closeDB()
	ids, err := identityService(d)
	if err != nil {
		return err
	}
	switch cmd {
	case "create":
		pw, err := readPassword(stdin)
		if err != nil {
			return err
		}
		u, err := ids.CreateUser(ctx, email, pw, true)
		if err != nil {
			return err
		}
		log.Info("admin created", "user_id", u.ID, "email", u.Email)
	case "set-password":
		pw, err := readPassword(stdin)
		if err != nil {
			return err
		}
		if err := ids.SetPassword(ctx, email, pw); err != nil {
			return err
		}
		log.Info("password changed; sessions revoked", "email", email)
	case "promote":
		if err := ids.Promote(ctx, email); err != nil {
			return err
		}
		log.Info("user promoted to admin", "email", email)
	default:
		return usageError{}
	}
	return nil
}

// readPassword: env first (scripts/k8s), else first stdin line. never from argv (ps/history leak).
func readPassword(stdin io.Reader) (string, error) {
	if pw := os.Getenv("CRAWLER_ADMIN_PASSWORD"); pw != "" {
		return pw, nil
	}
	fmt.Fprint(os.Stderr, "password: ")
	line, err := bufio.NewReader(io.LimitReader(stdin, 1024)).ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("read password from stdin: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

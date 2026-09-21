package auth

import (
	"context"
	"flag"
	"fmt"
	"io"
	"syscall"

	"golang.org/x/term"

	"github.com/worryyy/devops-platform/platform/server/internal/config"
	"github.com/worryyy/devops-platform/platform/server/internal/db"
)

// RunUserCreate implements the `user-create` subcommand:
//
//	platform-server user-create -username admin [-role admin] [-password ...]
//
// Password is read interactively (twice) when not supplied via flag/env so it
// never lands in shell history by default.
func RunUserCreate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("user-create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	username := fs.String("username", "", "unique username (3-64 chars)")
	role := fs.String("role", RoleViewer, "role: viewer|admin")
	passwordFlag := fs.String("password", "", "password (omit to prompt securely)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *username == "" {
		fmt.Fprintln(stderr, "-username is required")
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "config: %v\n", err)
		return 1
	}

	password := *passwordFlag
	if password == "" {
		password, err = promptPassword(stdout, stderr)
		if err != nil {
			fmt.Fprintf(stderr, "read password: %v\n", err)
			return 1
		}
	}

	ctx := context.Background()
	gdb, err := db.Open(ctx, cfg)
	if err != nil {
		fmt.Fprintf(stderr, "open database: %v\n", err)
		return 1
	}

	user, err := NewUserStore(gdb).Create(ctx, *username, password, *role)
	if err != nil {
		fmt.Fprintf(stderr, "create user: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "created user %s (role=%s, id=%d)\n", user.Username, user.Role, user.ID)
	return 0
}

func promptPassword(stdout, stderr io.Writer) (string, error) {
	fmt.Fprint(stdout, "password: ")
	first, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(stdout)
	if err != nil {
		return "", err
	}
	fmt.Fprint(stdout, "confirm: ")
	second, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(stdout)
	if err != nil {
		return "", err
	}
	if string(first) != string(second) {
		return "", fmt.Errorf("passwords do not match")
	}
	if len(first) < 8 {
		return "", fmt.Errorf("password must be at least 8 characters")
	}
	return string(first), nil
}

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"sklad/server/internal/auth"
	"sklad/server/internal/config"
	"sklad/server/internal/store"
)

func maintenance(command string, args []string) error {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	stdin := fs.Bool("password-stdin", false, "read temporary Admin password from stdin (server must be stopped)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	if command == "reset-admin" && !*stdin {
		return fmt.Errorf("reset-admin requires --password-stdin; stop the server first")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	info, err := os.Stat(cfg.DBPath)
	if err != nil || info.IsDir() {
		return fmt.Errorf("existing database required at %s", cfg.DBPath)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	st, err := store.Open(ctx, store.Options{DBPath: cfg.DBPath, DataDir: cfg.DataDir})
	if err != nil {
		return err
	}
	defer st.Close()
	if command == "backup" {
		path, err := st.BackupDB(ctx)
		if err == nil {
			fmt.Println(path)
		}
		return err
	}
	inputInfo, err := os.Stdin.Stat()
	if err != nil {
		return err
	}
	if inputInfo.Mode()&os.ModeCharDevice != 0 {
		return fmt.Errorf("pipe the password through stdin; interactive password echo is not supported")
	}
	input, err := io.ReadAll(io.LimitReader(os.Stdin, 1026))
	if err != nil {
		return err
	}
	password := strings.TrimSuffix(strings.TrimSuffix(string(input), "\n"), "\r")
	if strings.ContainsAny(password, "\r\n") {
		return fmt.Errorf("provide one password line")
	}
	// Keep a recovery point before changing credentials; failure aborts recovery.
	if _, err = st.BackupDB(ctx); err != nil {
		return fmt.Errorf("pre-recovery backup: %w", err)
	}
	if err = auth.RecoverAdmin(ctx, st.DB(), cfg.AdminUsername, password); err != nil {
		return err
	}
	fmt.Println("Admin recovered; sessions revoked. Sign in with the temporary password and change it immediately.")
	return nil
}

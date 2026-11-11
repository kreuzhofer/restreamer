package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kreuzhofer/restreamer/internal/accounts"
	"golang.org/x/term"
)

func resetAccount(args []string) int {
	fs := flag.NewFlagSet("account reset-password", flag.ContinueOnError)
	database := fs.String("database", "/data/users.sqlite", "existing users database")
	user := fs.String("username", "", "existing operator username")
	stdin := fs.Bool("password-stdin", false, "read a new password from stdin instead of a hidden terminal prompt")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *user == "" {
		fmt.Fprintln(os.Stderr, "Specify -username and the existing -database; passwords are never accepted as arguments.")
		return 2
	}
	store, err := accounts.Open(*database, "", "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var password string
	if *stdin {
		data, readErr := io.ReadAll(io.LimitReader(os.Stdin, 4097))
		if readErr != nil || len(data) > 4096 {
			fmt.Fprintln(os.Stderr, "Cannot read password (maximum 4096 bytes)")
			return 1
		}
		password = strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
	} else {
		fd := int(os.Stdin.Fd())
		if !term.IsTerminal(fd) {
			fmt.Fprintln(os.Stderr, "Use a terminal for the hidden prompt, or explicitly select -password-stdin.")
			return 2
		}
		fmt.Fprint(os.Stderr, "New password: ")
		first, readErr := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if readErr != nil {
			fmt.Fprintln(os.Stderr, "Cannot read password")
			return 1
		}
		fmt.Fprint(os.Stderr, "Confirm new password: ")
		second, readErr := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if readErr != nil || string(first) != string(second) {
			fmt.Fprintln(os.Stderr, "Passwords do not match")
			return 1
		}
		password = string(first)
	}
	if err := store.Reset(*user, password); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("Password reset. Sign in with the new password; previous credentials no longer work.")
	return 0
}

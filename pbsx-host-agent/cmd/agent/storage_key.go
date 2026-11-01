package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pbs-x/pbsx-host-agent/internal/agent"
	"golang.org/x/term"
)

func storageKeyCommand(args []string) (bool, error) {
	if len(args) == 0 || args[0] != "storage-key" {
		return false, nil
	}
	if len(args) != 2 {
		return true, fmt.Errorf("usage: pbsx-host-agent storage-key auto|user@realm!token (secret is read securely from terminal or stdin)")
	}
	c := agent.StorageCredentials{}
	if args[1] != "auto" {
		c.AuthID = args[1]
		var data []byte
		var err error
		if term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Fprint(os.Stderr, "API token secret: ")
			data, err = term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
		} else {
			data, err = io.ReadAll(io.LimitReader(os.Stdin, 16*1024))
		}
		if err != nil {
			return true, err
		}
		c.Secret = strings.TrimSpace(string(data))
	}
	if err := agent.SetStorageCredentials(c); err != nil {
		return true, err
	}
	fmt.Fprintln(os.Stderr, "Storage credential updated; host management credentials are unchanged.")
	return true, nil
}

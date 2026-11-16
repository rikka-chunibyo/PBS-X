// pbsx-agentctl is a graphics-free local status and administration CLI.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/pbs-x/pbsx-host-agent/internal/agentstatus"
	"github.com/pbs-x/pbsx-host-agent/internal/desktopcontrol"
)

var Version = "0.7.0-preview"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Println("PBS-X Agent CLI\n  status [--json]\n  watch\n  enroll --stdin\n  start | stop | restart | install | uninstall\n  version")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	switch args[0] {
	case "version":
		fmt.Println(Version)
		return nil
	case "status":
		if len(args) > 2 || (len(args) == 2 && args[1] != "--json") {
			return fmt.Errorf("usage: status [--json]")
		}
		status, err := agentstatus.Read()
		if err != nil {
			return fmt.Errorf("agent status unavailable; install/start the matching agent service")
		}
		if len(args) == 2 {
			return json.NewEncoder(os.Stdout).Encode(struct {
				agentstatus.Snapshot
				Running bool `json:"running"`
			}{status, status.Running(time.Now())})
		}
		fmt.Printf("Agent %s: %s (running=%t, last heartbeat successful=%t)\n", status.Version, status.Phase, status.Running(time.Now()), status.Connected)
		if status.Operation != nil {
			fmt.Printf("%s: %s; %d files, %d bytes read, %d bytes uploaded\n", status.Operation.Kind, status.Operation.Stage, status.Operation.Files, status.Operation.ProcessedBytes, status.Operation.UploadedBytes)
		}
		return nil
	case "watch":
		for {
			if err := run([]string{"status"}); err != nil {
				return err
			}
			time.Sleep(2 * time.Second)
		}
	case "enroll":
		if len(args) != 2 || args[1] != "--stdin" {
			return fmt.Errorf("usage: pbsx-agentctl enroll --stdin")
		}
		data, err := io.ReadAll(io.LimitReader(os.Stdin, (64<<10)+1))
		if err != nil {
			return err
		}
		return (desktopcontrol.Controller{}).Enroll(ctx, strings.TrimSpace(string(data)))
	default:
		if len(args) != 1 {
			return fmt.Errorf("unexpected service command arguments")
		}
		return (desktopcontrol.Controller{}).Service(ctx, args[0])
	}
}

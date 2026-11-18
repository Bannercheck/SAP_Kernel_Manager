package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/hub"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/hubcli"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
)

// runConsole is "kernelman-hub console": the hub on a terminal, no browser,
// no listening port. Inventory, vault and jobs are the dashboard's.
func runConsole(args []string) int {
	fs := flag.NewFlagSet("kernelman-hub console", flag.ContinueOnError)
	data := fs.String("data", hub.DefaultDataDir(), "data directory (the dashboard's: inventory, encrypted secrets, jobs, archives)")
	demo := fs.Bool("demo", false, "simulated SAP landscape, nothing real is touched (login admin / kernelman1)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: kernelman-hub console [flags]\n\nThe hub on this terminal: no browser, no open port.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	h, release, code := openHub(*data, *demo)
	if h == nil {
		return code
	}
	defer release()
	pal := ui.Detect(os.Stdout)
	var out io.Writer = os.Stdout
	if !pal.Unicode {
		out = ui.ASCIIWriter(os.Stdout)
	}
	c := hubcli.New(h, os.Stdin, out, pal)
	c.TTY = ui.IsTerminal(os.Stdin)
	if *demo {
		fmt.Fprintln(out, "\n  DEMO — simulated SAP hosts, nothing real is touched. Login: admin / kernelman1")
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		for range sig {
			if !c.OnInterrupt() { // not watching a job: leave
				fmt.Fprintln(out)
				release()
				os.Exit(130)
			}
		}
	}()
	return c.Run()
}

// openHub locks the data directory (one hub process per directory: two
// would run two jobs at once) and opens the hub; demo uses a fresh one.
func openHub(data string, demo bool) (*hub.Hub, func(), int) {
	cfg := hub.Config{DataDir: data}
	if demo {
		cfg.DataDir = filepath.Join(os.TempDir(), "kernelman-hub-demo")
		_ = os.RemoveAll(cfg.DataDir) // a fresh landscape every start
		cfg.Remote, cfg.Demo = hub.NewDemoRemote(), true
	}
	release, err := hub.LockDataDir(cfg.DataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kernelman-hub:", err)
		return nil, nil, 1
	}
	h, err := hub.New(cfg)
	if err != nil {
		release()
		fmt.Fprintln(os.Stderr, "kernelman-hub:", err)
		return nil, nil, 1
	}
	if demo {
		if err := hub.SetupDemo(h, "admin", "kernelman1"); err != nil {
			release()
			fmt.Fprintln(os.Stderr, "kernelman-hub demo:", err)
			return nil, nil, 1
		}
	}
	return h, release, 0
}

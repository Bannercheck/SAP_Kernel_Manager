package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/download"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/system"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/swdc"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ui"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/version"
)

// newDownloadClient builds the HTTP client; tests replace it to point at a local server.
var newDownloadClient = func(cred download.Credentials) *download.Client { return download.NewClient(cred) }

// credentialsDir is where the S-user is remembered (~/.kernelman).
func credentialsDir() string {
	if demoRoot != "" {
		return filepath.Join(demoRoot, ".kernelman")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.TempDir()
	}
	return filepath.Join(home, ".kernelman")
}

// defaultDownloadDir is /usr/sap/download (or the demo's download folder).
func downloadDir() string {
	if demoRoot != "" {
		return filepath.Join(demoRoot, "download")
	}
	return filepath.Join(system.UsrSap, "download")
}

// DownloadOp implements Kernel Download: S-user login + Download Basket
// links → /usr/sap/download. The files are dated today afterwards, so
// Kernel File Transfer finds them.
func DownloadOp(args []string) int {
	fs := flag.NewFlagSet("download", flag.ContinueOnError)
	sid := fs.String("sid", "", "SAP system")
	basket := fs.String("basket", "", "SAP Download Basket text export instead of the automatic search")
	var urls multiFlag
	fs.Var(&urls, "url", "download link (repeatable)")
	to := fs.String("to", "", "target directory (default /usr/sap/download)")
	user := fs.String("user", "", "S-user (default: remembered one)")
	yes := fs.Bool("yes", false, "do not ask for confirmation")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	pal := currentPalette()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Hour)
	defer cancel()

	if !checkInternet(ctx) {
		return ExitError
	}
	cred, ok := askCredentials(*user, *yes)
	if !ok {
		return ExitError
	}
	var items []download.Item
	var session *swdc.Session
	if *basket == "" && len(urls) == 0 {
		t, err := target(ctx, *sid)
		if err != nil {
			return fail(err)
		}
		items, session, err = autoSelect(ctx, t, cred)
		if err != nil {
			fmt.Fprintf(stdout, "  %s %s\n", pal.Cross(), pal.Paint(ui.Red, err.Error()))
			if *yes || !confirm("Use SAP Download Basket links instead?") {
				return ExitError
			}
		}
	}
	if len(items) == 0 {
		items = collectItems(*basket, urls, *yes)
	}
	if len(items) == 0 {
		fmt.Fprintf(stdout, "  %s nothing to download\n", pal.Cross())
		return ExitError
	}
	dir := *to
	if dir == "" {
		dir = downloadDir()
		if !*yes {
			dir = ask("Download into", dir)
		}
	}
	if session == nil { // the automatic path already showed its table
		rows := [][]string{pal.Headers("#", "FILE", "SIZE", "LINK")}
		for i, it := range items {
			size := "?"
			if it.Size > 0 {
				size = ops.HumanSize(it.Size)
			}
			name := it.Name
			if name == "" {
				name = pal.Paint(ui.Dim, "(name from server)")
			}
			rows = append(rows, []string{fmt.Sprint(i + 1), name, size, it.URL})
		}
		fmt.Fprintf(stdout, "\n  %s\n", pal.Header(fmt.Sprintf("%d file(s) → %s", len(items), dir)))
		for _, l := range ui.Table("    ", rows) {
			fmt.Fprintln(stdout, l)
		}
	} else {
		fmt.Fprintf(stdout, "  target directory: %s\n", dir)
	}
	if !*yes && !confirm(fmt.Sprintf("Download these %d file(s) as %s?", len(items), cred.User)) {
		fmt.Fprintln(stdout, "  cancelled")
		return ExitError
	}

	client := newDownloadClient(cred)
	if session != nil {
		client.WithJar(session.HTTP.Jar)
	}
	failed := 0
	var total int64
	for i, it := range items {
		label := it.Name
		if label == "" {
			label = it.URL
		}
		fmt.Fprintf(stdout, "  [%d/%d] %s ... ", i+1, len(items), label)
		lastPct := -1
		res, err := client.Download(ctx, it, dir, func(name string, done, size int64) {
			if size <= 0 {
				return
			}
			if pct := int(done * 100 / size); pct/10 != lastPct/10 {
				lastPct = pct
				fmt.Fprintf(stdout, "%d%% ", pct)
			}
		})
		if err != nil {
			failed++
			fmt.Fprintf(stdout, "%s %s\n", pal.Cross(), pal.Paint(ui.Red, err.Error()))
			if err == download.ErrAuth {
				break
			}
			continue
		}
		note := ""
		if res.Resumed {
			note = " resumed"
		}
		if !download.IsSAR(res.Path) && strings.EqualFold(filepath.Ext(res.Path), ".sar") {
			note += " " + pal.Paint(ui.Yellow, "! not a SAPCAR archive")
		}
		total += res.Bytes
		fmt.Fprintf(stdout, "%s %s (%s, %s)%s\n      sha256 %s\n", pal.Paint(ui.Green, "ok"), filepath.Base(res.Path),
			ops.HumanSize(res.Bytes), res.Duration.Round(time.Second), note, pal.Paint(ui.Dim, res.SHA256))
	}
	if failed > 0 {
		return fail(fmt.Errorf("%d of %d downloads failed", failed, len(items)))
	}
	fmt.Fprintf(stdout, "\n  %s %d file(s), %s in %s · dated today, so Kernel File Transfer will find them\n", pal.Check(), len(items), ops.HumanSize(total), dir)
	return ExitOK
}

// askCredentials returns the S-user login, remembering the user name and,
// on request, the password in ~/.kernelman/suser.json (mode 0600).
func askCredentials(userFlag string, yes bool) (download.Credentials, bool) {
	pal := currentPalette()
	saved, have, _ := download.LoadCredentials(credentialsDir())
	cred := saved
	if userFlag != "" {
		cred.User = userFlag
	}
	if env := os.Getenv(version.EnvPrefix + "SUSER"); env != "" && cred.User == "" {
		cred.User = env
	}
	if env := os.Getenv(version.EnvPrefix + "SUSER_PASSWORD"); env != "" {
		cred.Password = env
	}
	if !yes {
		note := ""
		if have {
			note = " (remembered)"
		}
		cred.User = ask("S-user"+note, cred.User)
	}
	if cred.User == "" {
		fmt.Fprintf(stdout, "  %s an S-user is required\n", pal.Cross())
		return cred, false
	}
	if cred.Password == "" {
		if yes {
			fmt.Fprintf(stdout, "  %s no password: set %sSUSER_PASSWORD or run interactively\n", pal.Cross(), version.EnvPrefix)
			return cred, false
		}
		cred.Password = askSecret("Password for " + cred.User)
		if cred.Password == "" {
			return cred, false
		}
	}
	if !yes && (!have || saved.User != cred.User || saved.Password != cred.Password) {
		keepPw := confirm("Remember the password too? (stored in " + filepath.Join(credentialsDir(), "suser.json") + ", mode 0600)")
		_ = download.SaveCredentials(credentialsDir(), cred, keepPw)
	}
	return cred, true
}

// askSecret reads a line without echo when stdin is a terminal.
func askSecret(prompt string) string {
	fmt.Fprintf(stdout, "%s: ", prompt)
	if ui.IsTerminal(os.Stdin) {
		osexec.Command("stty", "-echo").Run()
		defer func() { osexec.Command("stty", "echo").Run(); fmt.Fprintln(stdout) }()
	}
	line, _ := input.ReadString('\n')
	if !ui.IsTerminal(os.Stdin) {
		fmt.Fprintln(stdout, strings.Repeat("*", len(strings.TrimSpace(line))))
	}
	return strings.TrimSpace(line)
}

// collectItems gathers links from the basket file, the flags or the keyboard.
func collectItems(basket string, urls []string, yes bool) []download.Item {
	pal := currentPalette()
	var items []download.Item
	for _, u := range urls {
		items = append(items, download.ParseBasket(u)...)
	}
	if basket != "" {
		b, err := os.ReadFile(basket)
		if err != nil {
			fmt.Fprintf(stdout, "  %s %v\n", pal.Cross(), err)
			return nil
		}
		items = append(items, download.ParseBasket(string(b))...)
	}
	if len(items) > 0 || yes {
		return items
	}
	fmt.Fprintf(stdout, "\n  %s\n", pal.Paint(ui.Dim, "Links come from SAP for Me → Software Center → Download Basket → \"export as text\", or paste them here."))
	switch choose("Where are the download links?", choice{"F", "Basket export file", []string{"file", "b", "basket"}}, choice{"P", "Paste links", []string{"paste", "u", "url"}}, mainMenu) {
	case "F":
		path := ask("Basket file", "")
		if path == "" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(stdout, "  %s %v\n", pal.Cross(), err)
			return nil
		}
		return download.ParseBasket(string(b))
	case "P":
		fmt.Fprintln(stdout, "  Paste one link per line, finish with an empty line:")
		var text strings.Builder
		for {
			fmt.Fprint(stdout, "  › ")
			line, esc, err := readLine()
			if line == "" || esc || err != nil {
				break
			}
			text.WriteString(line + "\n")
		}
		return download.ParseBasket(text.String())
	}
	return nil
}

// multiFlag collects repeated --url flags.
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

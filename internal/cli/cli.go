// Package cli is the console mode of napi-discover (--console; Linux without a display by default).
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/lab240/napi-avahi/internal/napifind"
)

// Run parses the flags (-t, -v, -raw; -console and -n are accepted and ignored), searches and
// prints the table. The exit code: 0, 1 on an error, 2 on bad flags.
func Run(name string, args []string) int {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	wait := fs.Duration("t", 3*time.Second, "how long to listen for answers")
	verbose := fs.Bool("v", false, "show interfaces, the direct sweep and every answer")
	raw := fs.Bool("raw", false, "raw search: everything on TCP 22 of the own subnets, with the signs of a NAPI")
	fs.Bool("console", false, "napi-discover.exe: the console instead of the window")
	fs.Bool("n", false, "ignored (old: do not wait for Enter)")
	if fs.Parse(args) != nil {
		return 2
	}
	if *verbose {
		t0 := time.Now()
		napifind.Log = func(f string, a ...any) {
			fmt.Fprintf(os.Stderr, "  %5.2fs  "+f+"\n", append([]any{time.Since(t0).Seconds()}, a...)...)
		}
	}
	if *raw {
		hosts, err := napifind.Scan(nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ошибка:", err)
			return 1
		}
		PrintHosts(os.Stdout, hosts)
		return 0
	}
	list, err := napifind.Find(*wait)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		return 1
	}
	Print(os.Stdout, list)
	return 0
}

// Print writes the boards as a table.
func Print(out io.Writer, list []napifind.Board) {
	if len(list) == 0 {
		fmt.Fprintln(out, "NAPI не найдены. Проверьте, что компьютер в той же сети и на NAPI стоит пакет napi-avahi")
		return
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ИМЯ\tIP\tМОДЕЛЬ\tSOC\tOS\tРОЛЬ\tMAC")
	for _, b := range list {
		t := b.TXT
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", b.Host, dash(strings.Join(b.IP, ",")),
			dash(t["model"]), dash(t["soc"]), dash(t["os"]), dash(t["role"]), dash(t["mac"]))
	}
	w.Flush()
	fmt.Fprintf(out, "\nнайдено: %d\n", len(list))
}

// PrintHosts writes the raw search as a table.
func PrintHosts(out io.Writer, list []napifind.Host) {
	if len(list) == 0 {
		fmt.Fprintln(out, "Никто не ответил на порту 22")
		return
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ВЕРДИКТ\tIP\tИМЯ\tMAC\tSSH\tПРИЗНАКИ")
	for _, h := range list {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", h.Verdict, h.IP, dash(h.Name), dash(h.MAC), dash(h.SSH),
			dash(strings.Join(h.Signs, " ")))
	}
	w.Flush()
	fmt.Fprintf(out, "\nна порту 22: %d\n", len(list))
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

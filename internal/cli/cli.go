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

	"github.com/lab240/napi-avahi/internal/fixed"
	"github.com/lab240/napi-avahi/internal/napifind"
)

// Run parses the flags (-v, -raw, -fix, -unfix; -t, -console and -n are accepted), searches and
// prints the table. The exit code: 0, 1 on an error, 2 on bad flags.
//
//	--fix <IP> <name>     mark the board at IP with a name of your own (shown first, found by the
//	                      SSH host key / MAC even when its IP or network name changes)
//	--model M, --soc S    with --fix: the model and the SoC (a board without napi-avahi)
//	--unfix <IP|name>     drop the mark
func Run(name string, args []string) int {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Duration("t", 3*time.Second, "ignored (the search listens 3 s)")
	verbose := fs.Bool("v", false, "show interfaces, the direct sweep and every answer")
	raw := fs.Bool("raw", false, "raw search: everything on TCP 22 of the own subnets, with the signs of a NAPI")
	fix := fs.String("fix", "", "mark the board at this IP: --fix <IP> <name>")
	unfix := fs.String("unfix", "", "drop the mark: --unfix <IP or name>")
	model := fs.String("model", "", "with --fix: the model of the board (no napi-avahi): --fix <IP> --model M <name>")
	soc := fs.String("soc", "", "with --fix: the SoC of the board (no napi-avahi)")
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
	if *unfix != "" {
		return doUnfix(*unfix)
	}
	if *fix != "" {
		label := strings.Join(fs.Args(), " ")
		if label == "" {
			fmt.Fprintln(os.Stderr, "нужно имя: --fix <IP> [--model M] [--soc S] <имя>")
			return 2
		}
		return doFix(*fix, label, *model, *soc)
	}
	found, err := fixed.Search(*raw, nil)
	if err != nil && len(found) == 0 {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		return 1
	}
	Print(os.Stdout, fixed.Annotate(found), *raw)
	return 0
}

func doFix(ip, label, model, soc string) int {
	r := fixed.Row{Entry: -1, IP: ip}
	if hs := napifind.Probe([]string{ip}); len(hs) > 0 {
		r.Name, r.MAC = hs[0].Name, hs[0].MAC
	}
	r.Key = napifind.HostKeys([]string{ip})[ip]
	if r.Key == "" && r.MAC == "" {
		fmt.Fprintf(os.Stderr, "%s: нет ответа SSH и MAC (плата в другой подсети?), отметка будет только по IP\n", ip)
	}
	if err := fixed.SetName(r, label); err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		return 1
	}
	if model != "" || soc != "" {
		es, _ := fixed.Load()
		for i, e := range es {
			if e.Name != label {
				continue
			}
			for f, v := range map[string]string{"model": model, "soc": soc} {
				if v == "" {
					continue
				}
				if err := fixed.SetInfo(fixed.Row{Entry: i, Fixed: label}, f, v); err != nil {
					fmt.Fprintln(os.Stderr, "ошибка:", err)
					return 1
				}
			}
			break
		}
	}
	fmt.Printf("%s отмечена как «%s» (%s)\n", ip, label, fixed.Path())
	return 0
}

func doUnfix(what string) int {
	es, err := fixed.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		return 1
	}
	for i, e := range es {
		if e.IP == what || strings.EqualFold(e.Name, what) {
			if err := fixed.Save(fixed.Remove(es, i)); err != nil {
				fmt.Fprintln(os.Stderr, "ошибка:", err)
				return 1
			}
			fmt.Printf("отметка «%s» снята\n", e.Name)
			return 0
		}
	}
	fmt.Fprintf(os.Stderr, "нет отмеченной платы %q\n", what)
	return 1
}

// Print writes the rows as a table.
func Print(out io.Writer, rows []fixed.Row, raw bool) {
	if len(rows) == 0 {
		if raw {
			fmt.Fprintln(out, "Никто не ответил на порту 22")
		} else {
			fmt.Fprintln(out, "NAPI не найдены: никто не ответил по avahi. Проверьте, что компьютер в той же сети; платы без avahi найдёт --raw")
		}
		return
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "МОЁ ИМЯ\tИМЯ\tIP\tМОДЕЛЬ\tSOC\tOS\tРОЛЬ\tMAC\tПРИЗНАКИ")
	n := 0
	for _, r := range rows {
		if r.Kind != "gone" {
			n++
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", dash(r.Fixed), dash(r.Name), dash(r.IP),
			dash(r.Model), dash(r.SoC), dash(r.OS), dash(r.Role), dash(r.MAC), dash(r.Signs))
	}
	w.Flush()
	fmt.Fprintf(out, "\nнайдено: %d\n", n)
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

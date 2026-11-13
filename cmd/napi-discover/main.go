// napi-discover lists the NAPI boards on the LAN in the console: name, IP and the TXT fields of the
// mDNS service _napi._tcp that the napi-avahi package announces. One file, Windows and Linux.
// The window version for Windows is cmd/napi-finder.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/lab240/napi-avahi/internal/napifind"
)

func main() {
	wait := flag.Duration("t", 3*time.Second, "how long to listen for answers")
	nowait := flag.Bool("n", false, "Windows: do not wait for Enter at the end")
	flag.Parse()
	err := run(*wait)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
	}
	if runtime.GOOS == "windows" && !*nowait {
		fmt.Print("\nНажмите Enter...")
		fmt.Scanln()
	}
	if err != nil {
		os.Exit(1)
	}
}

func run(wait time.Duration) error {
	list, err := napifind.Find(wait)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("NAPI не найдены. Проверьте, что компьютер в той же сети и на NAPI стоит пакет napi-avahi")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ИМЯ\tIP\tМОДЕЛЬ\tSOC\tOS\tРОЛЬ\tMAC")
	for _, b := range list {
		t := b.TXT
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", b.Host, dash(strings.Join(b.IP, ",")),
			dash(t["model"]), dash(t["soc"]), dash(t["os"]), dash(t["role"]), dash(t["mac"]))
	}
	w.Flush()
	fmt.Printf("\nнайдено: %d\n", len(list))
	return nil
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// napi-finder for Linux: the same tool as the Windows window, as a page in the browser served by
// itself on 127.0.0.1 (no GUI libraries: one static file for any desktop, X11 or Wayland).
//
//	napi-finder                               the page (Chrome/Chromium --app window, else xdg-open)
//	napi-finder --console [-v] [-t 5s] [--raw]   the table in the console
//	napi-finder --serial /dev/ttyUSB0 [--baud 115200]   the UART console
//
// SSH and the UART console open in a terminal window (gnome-terminal, konsole, xfce4-terminal, ...).
// The page and its API live under a random token, and the Host header is checked: other users of
// the machine and other web pages cannot drive it. It exits when the page has been closed for a while.
package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lab240/napi-avahi/internal/cli"
	"github.com/lab240/napi-avahi/internal/napifind"
)

var version = "dev" // make tools: -X main.version=<VERSION>

//go:embed web
var webFS embed.FS

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--serial" || os.Args[1] == "-serial") {
		os.Exit(serialMain(os.Args[2:]))
	}
	if len(os.Args) > 1 {
		os.Exit(cli.Run("napi-finder", os.Args[1:]))
	}
	if err := serve(); err != nil {
		fmt.Fprintln(os.Stderr, "napi-finder:", err)
		os.Exit(1)
	}
}

// One line of the table.
type row struct {
	Name, IP, Model, SoC, OS, Role, MAC, Signs string
}

// The search state the page polls.
type state struct {
	Busy  bool   `json:"busy"`
	Raw   bool   `json:"raw"`
	Done  int32  `json:"done"`
	Total int32  `json:"total"`
	Since int64  `json:"since"` // ms since the start
	Rows  []row  `json:"rows"`
	Log   string `json:"log"`
	Err   string `json:"err"`
}

var (
	mu          sync.Mutex
	st          state
	started     time.Time
	done, total atomic.Int32
	lastPing    atomic.Int64
)

func serve() error {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	b := make([]byte, 16)
	rand.Read(b)
	token := hex.EncodeToString(b)
	host := ln.Addr().String()
	base := "/" + token + "/"
	url := "http://" + host + base

	web, _ := fs.Sub(webFS, "web")
	mux := http.NewServeMux()
	mux.Handle(base, http.StripPrefix(base, http.FileServer(http.FS(web))))
	api := func(path string, post bool, h func(http.ResponseWriter, *http.Request)) {
		mux.HandleFunc(base+"api/"+path, func(w http.ResponseWriter, r *http.Request) {
			if post && r.Method != http.MethodPost {
				http.Error(w, "POST", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			h(w, r)
		})
	}
	api("state", false, func(w http.ResponseWriter, r *http.Request) {
		lastPing.Store(time.Now().UnixMilli())
		mu.Lock()
		s := st
		mu.Unlock()
		s.Done, s.Total = done.Load(), total.Load()
		if s.Busy {
			s.Since = time.Since(started).Milliseconds()
		}
		writeJSON(w, s)
	})
	api("info", false, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"version": version, "terminal": terminalName()})
	})
	api("search", true, func(w http.ResponseWriter, r *http.Request) {
		if !search(r.URL.Query().Get("raw") == "1") {
			http.Error(w, "поиск уже идёт", http.StatusConflict)
		}
	})
	api("ssh", true, func(w http.ResponseWriter, r *http.Request) {
		ip := r.URL.Query().Get("ip")
		if !known(ip) {
			http.Error(w, "нет такой платы в таблице", http.StatusBadRequest)
			return
		}
		// The login is asked in the terminal; ip is one of the found addresses.
		script := `printf 'login: '; read L; [ -n "$L" ] && exec ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR "$L@` + ip + `"; ` + pause
		reply(w, terminal("NAPI "+ip, "sh", "-c", script))
	})
	api("ports", false, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ports": serialPorts(), "dialout": inDialout()})
	})
	api("serial", true, func(w http.ResponseWriter, r *http.Request) {
		port, baud := r.URL.Query().Get("port"), r.URL.Query().Get("baud")
		ok := false
		for _, p := range serialPorts() {
			ok = ok || p.Name == port
		}
		if b, err := strconv.Atoi(baud); !ok || err != nil || b <= 0 {
			http.Error(w, "нет такого порта", http.StatusBadRequest)
			return
		}
		exe, _ := os.Executable()
		reply(w, terminal("NAPI "+filepath.Base(port)+" "+baud, exe, "--serial", port, "--baud", baud))
	})

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != host { // DNS rebinding: only http://127.0.0.1:<port>
			http.Error(w, "bad host", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})}

	fmt.Println("napi-finder:", url)
	fmt.Println("Закройте страницу или нажмите Ctrl+C, чтобы выйти.")
	go openBrowser(url)
	// Exit when the page has not polled for 20 s (closed), or no page came within 2 minutes.
	lastPing.Store(time.Now().Add(100 * time.Second).UnixMilli())
	go func() {
		for range time.Tick(2 * time.Second) {
			if time.Now().UnixMilli()-lastPing.Load() > 20000 {
				fmt.Println("napi-finder: страница закрыта, выхожу")
				os.Exit(0)
			}
		}
	}()
	return srv.Serve(ln)
}

const pause = `echo; printf 'Нажмите Enter...'; read _`

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func reply(w http.ResponseWriter, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func known(ip string) bool {
	mu.Lock()
	defer mu.Unlock()
	for _, r := range st.Rows {
		for _, a := range strings.Split(r.IP, ",") {
			if strings.TrimSpace(a) != ip {
				continue
			}
			return net.ParseIP(ip) != nil
		}
	}
	return false
}

// search starts in the background; false when one is running already.
func search(raw bool) bool {
	mu.Lock()
	if st.Busy {
		mu.Unlock()
		return false
	}
	st = state{Busy: true, Raw: raw, Rows: st.Rows}
	started = time.Now()
	done.Store(0)
	total.Store(0)
	mu.Unlock()

	go func() {
		var lg strings.Builder
		var lmu sync.Mutex
		t0 := time.Now()
		napifind.Log = func(f string, a ...any) {
			lmu.Lock()
			fmt.Fprintf(&lg, "%5.2fs  "+f+"\n", append([]any{time.Since(t0).Seconds()}, a...)...)
			lmu.Unlock()
		}
		var rows []row
		var err error
		if raw {
			var hosts []napifind.Host
			hosts, err = napifind.Scan(func(d, t int) { done.Store(int32(d)); total.Store(int32(t)) })
			for _, h := range hosts {
				r := row{Name: h.Name, IP: h.IP, OS: h.SSH, MAC: h.MAC, Signs: h.Verdict}
				if b := h.Board; b != nil {
					r.Model, r.SoC, r.OS, r.Role = b.TXT["model"], b.TXT["soc"], b.TXT["os"], b.TXT["role"]
				}
				if len(h.Signs) > 0 {
					r.Signs += ": " + strings.Join(h.Signs, " ")
				}
				rows = append(rows, r)
			}
		} else {
			var list []napifind.Board
			list, err = napifind.Find(3 * time.Second)
			for _, b := range list {
				t := b.TXT
				rows = append(rows, row{Name: b.Host, IP: strings.Join(b.IP, ", "), Model: t["model"], SoC: t["soc"],
					OS: t["os"], Role: t["role"], MAC: t["mac"], Signs: "napi-avahi"})
			}
		}
		napifind.Log = nil
		lmu.Lock()
		fmt.Fprintf(&lg, "найдено: %d\n", len(rows))
		log := lg.String()
		lmu.Unlock()
		mu.Lock()
		st = state{Raw: raw, Rows: rows, Log: log}
		if err != nil {
			st.Err = err.Error()
		}
		mu.Unlock()
	}()
	return true
}

// An --app window of Chrome/Chromium looks like a program; else the default browser.
func openBrowser(url string) {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" { // ssh, a console: the address only
		fmt.Println("Нет графики: откройте адрес в браузере (ssh -L, если napi-finder на другой машине).")
		return
	}
	for _, b := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "microsoft-edge"} {
		if p, err := exec.LookPath(b); err == nil {
			if exec.Command(p, "--app="+url, "--window-size=1320,580").Start() == nil {
				return
			}
		}
	}
	if exec.Command("xdg-open", url).Start() != nil {
		fmt.Println("Откройте в браузере:", url)
	}
}

// The terminals and how each takes a command.
var terminals = []struct {
	bin  string
	args func(title string) []string // before the command
}{
	{"gnome-terminal", func(t string) []string { return []string{"--title", t, "--"} }},
	{"konsole", func(t string) []string { return []string{"-p", "tabtitle=" + t, "-e"} }},
	{"xfce4-terminal", func(t string) []string { return []string{"-T", t, "-x"} }},
	{"mate-terminal", func(t string) []string { return []string{"-t", t, "-x"} }},
	{"tilix", func(t string) []string { return []string{"-t", t, "-e"} }},
	{"kitty", func(t string) []string { return []string{"--title", t} }},
	{"alacritty", func(t string) []string { return []string{"-T", t, "-e"} }},
	{"foot", func(t string) []string { return []string{"-T", t} }},
	{"x-terminal-emulator", func(t string) []string { return []string{"-T", t, "-e"} }},
	{"xterm", func(t string) []string { return []string{"-T", t, "-e"} }},
}

func terminalName() string {
	if t := os.Getenv("TERMINAL"); t != "" {
		if _, err := exec.LookPath(t); err == nil {
			return t
		}
	}
	for _, t := range terminals {
		if _, err := exec.LookPath(t.bin); err == nil {
			return t.bin
		}
	}
	return ""
}

func terminal(title string, cmd ...string) error {
	name := terminalName()
	if name == "" {
		return fmt.Errorf("не найден терминал (gnome-terminal, konsole, xfce4-terminal, xterm...)")
	}
	args := []string{"-e"} // $TERMINAL of an unknown kind
	for _, t := range terminals {
		if t.bin == name {
			args = t.args(title)
		}
	}
	c := exec.Command(name, append(args, cmd...)...)
	if err := c.Start(); err != nil {
		return err
	}
	go c.Wait()
	return nil
}

type serialPort struct {
	Name string `json:"name"` // /dev/ttyUSB0
	Desc string `json:"desc"` // FT232R USB UART
}

// USB-UART adapters and built-in ports with a device behind them.
func serialPorts() []serialPort {
	var out []serialPort
	for _, pat := range []string{"/dev/ttyUSB*", "/dev/ttyACM*"} {
		list, _ := filepath.Glob(pat)
		for _, p := range list {
			out = append(out, serialPort{p, usbName(filepath.Base(p))})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// The product name from sysfs: /sys/class/tty/ttyUSB0/device -> the interface, its parent the device.
func usbName(tty string) string {
	dev, err := filepath.EvalSymlinks("/sys/class/tty/" + tty + "/device")
	if err != nil {
		return ""
	}
	for d := dev; d != "/" && d != "."; d = filepath.Dir(d) {
		if b, err := os.ReadFile(d + "/product"); err == nil {
			m, _ := os.ReadFile(d + "/manufacturer")
			return strings.TrimSpace(strings.TrimSpace(string(m)) + " " + strings.TrimSpace(string(b)))
		}
	}
	return ""
}

// The UART needs the dialout group (or root) on most distributions.
func inDialout() bool {
	if os.Geteuid() == 0 {
		return true
	}
	gs, _ := os.Getgroups()
	b, _ := os.ReadFile("/etc/group")
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Split(l, ":")
		if len(f) >= 3 && (f[0] == "dialout" || f[0] == "uucp") {
			gid, _ := strconv.Atoi(f[2])
			for _, g := range gs {
				if g == gid {
					return true
				}
			}
		}
	}
	return false
}

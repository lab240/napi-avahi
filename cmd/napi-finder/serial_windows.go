package main

// The UART console of a NAPI (USB-UART adapter, COM port): napi-finder.exe --serial COM5 [--baud 1500000].
// The window's "COM" menu starts it in a console of its own. 8N1, no flow control; the console
// passes the keys as VT sequences (arrows, Tab, Ctrl+C go to the board), Ctrl+] quits.

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// The debug UART of RK3308 (NAPI-C) and RK3568 (NAPI2).
const defaultBaud = 1500000

var (
	pSetConsoleCP       = kernel32.NewProc("SetConsoleCP")
	pSetConsoleOutputCP = kernel32.NewProc("SetConsoleOutputCP")
	pSetConsoleTitle    = kernel32.NewProc("SetConsoleTitleW")
	comRe               = regexp.MustCompile(`^COM[0-9]{1,3}$`)
)

type comPort struct {
	name, desc string // COM5, "USB-SERIAL CH340"
}

// The ports present now (HARDWARE\DEVICEMAP\SERIALCOMM) with the adapter names from the device tree
// of the registry (Enum\USB, Enum\FTDIBUS: FriendlyName and Device Parameters\PortName).
func comPorts() []comPort {
	friendly := map[string]string{}
	for _, bus := range []string{`SYSTEM\CurrentControlSet\Enum\USB`, `SYSTEM\CurrentControlSet\Enum\FTDIBUS`} {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, bus, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		devs, _ := k.ReadSubKeyNames(-1)
		k.Close()
		for _, d := range devs {
			dk, err := registry.OpenKey(registry.LOCAL_MACHINE, bus+`\`+d, registry.ENUMERATE_SUB_KEYS)
			if err != nil {
				continue
			}
			insts, _ := dk.ReadSubKeyNames(-1)
			dk.Close()
			for _, in := range insts {
				path := bus + `\` + d + `\` + in
				pk, err := registry.OpenKey(registry.LOCAL_MACHINE, path+`\Device Parameters`, registry.QUERY_VALUE)
				if err != nil {
					continue
				}
				port, _, err := pk.GetStringValue("PortName")
				pk.Close()
				if err != nil {
					continue
				}
				if ik, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE); err == nil {
					if f, _, err := ik.GetStringValue("FriendlyName"); err == nil {
						friendly[port] = strings.TrimSpace(strings.TrimSuffix(f, "("+port+")"))
					}
					ik.Close()
				}
			}
		}
	}
	var out []comPort
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DEVICEMAP\SERIALCOMM`, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	devs, _ := k.ReadValueNames(-1)
	for _, dev := range devs {
		port, _, err := k.GetStringValue(dev)
		if err != nil || !comRe.MatchString(port) {
			continue
		}
		desc := friendly[port]
		if desc == "" {
			desc = driverName(dev)
		}
		out = append(out, comPort{port, desc})
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := strconv.Atoi(out[i].name[3:])
		b, _ := strconv.Atoi(out[j].name[3:])
		return a < b
	})
	return out
}

// \Device\Silabser0 -> CP210x and so on, when there is no friendly name.
func driverName(dev string) string {
	d := strings.ToLower(dev)
	for _, m := range [][2]string{{"silabser", "CP210x"}, {"vcp", "FTDI"}, {"ch34", "CH340"}, {"wch", "CH340"},
		{"prolific", "PL2303"}, {"usbser", "USB CDC"}, {`\serial`, "встроенный"}} {
		if strings.Contains(d, m[0]) {
			return m[1]
		}
	}
	return strings.TrimPrefix(dev, `\Device\`)
}

// serialMain runs the terminal: the args after --serial.
func serialMain(args []string) int {
	port, baud := "", defaultBaud
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--baud" || a == "-baud":
			if i+1 < len(args) {
				baud, _ = strconv.Atoi(args[i+1])
				i++
			}
		case port == "":
			port = strings.ToUpper(a)
		}
	}
	if !comRe.MatchString(port) || baud <= 0 {
		fmt.Println("napi-finder --serial COM5 [--baud 1500000]")
		return 2
	}
	pSetConsoleTitle.Call(uintptr(unsafe.Pointer(u16(fmt.Sprintf("NAPI %s %d", port, baud)))))
	err := serialTerm(port, baud)
	if err != nil {
		fmt.Printf("\r\n%s: %v\r\n", port, err)
	}
	fmt.Print("\r\nНажмите Enter...")
	fmt.Scanln()
	if err != nil {
		return 1
	}
	return 0
}

func serialTerm(port string, baud int) error {
	h, err := windows.CreateFile(u16(`\\.\`+port), windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil,
		windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return fmt.Errorf("не открывается (занят другой программой?): %v", err)
	}
	defer windows.CloseHandle(h)
	var dcb windows.DCB
	dcb.DCBlength = uint32(unsafe.Sizeof(dcb))
	if err := windows.GetCommState(h, &dcb); err != nil {
		return err
	}
	dcb.BaudRate = uint32(baud)
	dcb.Flags = 0x1 | 0x10 | 0x1000 // fBinary, DTR_CONTROL_ENABLE, RTS_CONTROL_ENABLE; no flow control
	dcb.ByteSize, dcb.Parity, dcb.StopBits = 8, 0, 0
	if err := windows.SetCommState(h, &dcb); err != nil {
		return fmt.Errorf("скорость %d не поддерживается: %v", baud, err)
	}
	// A read returns what has come, or after 50 ms with nothing.
	to := windows.CommTimeouts{ReadIntervalTimeout: 0xFFFFFFFF, ReadTotalTimeoutMultiplier: 0xFFFFFFFF, ReadTotalTimeoutConstant: 50}
	if err := windows.SetCommTimeouts(h, &to); err != nil {
		return err
	}

	in, out := windows.Handle(os.Stdin.Fd()), windows.Handle(os.Stdout.Fd())
	var inMode, outMode uint32
	windows.GetConsoleMode(in, &inMode)
	windows.GetConsoleMode(out, &outMode)
	pSetConsoleCP.Call(65001)
	pSetConsoleOutputCP.Call(65001)
	windows.SetConsoleMode(out, outMode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.ENABLE_PROCESSED_OUTPUT)
	// Raw keys: no line editing, no echo, Ctrl+C as a byte; VT sequences for the arrows.
	windows.SetConsoleMode(in, windows.ENABLE_VIRTUAL_TERMINAL_INPUT)
	defer windows.SetConsoleMode(in, inMode)
	defer windows.SetConsoleMode(out, outMode)

	fmt.Printf("%s %d 8N1. Выход: Ctrl+]\r\n\r\n", port, baud)
	done := make(chan error, 1)
	go func() { // the board -> the screen
		buf := make([]byte, 4096)
		for {
			var n uint32
			if err := windows.ReadFile(h, buf, &n, nil); err != nil {
				done <- fmt.Errorf("чтение: %v (переходник отключили?)", err)
				return
			}
			if n > 0 {
				os.Stdout.Write(buf[:n])
			}
		}
	}()
	go func() { // the keys -> the board
		buf := make([]byte, 256)
		for {
			var n uint32
			if err := windows.ReadFile(in, buf, &n, nil); err != nil {
				done <- err
				return
			}
			b := buf[:n]
			if i := strings.IndexByte(string(b), 0x1d); i >= 0 { // Ctrl+]
				if i > 0 {
					windows.WriteFile(h, b[:i], nil, nil)
				}
				done <- nil
				return
			}
			var w uint32
			if err := windows.WriteFile(h, b, &w, nil); err != nil {
				done <- fmt.Errorf("запись: %v", err)
				return
			}
		}
	}()
	err = <-done
	time.Sleep(50 * time.Millisecond)
	return err
}

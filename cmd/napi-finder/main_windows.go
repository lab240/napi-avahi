// napi-finder: a small Windows window that finds the NAPI boards on the LAN.
// Button "Искать", a table (name, IP, model, SoC, OS, role, MAC, signs); double click copies the IP,
// napiconfig / influxdb / grafana / donoff open http://<IP>:<port> of the selected board; "Журнал"
// shows what the last search did. "Сырой поиск" lists everything on TCP 22 of the subnet with the
// signs of a NAPI, for boards without napi-avahi. "SSH" opens a console that asks the login and runs
// ssh <login>@<IP> (the host key is not checked: boards get reflashed). "Консоль ▾" lists the COM ports
// and opens the UART console of a board (1500000 8N1 by default; serial_windows.go). A progress bar runs while searching. Plain Win32.
//
//	napi-finder.exe                     the window
//	napi-finder.exe --console [-v] [-t 5s]   the table in the console (-v: the log)
//	napi-finder.exe --console --raw          the raw search in the console
//	napi-finder.exe --serial COM5 [--baud 115200]   the UART console
//
// Built as a console program (GOOS=windows go build -o napi-finder.exe ./cmd/napi-finder), so the
// console mode prints into the terminal it was started from and the terminal waits for it. Started
// with a double click it owns a console of its own: it hides and drops it and opens the window.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/lab240/napi-avahi/internal/cli"
	"github.com/lab240/napi-avahi/internal/napifind"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	pRegisterClassEx       = user32.NewProc("RegisterClassExW")
	pCreateWindowEx        = user32.NewProc("CreateWindowExW")
	pDefWindowProc         = user32.NewProc("DefWindowProcW")
	pGetMessage            = user32.NewProc("GetMessageW")
	pTranslateMessage      = user32.NewProc("TranslateMessage")
	pDispatchMessage       = user32.NewProc("DispatchMessageW")
	pPostQuitMessage       = user32.NewProc("PostQuitMessage")
	pPostMessage           = user32.NewProc("PostMessageW")
	pSendMessage           = user32.NewProc("SendMessageW")
	pMoveWindow            = user32.NewProc("MoveWindow")
	pLoadCursor            = user32.NewProc("LoadCursorW")
	pLoadIcon              = user32.NewProc("LoadIconW")
	pLoadImage             = user32.NewProc("LoadImageW")
	pGetSystemMetrics      = user32.NewProc("GetSystemMetrics")
	pSetWindowText         = user32.NewProc("SetWindowTextW")
	pEnableWindow          = user32.NewProc("EnableWindow")
	pShowWindow            = user32.NewProc("ShowWindow")
	pIsDialogMessage       = user32.NewProc("IsDialogMessageW")
	pOpenClipboard         = user32.NewProc("OpenClipboard")
	pEmptyClipboard        = user32.NewProc("EmptyClipboard")
	pSetClipboardData      = user32.NewProc("SetClipboardData")
	pCloseClipboard        = user32.NewProc("CloseClipboard")
	pGetDpiForWindow       = user32.NewProc("GetDpiForWindow")
	pSetProcessDPIAware    = user32.NewProc("SetProcessDPIAware")
	pGetConsoleWindow      = kernel32.NewProc("GetConsoleWindow")
	pGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	pFreeConsole           = kernel32.NewProc("FreeConsole")
	pGetModuleHandle       = kernel32.NewProc("GetModuleHandleW")
	pGlobalAlloc           = kernel32.NewProc("GlobalAlloc")
	pGlobalLock            = kernel32.NewProc("GlobalLock")
	pGlobalUnlock          = kernel32.NewProc("GlobalUnlock")
	pCreateFont            = gdi32.NewProc("CreateFontW")
	pInitCommonCtrls       = comctl32.NewProc("InitCommonControlsEx")
	pShellExecute          = shell32.NewProc("ShellExecuteW")
	pSetTimer              = user32.NewProc("SetTimer")
	pGetKeyState           = user32.NewProc("GetKeyState")
	pKillTimer             = user32.NewProc("KillTimer")
)

const (
	wsOverlappedWindow = 0x00CF0000
	wsVisible          = 0x10000000
	wsChild            = 0x40000000
	wsTabStop          = 0x00010000
	wsBorder           = 0x00800000
	wsExClientEdge     = 0x00000200
	cwUseDefault       = 0x80000000

	wmDestroy = 0x0002
	wmSize    = 0x0005
	wmSetFont = 0x0030
	wmNotify  = 0x004E
	wmCommand = 0x0111
	wmTimer   = 0x0113

	pbmSetPos     = 0x0402
	pbmSetRange32 = 0x0406
	wmApp         = 0x8000

	lvsReport        = 0x0001
	lvsSingleSel     = 0x0004
	lvsShowSelAlways = 0x0008
	lvmDeleteAll     = 0x1009
	lvmGetNextItem   = 0x100C
	lvmInsertItem    = 0x104D
	lvmInsertColumn  = 0x1061
	lvmSetItemText   = 0x1074
	lvmSetExStyle    = 0x1036
	lvsExFullRow     = 0x0020
	lvsExGrid        = 0x0001
	lvcfWidth        = 0x0002
	lvcfText         = 0x0004
	lvifText         = 0x0001
	lvniSelected     = 0x0002
	nmDblClk         = ^uint32(2)   // NM_DBLCLK = -3
	nmCustomDraw     = ^uint32(11)  // NM_CUSTOMDRAW = -12
	lvnKeyDown       = ^uint32(154) // LVN_KEYDOWN = -155

	idFind  = 101
	idWeb   = 110 // + index in webs
	idList  = 103
	idLog   = 104 // the "Журнал" button
	idAbout = 109
	idRaw   = 105 // "Сырой поиск"
	idSSH   = 106
	idCOM   = 108
	idPort  = 200 // + index in the COM menu
	idBaud  = 190 // + index in bauds
)

type wndClassEx struct {
	size, style                   uint32
	wndProc                       uintptr
	clsExtra, wndExtra            int32
	instance, icon, cursor, brush uintptr
	menuName, className           *uint16
	iconSm                        uintptr
}

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	x, y    int32
	private uint32
}

type lvColumn struct {
	mask      uint32
	fmt       int32
	cx        int32
	text      *uint16
	textMax   int32
	subItem   int32
	image     int32
	order     int32
	cxMin     int32
	cxDefault int32
	cxIdeal   int32
}

type lvItem struct {
	mask      uint32
	item      int32
	subItem   int32
	state     uint32
	stateMask uint32
	text      *uint16
	textMax   int32
	image     int32
	lParam    uintptr
	indent    int32
	groupID   int32
	columns   uint32
	puColumns uintptr
	piColFmt  uintptr
	group     int32
}

type nmhdr struct {
	from uintptr
	id   uintptr
	code uint32
}

// One line of the table: the IP for the buttons and the cells.
type row struct {
	ip    string
	cells []string
	kind  int // the row colour: kindAvahi, kindRaw (NAPI? of the raw search), 0
}

const (
	kindAvahi = 1
	kindRaw   = 2
)

// Row backgrounds (COLORREF 0x00BBGGRR): napi-avahi light green, NAPI? of the raw search light amber.
var kindColor = map[int]uint32{kindAvahi: 0x00DEF3E8, kindRaw: 0x00D0F3FD}

var (
	hwndSSH, hwndCOM, hwndAbout uintptr

	version = "dev" // make tools: -X main.version=<VERSION>
	ports   []comPort
	baud    = defaultBaud
	bauds   = []int{1500000, 115200}

	hwndMain, hwndFind, hwndRaw, hwndStatus, hwndList, hwndLogBtn, hwndLog, hwndProgress uintptr
	logShown, busy                                                                       bool
	logText                                                                              string
	hwndWeb                                                                              []uintptr
	rows                                                                                 []row
	result                                                                               chan []row
	findErr                                                                              error
	dpi                                                                                  = 96

	// The progress of a search: the raw one counts addresses, the ordinary one goes by time.
	busyText            string
	busyStart           time.Time
	busyTick            int
	scanDone, scanTotal atomic.Int32
	rawSearch           bool
)

func u16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

func send(h uintptr, m uint32, w, l uintptr) uintptr {
	r, _, _ := pSendMessage.Call(h, uintptr(m), w, l)
	return r
}

func px(v int) int { return v * dpi / 96 }

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--serial" || os.Args[1] == "-serial") {
		os.Exit(serialMain(os.Args[2:]))
	}
	if len(os.Args) > 1 {
		code := cli.Run("napi-finder", os.Args[1:])
		if ownConsole() { // started from Explorer with arguments (a shortcut): keep the output readable
			fmt.Print("\nНажмите Enter...")
			fmt.Scanln()
		}
		os.Exit(code)
	}
	if ownConsole() {
		if h, _, _ := pGetConsoleWindow.Call(); h != 0 {
			pShowWindow.Call(h, 0) // SW_HIDE
		}
		pFreeConsole.Call()
	}
	window()
}

// Only this process is attached to its console: it was started by a double click, not from a terminal.
func ownConsole() bool {
	var ids [4]uint32
	n, _, _ := pGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&ids[0])), 4)
	return n == 1
}

func window() {
	runtime.LockOSThread()
	pSetProcessDPIAware.Call()
	icc := [2]uint32{8, 0x1 | 0x20} // ICC_LISTVIEW_CLASSES | ICC_PROGRESS_CLASS
	pInitCommonCtrls.Call(uintptr(unsafe.Pointer(&icc)))

	inst, _, _ := pGetModuleHandle.Call(0)
	cursor, _, _ := pLoadCursor.Call(0, 32512)
	// The NAPI icon from the resources (tools/mkres, group 1), the standard one without them.
	loadIcon := func(metric uintptr) uintptr {
		size, _, _ := pGetSystemMetrics.Call(metric)
		h, _, _ := pLoadImage.Call(inst, 1, 1, size, size, 0) // IMAGE_ICON
		if h == 0 {
			h, _, _ = pLoadIcon.Call(0, 32512)
		}
		return h
	}
	wc := wndClassEx{
		wndProc: syscall.NewCallback(wndProc), instance: inst, cursor: cursor,
		icon: loadIcon(11), iconSm: loadIcon(49), // SM_CXICON, SM_CXSMICON
		brush: 16, className: u16("NapiFinder"), // COLOR_BTNFACE+1
	}
	wc.size = uint32(unsafe.Sizeof(wc))
	pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))

	hwndMain, _, _ = pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(wc.className)), uintptr(unsafe.Pointer(u16("Поиск NAPI в сети"))),
		wsOverlappedWindow|wsVisible, cwUseDefault, cwUseDefault, 1320, 420, 0, 0, inst, 0)
	if pGetDpiForWindow.Find() == nil { // Windows 10+
		if d, _, _ := pGetDpiForWindow.Call(hwndMain); d > 0 {
			dpi = int(d)
		}
	}
	pMoveWindow.Call(hwndMain, uintptr(px(120)), uintptr(px(120)), uintptr(px(1320)), uintptr(px(420)), 1)

	font, _, _ := pCreateFont.Call(uintptr(-px(15)), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(u16("Segoe UI"))))
	child := func(class, text string, style uintptr, id int) uintptr {
		h, _, _ := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(u16(class))), uintptr(unsafe.Pointer(u16(text))),
			wsChild|wsVisible|style, 0, 0, 10, 10, hwndMain, uintptr(id), inst, 0)
		send(h, wmSetFont, font, 1)
		return h
	}
	hwndFind = child("BUTTON", "Искать", wsTabStop|0x1, idFind) // BS_DEFPUSHBUTTON
	hwndRaw = child("BUTTON", "Сырой поиск", wsTabStop, idRaw)
	hwndCOM = child("BUTTON", "Консоль ▾", wsTabStop, idCOM)
	for i, w := range webs {
		hwndWeb = append(hwndWeb, child("BUTTON", w.name, wsTabStop, idWeb+i))
		if i == 0 { // SSH right after napiconfig
			hwndSSH = child("BUTTON", "SSH", wsTabStop, idSSH)
		}
	}
	hwndLogBtn = child("BUTTON", "Журнал", wsTabStop, idLog)
	hwndAbout = child("BUTTON", "Help", wsTabStop, idAbout)
	hwndStatus = child("STATIC", "", 0, 0)
	hwndProgress, _, _ = pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(u16("msctls_progress32"))), 0,
		wsChild, 0, 0, 10, 10, hwndMain, 0, inst, 0)
	send(hwndProgress, pbmSetRange32, 0, 1000)
	// The log of the last search: interfaces, the direct sweep, every answer.
	hwndLog, _, _ = pCreateWindowEx.Call(wsExClientEdge, uintptr(unsafe.Pointer(u16("EDIT"))), 0,
		wsChild|0x4|0x40|0x800|0x00200000, 0, 0, 10, 10, hwndMain, 0, inst, 0) // ES_MULTILINE|ES_AUTOVSCROLL|ES_READONLY|WS_VSCROLL
	mono, _, _ := pCreateFont.Call(uintptr(-px(13)), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(u16("Consolas"))))
	send(hwndLog, wmSetFont, mono, 1)
	hwndList, _, _ = pCreateWindowEx.Call(wsExClientEdge, uintptr(unsafe.Pointer(u16("SysListView32"))), 0,
		wsChild|wsVisible|wsTabStop|lvsReport|lvsSingleSel|lvsShowSelAlways, 0, 0, 10, 10, hwndMain, idList, inst, 0)
	send(hwndList, wmSetFont, font, 1)
	send(hwndList, lvmSetExStyle, 0, lvsExFullRow|lvsExGrid)
	for i, c := range []struct {
		name string
		w    int
	}{{"Имя", 160}, {"IP", 115}, {"Модель", 80}, {"SoC", 70}, {"ОС", 205}, {"Роль", 90}, {"MAC", 125}, {"Признаки", 300}} {
		col := lvColumn{mask: lvcfText | lvcfWidth, cx: int32(px(c.w)), text: u16(c.name)}
		send(hwndList, lvmInsertColumn, uintptr(i), uintptr(unsafe.Pointer(&col)))
	}
	layout()
	actions()
	status("Нажмите «Искать» (платы с napi-avahi) или «Сырой поиск» (всё, что отвечает на порту 22).")

	var m msg
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		if d, _, _ := pIsDialogMessage.Call(hwndMain, uintptr(unsafe.Pointer(&m))); d != 0 {
			continue
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func layout() {
	var rc [4]int32
	user32.NewProc("GetClientRect").Call(hwndMain, uintptr(unsafe.Pointer(&rc)))
	w, h := int(rc[2]), int(rc[3])
	m, bh, bw := px(10), px(30), px(100)
	pMoveWindow.Call(hwndFind, uintptr(m), uintptr(m), uintptr(bw), uintptr(bh), 1)
	pMoveWindow.Call(hwndRaw, uintptr(m+bw+m), uintptr(m), uintptr(bw), uintptr(bh), 1)
	// napiconfig, then SSH, then the other web pages, then the log
	pMoveWindow.Call(hwndCOM, uintptr(m+2*(bw+m)), uintptr(m), uintptr(px(100)), uintptr(bh), 1)
	x := m + 2*(bw+m) + px(100) + 2*m
	for i, hb := range hwndWeb {
		pMoveWindow.Call(hb, uintptr(x), uintptr(m), uintptr(bw), uintptr(bh), 1)
		x += bw + m
		if i == 0 {
			sw := px(70)
			pMoveWindow.Call(hwndSSH, uintptr(x), uintptr(m), uintptr(sw), uintptr(bh), 1)
			x += sw + m
		}
	}
	pMoveWindow.Call(hwndLogBtn, uintptr(x+m), uintptr(m), uintptr(bw), uintptr(bh), 1)
	pMoveWindow.Call(hwndAbout, uintptr(x+m+bw+m), uintptr(m), uintptr(px(70)), uintptr(bh), 1)
	sh := px(22)
	sx := m
	if busy { // the progress bar left of the status text
		pw := px(180)
		pMoveWindow.Call(hwndProgress, uintptr(m), uintptr(m+bh+px(10)), uintptr(pw), uintptr(px(16)), 1)
		sx = m + pw + m
	}
	pMoveWindow.Call(hwndStatus, uintptr(sx), uintptr(m+bh+px(8)), uintptr(max(w-sx-m, 10)), uintptr(sh), 1)
	top := m + bh + px(8) + sh
	rest := max(h-top-m, 20)
	if logShown {
		lh := rest * 2 / 5
		pMoveWindow.Call(hwndList, uintptr(m), uintptr(top), uintptr(max(w-2*m, 10)), uintptr(rest-lh-px(6)), 1)
		pMoveWindow.Call(hwndLog, uintptr(m), uintptr(top+rest-lh), uintptr(max(w-2*m, 10)), uintptr(lh), 1)
	} else {
		pMoveWindow.Call(hwndList, uintptr(m), uintptr(top), uintptr(max(w-2*m, 10)), uintptr(rest), 1)
	}
}

func toggleLog() {
	logShown = !logShown
	sw := 0
	if logShown {
		sw = 5 // SW_SHOW
	}
	pShowWindow.Call(hwndLog, uintptr(sw))
	layout()
}

func status(s string) { pSetWindowText.Call(hwndStatus, uintptr(unsafe.Pointer(u16(s)))) }

// The search runs in the background; the window gets wmApp when it is over.
func find(raw bool) {
	if busy {
		return
	}
	setBusy(true, raw)
	result = make(chan []row, 1)
	go func(ch chan []row) {
		var (
			lg  strings.Builder
			lmu sync.Mutex
		)
		t0 := time.Now()
		napifind.Log = func(f string, a ...any) {
			lmu.Lock()
			fmt.Fprintf(&lg, "%5.2fs  "+f+"\r\n", append([]any{time.Since(t0).Seconds()}, a...)...)
			lmu.Unlock()
		}
		var out []row
		var err error
		if raw {
			var hosts []napifind.Host
			hosts, err = napifind.Scan(func(done, total int) {
				scanDone.Store(int32(done))
				scanTotal.Store(int32(total))
			})
			for _, h := range hosts {
				name, model, soc, osn, role := h.Name, "", "", h.SSH, ""
				if b := h.Board; b != nil {
					model, soc, osn, role = b.TXT["model"], b.TXT["soc"], b.TXT["os"], b.TXT["role"]
				}
				signs := h.Verdict
				if len(h.Signs) > 0 {
					signs += ": " + strings.Join(h.Signs, " ")
				}
				kind := 0
				if h.Board != nil {
					kind = kindAvahi
				} else if h.Verdict == "NAPI?" {
					kind = kindRaw
				}
				out = append(out, row{h.IP, []string{name, h.IP, model, soc, osn, role, h.MAC, signs}, kind})
			}
		} else {
			var list []napifind.Board
			list, err = napifind.Find(3 * time.Second)
			for _, b := range list {
				t := b.TXT
				ip := b.Host
				if len(b.IP) > 0 {
					ip = b.IP[0]
				}
				out = append(out, row{ip, []string{b.Host, strings.Join(b.IP, ", "), t["model"], t["soc"], t["os"], t["role"], t["mac"], "napi-avahi"}, kindAvahi})
			}
		}
		napifind.Log = nil
		lmu.Lock()
		fmt.Fprintf(&lg, "найдено: %d\r\n", len(out))
		for _, r := range out {
			fmt.Fprintf(&lg, "  %s %s %s\r\n", r.cells[0], r.cells[1], r.cells[7])
		}
		if err != nil {
			fmt.Fprintf(&lg, "ошибка: %v\r\n", err)
		}
		logText = lg.String()
		lmu.Unlock()
		findErr = err
		ch <- out
		pPostMessage.Call(hwndMain, wmApp, 0, 0)
	}(result)
}

// While searching: the buttons are off, the progress bar and the dots in the status run.
func setBusy(on, raw bool) {
	busy = on
	pEnableWindow.Call(hwndFind, b2u(!on))
	pEnableWindow.Call(hwndRaw, b2u(!on))
	actions()
	if on {
		rawSearch = raw
		scanDone.Store(0)
		scanTotal.Store(0)
		busyStart = time.Now()
		busyTick = 0
		busyText = "Ищу NAPI (napi-avahi)"
		if raw {
			busyText = "Сырой поиск: проверяю порт 22 по всей подсети"
		}
		send(hwndProgress, pbmSetPos, 0, 0)
		pShowWindow.Call(hwndProgress, 5) // SW_SHOW
		pSetTimer.Call(hwndMain, 1, 150, 0)
		tick()
	} else {
		pKillTimer.Call(hwndMain, 1)
		pShowWindow.Call(hwndProgress, 0)
	}
	layout()
}

func b2u(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

func tick() {
	busyTick++
	dots := []string{"   ", ".  ", ".. ", "..."}[busyTick/2%4]
	pos := 0
	if rawSearch {
		if t := scanTotal.Load(); t > 0 {
			d := scanDone.Load()
			pos = int(d) * 900 / int(t) // the last tenth: names and the napi-avahi answers
			if d >= t {
				pos = 900 + min(int(time.Since(busyStart).Milliseconds())/20, 99)
			}
			status(fmt.Sprintf("%s%s  %d из %d", busyText, dots, d, t))
		} else {
			status(busyText + dots)
		}
	} else {
		pos = min(int(time.Since(busyStart).Milliseconds())/3, 1000) // Find listens 3 s
		status(busyText + dots)
	}
	send(hwndProgress, pbmSetPos, uintptr(pos), 0)
}

// The board buttons work when there are boards in the table, "Журнал" after a search; not while searching.
func actions() {
	on := b2u(!busy && len(rows) > 0)
	pEnableWindow.Call(hwndSSH, on)
	for _, h := range hwndWeb {
		pEnableWindow.Call(h, on)
	}
	pEnableWindow.Call(hwndLogBtn, b2u(!busy && logText != ""))
}

func show(list []row) {
	rows = list
	setBusy(false, false)
	pSetWindowText.Call(hwndLog, uintptr(unsafe.Pointer(u16(logText))))
	send(hwndList, lvmDeleteAll, 0, 0)
	for i, r := range list {
		cells := r.cells
		it := lvItem{mask: lvifText, item: int32(i), text: u16(cells[0])}
		send(hwndList, lvmInsertItem, 0, uintptr(unsafe.Pointer(&it)))
		for c := 1; c < len(cells); c++ {
			it := lvItem{subItem: int32(c), text: u16(cells[c])}
			send(hwndList, lvmSetItemText, uintptr(i), uintptr(unsafe.Pointer(&it)))
		}
	}
	switch {
	case findErr != nil:
		status("Ошибка: " + findErr.Error())
	case len(list) == 0 && rawSearch:
		status("Никто не ответил на порту 22.")
	case len(list) == 0:
		status("NAPI не найдены. На платах нужен сервис napi-avahi (apt install napi-avahi, см. «Help»); платы без него найдёт «Сырой поиск».")
	default:
		sel := lvItem{state: 0x3, stateMask: 0x3}                // the first row: selected and focused
		send(hwndList, 0x102B, 0, uintptr(unsafe.Pointer(&sel))) // LVM_SETITEMSTATE
		if rawSearch {
			status("На порту 22: " + strconv.Itoa(len(list)) + ". «NAPI?»: Linux с признаками NAPI, но без napi-avahi.")
		} else {
			status("Найдено: " + strconv.Itoa(len(list)) + ". Двойной щелчок: SSH; Ctrl+C копирует IP.")
		}
	}
}

// NM_CUSTOMDRAW of the list: the background of a row by its kind. NMLVCUSTOMDRAW on amd64:
// NMHDR (24), dwDrawStage at 24, dwItemSpec at 56, clrText at 80, clrTextBk at 84.
func rowColor(lp uintptr) uintptr {
	switch *(*uint32)(unsafe.Pointer(lp + 24)) {
	case 0x1: // CDDS_PREPAINT
		return 0x20 // CDRF_NOTIFYITEMDRAW
	case 0x10001: // CDDS_ITEMPREPAINT
		i := int(*(*uintptr)(unsafe.Pointer(lp + 56)))
		if i >= 0 && i < len(rows) {
			if c, ok := kindColor[rows[i].kind]; ok {
				*(*uint32)(unsafe.Pointer(lp + 84)) = c
				return 0x2 // CDRF_NEWFONT: use the colours
			}
		}
	}
	return 0
}

func selected() *row {
	i := int32(send(hwndList, lvmGetNextItem, ^uintptr(0), lvniSelected))
	if i < 0 || int(i) >= len(rows) {
		return nil
	}
	return &rows[i]
}

// The web pages of a NAPI, a button each.
var webs = []struct {
	name string
	port int
}{{"napiconfig", 8080}, {"influxdb", 8086}, {"grafana", 3000}, {"donoff", 8090}}

// A new console that asks the login, then ssh to the selected board (Windows Terminal when there is
// one, else cmd; /k keeps the window with the error if ssh fails).
func sshOpen() {
	b := selected()
	if b == nil {
		status("Выберите NAPI в таблице.")
		return
	}
	args := []string{"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=NUL", "-o", "LogLevel=ERROR"}
	// The IP is ours, the login goes through a variable: the line needs no quotes. /v:on: !L! is
	// read when ssh starts, after set /p (%L% would be read with the whole line, before).
	line := "cmd.exe /v:on /k title NAPI " + b.ip + " && set /p L=login: && ssh " + strings.Join(args, " ") + " !L!@" + b.ip
	if wt, err := exec.LookPath("wt.exe"); err == nil {
		line = `"` + wt + `" new-tab --title "NAPI ` + b.ip + `" ` + line
	}
	// CreateProcess without std handles: the new console is the terminal of ssh. (os/exec would
	// give it NUL for stdin and stdout: the password prompt works, the session after it does not.)
	var si windows.StartupInfo
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi windows.ProcessInformation
	cl, _ := windows.UTF16PtrFromString(line)
	if err := windows.CreateProcess(nil, cl, nil, nil, false, windows.CREATE_NEW_CONSOLE, nil, nil, &si, &pi); err != nil {
		status("Не запустился ssh: " + err.Error())
		return
	}
	windows.CloseHandle(pi.Thread)
	windows.CloseHandle(pi.Process)
	status("SSH: " + b.ip)
}

// The COM menu under its button: the ports present now, the speed.
func comMenu() {
	ports = comPorts()
	menu, _, _ := user32.NewProc("CreatePopupMenu").Call()
	defer user32.NewProc("DestroyMenu").Call(menu)
	appendMenu := user32.NewProc("AppendMenuW")
	if len(ports) == 0 {
		appendMenu.Call(menu, 0x1, 0, uintptr(unsafe.Pointer(u16("COM-портов нет: подключите USB-UART")))) // MF_GRAYED
	}
	for i, p := range ports {
		appendMenu.Call(menu, 0, uintptr(idPort+i), uintptr(unsafe.Pointer(u16(p.name+"   "+p.desc))))
	}
	appendMenu.Call(menu, 0x800, 0, 0) // MF_SEPARATOR
	for i, b := range bauds {
		f := uintptr(0)
		if b == baud {
			f = 0x8 // MF_CHECKED
		}
		appendMenu.Call(menu, f, uintptr(idBaud+i), uintptr(unsafe.Pointer(u16("Скорость "+strconv.Itoa(b)))))
	}
	var rc [4]int32
	user32.NewProc("GetWindowRect").Call(hwndCOM, uintptr(unsafe.Pointer(&rc)))
	id, _, _ := user32.NewProc("TrackPopupMenu").Call(menu, 0x100, uintptr(rc[0]), uintptr(rc[3]), 0, hwndMain, 0) // TPM_RETURNCMD
	switch i := int(id); {
	case i >= idPort && i < idPort+len(ports):
		serialOpen(ports[i-idPort])
	case i >= idBaud && i < idBaud+len(bauds):
		baud = bauds[i-idBaud]
		status("Скорость консоли: " + strconv.Itoa(baud) + ". Выберите порт в «Консоль ▾».")
	}
}

// Itself with --serial in a console of its own.
func serialOpen(p comPort) {
	exe, err := os.Executable()
	if err != nil {
		status("Ошибка: " + err.Error())
		return
	}
	line := `"` + exe + `" --serial ` + p.name + " --baud " + strconv.Itoa(baud)
	var si windows.StartupInfo
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi windows.ProcessInformation
	cl, _ := windows.UTF16PtrFromString(line)
	if err := windows.CreateProcess(nil, cl, nil, nil, false, windows.CREATE_NEW_CONSOLE, nil, nil, &si, &pi); err != nil {
		status("Не запустилась консоль: " + err.Error())
		return
	}
	windows.CloseHandle(pi.Thread)
	windows.CloseHandle(pi.Process)
	status("Консоль " + p.name + " " + strconv.Itoa(baud) + ". Выход из неё: Ctrl+]")
}

// The help: TaskDialog (common controls 6, from the manifest) with links that open in the browser.
const helpText = `Поиск плат NAPI в сети, SSH и консоль UART.

«Искать»: платы с сервисом napi-avahi. «Сырой поиск»: всё, что отвечает на порту 22, с признаками NAPI (для плат без napi-avahi). «Консоль»: UART платы через USB-переходник, 1500000 8N1, выход Ctrl+]. napiconfig, SSH, influxdb, grafana, donoff: выбранная в таблице плата; двойной щелчок по строке: SSH, Ctrl+C: копировать IP. Зелёные строки найдены через napi-avahi, жёлтые: «NAPI?» сырого поиска.

Чтобы плата находилась кнопкой «Искать», на ней должен стоять сервис napi-avahi:
    apt install napi-avahi
из репозитория napilab: <a href="https://repo.napilab.ru">repo.napilab.ru</a>
Исходники и подключение репозитория: <a href="https://github.com/lab240/napi-avahi">github.com/lab240/napi-avahi</a>
Свежая версия программы: <a href="https://github.com/lab240/napi-avahi/releases/latest">github.com/lab240/napi-avahi/releases</a>`

const helpFooter = `© 2026 napilab (lab240). Свободная программа под <a href="https://github.com/lab240/napi-avahi/blob/main/LICENSE">лицензией MIT</a>: её можно использовать, копировать, изменять и распространять, в том числе в коммерческих целях, если сохранено это уведомление. Программа предоставляется «как есть», без каких-либо гарантий. Авторы не несут ответственности за любые последствия её использования, в том числе за ущерб, потерю данных и сбои оборудования.`

var helpCallback = syscall.NewCallback(func(hwnd, msg, wp, lp, data uintptr) uintptr {
	if msg == 3 { // TDN_HYPERLINK_CLICKED: lp is the href
		pShellExecute.Call(hwnd, uintptr(unsafe.Pointer(u16("open"))), lp, 0, 0, 1)
	}
	return 0
})

func help() {
	inst, _, _ := pGetModuleHandle.Call(0)
	title, main := u16("Help"), u16("napi-finder "+version)
	text, foot := u16(helpText), u16(helpFooter)
	// TASKDIALOGCONFIG is packed (pshpack1): 160 bytes on amd64, filled by offsets.
	var c [160]byte
	put32 := func(off int, v uint32) { *(*uint32)(unsafe.Pointer(&c[off])) = v }
	put64 := func(off int, v uintptr) { *(*uintptr)(unsafe.Pointer(&c[off])) = v }
	put32(0, 160)
	put64(4, hwndMain)
	put64(12, inst)
	put32(20, 0x1|0x8) // TDF_ENABLE_HYPERLINKS | TDF_ALLOW_DIALOG_CANCELLATION
	put32(24, 0x1)     // TDCBF_OK_BUTTON
	put64(28, uintptr(unsafe.Pointer(title)))
	put64(36, 1) // the main icon: resource group 1 of hInstance
	put64(44, uintptr(unsafe.Pointer(main)))
	put64(52, uintptr(unsafe.Pointer(text)))
	put64(132, uintptr(unsafe.Pointer(foot)))
	put64(140, helpCallback)
	put32(156, 300) // cxWidth, dialog units (~520 px)
	r, _, _ := comctl32.NewProc("TaskDialogIndirect").Call(uintptr(unsafe.Pointer(&c[0])), 0, 0, 0)
	runtime.KeepAlive(title)
	runtime.KeepAlive(main)
	runtime.KeepAlive(text)
	runtime.KeepAlive(foot)
	if r != 0 {
		status("Help не открылся: " + fmt.Sprintf("0x%x", r))
	}
}

func open(i int) {
	b := selected()
	if b == nil {
		status("Выберите NAPI в таблице.")
		return
	}
	url := "http://" + b.ip + ":" + strconv.Itoa(webs[i].port)
	pShellExecute.Call(hwndMain, uintptr(unsafe.Pointer(u16("open"))), uintptr(unsafe.Pointer(u16(url))), 0, 0, 1)
	status("Открываю " + url)
}

func copyText(s string) bool {
	if r, _, _ := pOpenClipboard.Call(hwndMain); r == 0 {
		return false
	}
	defer pCloseClipboard.Call()
	pEmptyClipboard.Call()
	w := syscall.StringToUTF16(s)
	h, _, _ := pGlobalAlloc.Call(0x2, uintptr(len(w)*2)) // GMEM_MOVEABLE
	if h == 0 {
		return false
	}
	p, _, _ := pGlobalLock.Call(h)
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(w)), w)
	pGlobalUnlock.Call(h)
	r, _, _ := pSetClipboardData.Call(13, h) // CF_UNICODETEXT
	return r != 0
}

func wndProc(hwnd, m, wp, lp uintptr) uintptr {
	switch uint32(m) {
	case wmSize:
		if hwndList != 0 {
			layout()
		}
		return 0
	case wmTimer:
		if busy {
			tick()
		}
		return 0
	case wmApp:
		select {
		case list := <-result:
			show(list)
		default:
		}
		return 0
	case wmCommand:
		switch wp & 0xffff {
		case idFind:
			find(false)
		case idSSH:
			sshOpen()
		case idCOM:
			comMenu()
		case idRaw:
			find(true)
		case idAbout:
			help()
		case idLog:
			toggleLog()
		default:
			if i := int(wp&0xffff) - idWeb; i >= 0 && i < len(webs) {
				open(i)
			}
		}
		return 0
	case wmNotify:
		h := (*nmhdr)(unsafe.Pointer(lp))
		if h.id != idList {
			return 0
		}
		switch h.code {
		case nmDblClk:
			if selected() != nil {
				sshOpen()
			}
		case lvnKeyDown: // NMLVKEYDOWN: wVKey after the header
			key := *(*uint16)(unsafe.Pointer(lp + unsafe.Sizeof(nmhdr{})))
			if ctrl, _, _ := pGetKeyState.Call(0x11); key == 'C' && int16(ctrl) < 0 {
				if b := selected(); b != nil && copyText(b.ip) {
					status("Скопировано: " + b.ip)
				}
			}
		case nmCustomDraw:
			return rowColor(lp)
		}
		return 0
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, m, wp, lp)
	return r
}

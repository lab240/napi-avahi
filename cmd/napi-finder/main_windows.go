// napi-finder: a small Windows window that finds the NAPI boards on the LAN.
// Button "Искать", a table (name, IP, model, SoC, OS, role, MAC); double click copies the IP,
// napiconfig / influxdb / grafana / donoff open http://<IP>:<port> of the selected board; "Журнал"
// shows what the last search did. Plain Win32, no libraries.
//
//	napi-finder.exe                     the window
//	napi-finder.exe --console [-v] [-t 5s]   the table in the console (-v: the log)
//
// Built as a console program (GOOS=windows go build -o napi-finder.exe ./cmd/napi-finder), so the
// console mode prints into the terminal it was started from and the terminal waits for it. Started
// with a double click it owns a console of its own: it hides and drops it and opens the window.
package main

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

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
	wmApp     = 0x8000

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
	nmDblClk         = ^uint32(2) // NM_DBLCLK = -3

	idFind = 101
	idWeb  = 110 // + index in webs
	idList = 103
	idLog  = 104 // the "Журнал" button
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

var (
	hwndMain, hwndFind, hwndStatus, hwndList, hwndLogBtn, hwndLog uintptr
	logShown                                                      bool
	logText                                                       string
	hwndWeb                                                       []uintptr
	boards                                                        []napifind.Board
	result                                                        chan []napifind.Board
	findErr                                                       error
	dpi                                                           = 96
)

func u16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

func send(h uintptr, m uint32, w, l uintptr) uintptr {
	r, _, _ := pSendMessage.Call(h, uintptr(m), w, l)
	return r
}

func px(v int) int { return v * dpi / 96 }

func main() {
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
	icc := [2]uint32{8, 0x1} // ICC_LISTVIEW_CLASSES
	pInitCommonCtrls.Call(uintptr(unsafe.Pointer(&icc)))

	inst, _, _ := pGetModuleHandle.Call(0)
	cursor, _, _ := pLoadCursor.Call(0, 32512)
	icon, _, _ := pLoadIcon.Call(0, 32512)
	wc := wndClassEx{
		wndProc: syscall.NewCallback(wndProc), instance: inst, cursor: cursor, icon: icon, iconSm: icon,
		brush: 16, className: u16("NapiFinder"), // COLOR_BTNFACE+1
	}
	wc.size = uint32(unsafe.Sizeof(wc))
	pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))

	hwndMain, _, _ = pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(wc.className)), uintptr(unsafe.Pointer(u16("Поиск NAPI в сети"))),
		wsOverlappedWindow|wsVisible, cwUseDefault, cwUseDefault, 900, 400, 0, 0, inst, 0)
	if pGetDpiForWindow.Find() == nil { // Windows 10+
		if d, _, _ := pGetDpiForWindow.Call(hwndMain); d > 0 {
			dpi = int(d)
		}
	}
	pMoveWindow.Call(hwndMain, uintptr(px(120)), uintptr(px(120)), uintptr(px(900)), uintptr(px(400)), 1)

	font, _, _ := pCreateFont.Call(uintptr(-px(15)), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(u16("Segoe UI"))))
	child := func(class, text string, style uintptr, id int) uintptr {
		h, _, _ := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(u16(class))), uintptr(unsafe.Pointer(u16(text))),
			wsChild|wsVisible|style, 0, 0, 10, 10, hwndMain, uintptr(id), inst, 0)
		send(h, wmSetFont, font, 1)
		return h
	}
	hwndFind = child("BUTTON", "Искать", wsTabStop|0x1, idFind) // BS_DEFPUSHBUTTON
	for i, w := range webs {
		hwndWeb = append(hwndWeb, child("BUTTON", w.name, wsTabStop, idWeb+i))
	}
	hwndLogBtn = child("BUTTON", "Журнал", wsTabStop, idLog)
	hwndStatus = child("STATIC", "", 0, 0)
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
	}{{"Имя", 160}, {"IP", 115}, {"Модель", 80}, {"SoC", 70}, {"ОС", 190}, {"Роль", 100}, {"MAC", 130}} {
		col := lvColumn{mask: lvcfText | lvcfWidth, cx: int32(px(c.w)), text: u16(c.name)}
		send(hwndList, lvmInsertColumn, uintptr(i), uintptr(unsafe.Pointer(&col)))
	}
	layout()
	find()

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
	for i, hb := range hwndWeb {
		pMoveWindow.Call(hb, uintptr(m+(i+1)*(bw+m)), uintptr(m), uintptr(bw), uintptr(bh), 1)
	}
	pMoveWindow.Call(hwndLogBtn, uintptr(m+(len(hwndWeb)+1)*(bw+m)+m), uintptr(m), uintptr(bw), uintptr(bh), 1)
	sh := px(22)
	pMoveWindow.Call(hwndStatus, uintptr(m), uintptr(m+bh+px(8)), uintptr(max(w-2*m, 10)), uintptr(sh), 1)
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
func find() {
	pEnableWindow.Call(hwndFind, 0)
	status("Ищу...")
	result = make(chan []napifind.Board, 1)
	go func(ch chan []napifind.Board) {
		var lg strings.Builder
		t0 := time.Now()
		napifind.Log = func(f string, a ...any) {
			fmt.Fprintf(&lg, "%5.2fs  "+f+"\r\n", append([]any{time.Since(t0).Seconds()}, a...)...)
		}
		list, err := napifind.Find(3 * time.Second)
		napifind.Log = nil
		fmt.Fprintf(&lg, "найдено: %d\r\n", len(list))
		for _, b := range list {
			fmt.Fprintf(&lg, "  %s %s\r\n", b.Host, strings.Join(b.IP, ","))
		}
		if err != nil {
			fmt.Fprintf(&lg, "ошибка: %v\r\n", err)
		}
		logText = lg.String()
		findErr = err
		ch <- list
		pPostMessage.Call(hwndMain, wmApp, 0, 0)
	}(result)
}

func show(list []napifind.Board) {
	boards = list
	pSetWindowText.Call(hwndLog, uintptr(unsafe.Pointer(u16(logText))))
	send(hwndList, lvmDeleteAll, 0, 0)
	for i, b := range list {
		t := b.TXT
		cells := []string{b.Host, strings.Join(b.IP, ", "), t["model"], t["soc"], t["os"], t["role"], t["mac"]}
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
	case len(list) == 0:
		status("NAPI не найдены. Компьютер в той же сети? Брандмауэр пропускает программу?")
	default:
		sel := lvItem{state: 0x3, stateMask: 0x3}                // the first row: selected and focused
		send(hwndList, 0x102B, 0, uintptr(unsafe.Pointer(&sel))) // LVM_SETITEMSTATE
		status("Найдено: " + strconv.Itoa(len(list)) + ". Выберите строку и нажмите кнопку службы; двойной щелчок копирует IP.")
	}
	pEnableWindow.Call(hwndFind, 1)
}

func selected() *napifind.Board {
	i := int32(send(hwndList, lvmGetNextItem, ^uintptr(0), lvniSelected))
	if i < 0 || int(i) >= len(boards) {
		return nil
	}
	return &boards[i]
}

// The web pages of a NAPI, a button each.
var webs = []struct {
	name string
	port int
}{{"napiconfig", 8080}, {"influxdb", 8086}, {"grafana", 3000}, {"donoff", 8090}}

func open(i int) {
	b := selected()
	if b == nil {
		status("Выберите NAPI в таблице.")
		return
	}
	url := "http://" + firstIP(b) + ":" + strconv.Itoa(webs[i].port)
	pShellExecute.Call(hwndMain, uintptr(unsafe.Pointer(u16("open"))), uintptr(unsafe.Pointer(u16(url))), 0, 0, 1)
	status("Открываю " + url)
}

func firstIP(b *napifind.Board) string {
	if len(b.IP) > 0 {
		return b.IP[0]
	}
	return b.Host
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
			find()
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
		if h.id == idList && h.code == nmDblClk {
			if b := selected(); b != nil && copyText(firstIP(b)) {
				status("Скопировано: " + firstIP(b))
			}
		}
		return 0
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, m, wp, lp)
	return r
}

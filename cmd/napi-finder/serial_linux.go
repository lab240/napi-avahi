package main

// The UART console of a NAPI on Linux: napi-finder --serial /dev/ttyUSB0 [--baud 1500000].
// 8N1, no flow control, the terminal in raw mode (keys go to the board as they are), Ctrl+] quits.

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// The debug UART of RK3308 (NAPI-C) and RK3568 (NAPI2).
const defaultBaud = 1500000

var speeds = map[int]uint32{9600: unix.B9600, 19200: unix.B19200, 38400: unix.B38400, 57600: unix.B57600,
	115200: unix.B115200, 230400: unix.B230400, 460800: unix.B460800, 921600: unix.B921600,
	1000000: unix.B1000000, 1500000: unix.B1500000, 2000000: unix.B2000000, 3000000: unix.B3000000}

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
			port = a
		}
	}
	if !strings.HasPrefix(port, "/dev/") || speeds[baud] == 0 {
		fmt.Println("napi-finder --serial /dev/ttyUSB0 [--baud 1500000]")
		return 2
	}
	fmt.Printf("\033]0;NAPI %s %d\007", port, baud) // the terminal title
	err := serialTerm(port, baud)
	if err != nil {
		fmt.Printf("\r\n%s: %v\r\n", port, err)
		fmt.Print("Нажмите Enter...")
		fmt.Scanln()
		return 1
	}
	return 0
}

func serialTerm(port string, baud int) error {
	fd, err := unix.Open(port, unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		if err == unix.EACCES {
			return fmt.Errorf("нет доступа: добавьте себя в группу dialout (sudo usermod -aG dialout $USER) и перезайдите")
		}
		return fmt.Errorf("не открывается (занят другой программой?): %v", err)
	}
	defer unix.Close(fd)
	t, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return err
	}
	raw(t)
	t.Cflag &^= unix.CBAUD | unix.CSIZE | unix.PARENB | unix.CSTOPB | unix.CRTSCTS
	t.Cflag |= speeds[baud] | unix.CS8 | unix.CREAD | unix.CLOCAL
	t.Ispeed, t.Ospeed = speeds[baud], speeds[baud]
	t.Cc[unix.VMIN], t.Cc[unix.VTIME] = 1, 0
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, t); err != nil {
		return fmt.Errorf("скорость %d: %v", baud, err)
	}

	// The terminal: raw, back as it was at the end.
	if old, err := unix.IoctlGetTermios(0, unix.TCGETS); err == nil {
		n := *old
		raw(&n)
		n.Oflag |= unix.OPOST | unix.ONLCR // our own messages keep \n
		unix.IoctlSetTermios(0, unix.TCSETS, &n)
		defer unix.IoctlSetTermios(0, unix.TCSETS, old)
	}
	fmt.Printf("%s %d 8N1. Выход: Ctrl+]\r\n\r\n", port, baud)

	end := make(chan error, 2)
	go func() { // the board -> the screen
		buf := make([]byte, 4096)
		for {
			n, err := unix.Read(fd, buf)
			if err != nil || n == 0 {
				end <- fmt.Errorf("чтение: %v (переходник отключили?)", err)
				return
			}
			os.Stdout.Write(buf[:n])
		}
	}()
	go func() { // the keys -> the board
		buf := make([]byte, 256)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				end <- err
				return
			}
			b := buf[:n]
			if i := strings.IndexByte(string(b), 0x1d); i >= 0 { // Ctrl+]
				unix.Write(fd, b[:i])
				end <- nil
				return
			}
			if _, err := unix.Write(fd, b); err != nil {
				end <- fmt.Errorf("запись: %v", err)
				return
			}
		}
	}()
	err = <-end
	fmt.Print("\r\n")
	return err
}

// cfmakeraw
func raw(t *unix.Termios) {
	t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	t.Oflag &^= unix.OPOST
	t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	t.Cflag &^= unix.CSIZE | unix.PARENB
	t.Cflag |= unix.CS8
}

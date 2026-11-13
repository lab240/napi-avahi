// napi-discover lists the NAPI boards on the LAN in the console: name, IP and the TXT fields of the
// mDNS service _napi._tcp that the napi-avahi package announces. For Linux; on Windows the same is
// napi-finder.exe --console.
//
//	napi-discover [-t 3s] [-v]
package main

import (
	"os"

	"github.com/lab240/napi-avahi/internal/cli"
)

func main() { os.Exit(cli.Run("napi-discover", os.Args[1:])) }

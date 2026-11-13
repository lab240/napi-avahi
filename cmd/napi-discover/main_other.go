//go:build !windows && !linux

// napi-discover is for Windows and Linux; elsewhere use napi-discover.
package main

import "fmt"

func main() { fmt.Println("napi-discover is for Windows and Linux; use napi-discover") }

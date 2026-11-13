//go:build !windows && !linux

// napi-finder is for Windows and Linux; elsewhere use napi-discover.
package main

import "fmt"

func main() { fmt.Println("napi-finder is for Windows and Linux; use napi-discover") }

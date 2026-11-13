//go:build !windows

// napi-finder is a Windows window; elsewhere use napi-discover.
package main

import "fmt"

func main() { fmt.Println("napi-finder is for Windows; use napi-discover") }

//go:build !linux && !windows

package napifind

func arpTable() map[string]string { return map[string]string{} }

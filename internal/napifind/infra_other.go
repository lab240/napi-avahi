//go:build !linux && !windows

package napifind

func infraIPs() map[string]string { return map[string]string{} }

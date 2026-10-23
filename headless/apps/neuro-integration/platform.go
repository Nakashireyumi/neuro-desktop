package main

import "runtime"

// platformKey names the current OS the same way the game profiles and the
// Python controller do ("windows", "linux", "macos").
func platformKey() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "macos"
	default:
		return "linux"
	}
}

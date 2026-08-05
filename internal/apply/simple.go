package apply

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// signalAll sends sig to every process with exactly this name and reports
// how many it reached.
func signalAll(comm string, sig syscall.Signal) int {
	out, err := exec.Command("pgrep", "-x", comm).Output()
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Fields(string(out)) {
		var pid int
		if _, err := fmt.Sscanf(line, "%d", &pid); err == nil {
			if syscall.Kill(pid, sig) == nil {
				n++
			}
		}
	}
	return n
}

func environWithout(key string) []string {
	env := os.Environ()
	kept := env[:0]
	for _, kv := range env {
		if !strings.HasPrefix(kv, key+"=") {
			kept = append(kept, kv)
		}
	}
	return kept
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

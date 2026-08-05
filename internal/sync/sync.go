// Package sync fetches the list of theme names and writes it where Discover
// reads it.
//
// It used to pull all 48 palettes and write them out as inline themes. Nothing
// needs colours any more — every target downloads a file generated for it — so
// what is left is one plain list, one request, and no parsing. The old form
// also went stale without either side knowing: its copy of the palette
// disagreed with the generator about two colours for weeks.
package sync

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Run fetches the theme list at url and writes it to dest, returning how many
// names landed.
func Run(url, dest string) (int, error) {
	if url == "" {
		return 0, fmt.Errorf("no themes_url is configured")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return 0, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("fetching %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", url, err)
	}

	var names []string
	for _, line := range strings.Split(string(body), "\n") {
		name := strings.TrimSpace(line)
		if name == "" || strings.HasPrefix(name, "#") {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		// An empty answer is far more likely a redirect or an error page than
		// a generator that produced no themes, and writing it out would take
		// every theme away from a working installation.
		return 0, fmt.Errorf("%s listed no theme names", url)
	}
	sort.Strings(names)

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return 0, err
	}
	// Write-then-rename: a sync that dies mid-write must not leave a truncated
	// list, which looks exactly like themes having been removed upstream.
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(names, "\n")+"\n"), 0o644); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return 0, err
	}
	return len(names), nil
}

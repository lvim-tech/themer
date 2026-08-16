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
	"sort"
	"strings"
	"time"

	"github.com/lvim-tech/themer/internal/safefile"
	"github.com/lvim-tech/themer/internal/theme"
)

// maxList is as much of the answer as is read.
//
// A list of theme names is a couple of kilobytes; a megabyte is already a
// hundred times what any generator will publish. The client's timeout bounds
// how LONG a body may take, not how BIG it may be, so without a ceiling a
// server that trickles gigabytes stays inside the timeout and is buffered
// whole.
const maxList = 1 << 20

// Run fetches the theme list at url and writes it to dest, returning how many
// names landed.
//
// url should be https. The names it returns are substituted into filesystem
// paths downstream, so on plain http anyone on the path chooses them — see
// theme.ValidName for what that would otherwise be worth.
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
	// One byte past the ceiling, so going over is detected rather than silently
	// truncating the list to a prefix.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxList+1))
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", url, err)
	}
	if len(body) > maxList {
		return 0, fmt.Errorf("reading %s: the answer is larger than %d bytes, which is not a theme list", url, maxList)
	}

	var names []string
	for _, line := range strings.Split(string(body), "\n") {
		name := strings.TrimSpace(line)
		if name == "" || strings.HasPrefix(name, "#") {
			continue
		}
		// Checked here as well as in theme.Discover, so a name that could not
		// be used never reaches the disk in the first place.
		if !theme.ValidName(strings.Fields(name)[0]) {
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

	// Written through a temporary file: a sync that dies mid-write must not
	// leave a truncated list, which looks exactly like themes having been
	// removed upstream.
	if err := safefile.Write(dest, []byte(strings.Join(names, "\n")+"\n"), 0o644); err != nil {
		return 0, err
	}
	return len(names), nil
}

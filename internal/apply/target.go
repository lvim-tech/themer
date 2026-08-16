package apply

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/lvim-tech/themer/internal/config"
	"github.com/lvim-tech/themer/internal/safefile"
	"github.com/lvim-tech/themer/internal/theme"
)

// TargetApplier runs one declarative target from the configuration. The
// compositors ship as built-in targets, but nothing about this type knows
// what a compositor is: any tool whose theme lives on rewritable lines and
// reloads on a command or a signal can be added in config.toml without
// touching Go — that is the point of the declarative form.
type TargetApplier struct {
	t config.Target
	// fetched holds what Run downloaded ahead of time, keyed by URL. Empty is
	// fine: a fetch operation falls back to downloading for itself, so nothing
	// depends on the prefetch having happened.
	fetched map[string][]byte
}

// NewTarget desugars here rather than trusting the caller to have done it.
// Load desugars too, but a target also arrives from a built-in list or a test,
// and one that skipped the step would carry no operations and apply NOTHING —
// silently, reporting success.
func NewTarget(t config.Target) *TargetApplier {
	return &TargetApplier{t: t.Desugar()}
}

func (a *TargetApplier) Name() string { return a.t.Name }

// URLs is every address this target will download for the given theme, so Run
// can fetch them all at once before anything is applied.
//
// Detect is deliberately NOT consulted: it can be slow (pgrep, a file read),
// and a URL fetched for a target that turns out to be skipped costs a few
// kilobytes, while asking first would serialise the very thing being made
// concurrent.
func (a *TargetApplier) URLs(t theme.Theme) []string {
	t = a.themeFor(t)
	var out []string
	for _, op := range a.t.Ops {
		if op.Kind != config.OpFetch {
			continue
		}
		src, err := expandTheme(op.Source, t)
		// Both schemes, spelled out: HasPrefix("http") also said yes to a local
		// path that happened to start with those four letters, and prefetching
		// it meant a request for a file that was never a URL.
		if err != nil || !(strings.HasPrefix(src, "https://") || strings.HasPrefix(src, "http://")) {
			continue
		}
		out = append(out, src)
	}
	return out
}

func (a *TargetApplier) Detect() (bool, string) {
	d := a.t.Detect
	if d.Running != "" {
		if err := exec.Command("pgrep", "-x", d.Running).Run(); err != nil {
			return false, d.Running + " is not running"
		}
	}
	if d.File != "" {
		b, err := os.ReadFile(expandHome(d.File))
		if err != nil {
			return false, "no " + d.File
		}
		if d.Match != "" {
			re, err := regexp.Compile("(?m)" + d.Match)
			if err != nil {
				return false, fmt.Sprintf("detect match %q: %v", d.Match, err)
			}
			if !re.Match(b) {
				if d.Reason != "" {
					return false, d.Reason
				}
				return false, d.File + " does not match " + d.Match
			}
		}
	}
	if d.Command != "" {
		if _, err := exec.LookPath(d.Command); err != nil {
			return false, d.Command + " not in PATH"
		}
	}
	if d.Dir != "" {
		if fi, err := os.Stat(expandHome(d.Dir)); err != nil || !fi.IsDir() {
			return false, "no " + d.Dir
		}
	}
	if !d.Always && d.Running == "" && d.File == "" && d.Command == "" && d.Dir == "" {
		return false, "target declares no detect rule"
	}
	return true, ""
}

// Apply runs the target's operations in order and STOPS at the first failure.
//
// Stopping is the point, not a shortcut: GTK's four steps are one switch —
// copying the stylesheet and then failing to name the theme leaves a desktop
// that is neither the old one nor the new one. An operation list that carried
// on would turn every multi-step target into that.
func (a *TargetApplier) Apply(t theme.Theme) (string, error) {
	t = a.themeFor(t)
	var did []string
	for _, op := range a.t.Ops {
		note, err := a.runOp(op, t)
		if err != nil {
			if !op.Optional {
				return "", err
			}
			// Recorded rather than swallowed: an optional step that did not
			// run is still something the reader wants to know.
			did = append(did, op.Kind+" skipped ("+firstLine(err.Error())+")")
			continue
		}
		if note != "" {
			did = append(did, note)
		}
	}
	return strings.Join(did, ", "), nil
}

// themeFor is the theme this target actually uses: the one being switched to,
// unless the definition pinned a different one.
func (a *TargetApplier) themeFor(t theme.Theme) theme.Theme {
	if a.t.Theme == "" {
		return t
	}
	return theme.FromName(a.t.Theme)
}

func (a *TargetApplier) runOp(op config.Op, t theme.Theme) (string, error) {
	// Every path is templated too: a theme file is usually named after the
	// theme, so {theme} has to expand in `file` and `target`, not only in the
	// values written into them.
	expand := func(s string) (string, error) { return expandTheme(s, t) }
	// expandPath is expand plus the two things a path needs: ~ resolved, and a
	// refusal to climb out of where the definition pointed it.
	expandPath := func(s string) (string, error) {
		v, err := expand(s)
		if err != nil {
			return "", err
		}
		return safePath(v)
	}

	switch op.Kind {
	case config.OpRewrite:
		n, err := a.applyRewrite(op, t)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s (%d)", shortPath(op.File), n), nil

	case config.OpWrite:
		path, err := expandPath(op.File)
		if err != nil {
			return "", err
		}
		content, err := expand(op.Content)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return "", err
		}
		return "wrote " + shortPath(path), nil

	case config.OpFetch:
		src, err := expand(op.Source)
		if err != nil {
			return "", err
		}
		dst, err := expandPath(op.Target)
		if err != nil {
			return "", err
		}
		return a.fetch(src, dst)

	case config.OpLink:
		src, err := expandPath(op.Source)
		if err != nil {
			return "", err
		}
		dst, err := expandPath(op.Target)
		if err != nil {
			return "", err
		}
		return a.link(src, dst)

	case config.OpCopyTree:
		src, err := expandPath(op.Source)
		if err != nil {
			return "", err
		}
		dst, err := expandPath(op.Target)
		if err != nil {
			return "", err
		}
		return copyTree(src, dst)

	case config.OpJSONSet:
		path, err := expandPath(op.File)
		if err != nil {
			return "", err
		}
		value, err := expand(op.Value)
		if err != nil {
			return "", err
		}
		return jsonSet(path, op.Key, value)

	case config.OpCommand:
		argv := make([]string, len(op.Command))
		for i, a := range op.Command {
			v, err := expand(a)
			if err != nil {
				return "", err
			}
			// ~ expands in arguments too, and in argv[0] above all: a target
			// naming a binary by full path — the way tmux does, because it is
			// installed outside the PATH a compositor keybind carries — would
			// otherwise hand exec a literal tilde and fail to find it.
			argv[i] = expandHome(v)
		}
		env := make([]string, len(op.Env))
		for i, e := range op.Env {
			v, err := expand(e)
			if err != nil {
				return "", err
			}
			env[i] = v
		}
		return runCommand(argv, env)

	case config.OpCopy:
		src, err := expandPath(op.Source)
		if err != nil {
			return "", err
		}
		dst, err := expandPath(op.Target)
		if err != nil {
			return "", err
		}
		keep, err := expandPath(op.Keep)
		if err != nil {
			return "", err
		}
		return copyFile(src, dst, keep, op.Unless)

	case config.OpSetLine:
		path, err := expandPath(op.File)
		if err != nil {
			return "", err
		}
		value, err := expand(op.Value)
		if err != nil {
			return "", err
		}
		return setLine(path, op.Regex, value, op.Section)

	case config.OpMoveAside:
		path, err := expandPath(op.File)
		if err != nil {
			return "", err
		}
		dst, err := expandPath(op.Target)
		if err != nil {
			return "", err
		}
		return moveAside(path, dst, op.Unless)

	case config.OpAssemble:
		sources := make([]string, len(op.Sources))
		for i, src := range op.Sources {
			v, err := expandPath(src)
			if err != nil {
				return "", err
			}
			sources[i] = v
		}
		dst, err := expandPath(op.Target)
		if err != nil {
			return "", err
		}
		marker, err := expand(op.Marker)
		if err != nil {
			return "", err
		}
		return assemble(sources, expandHome(dst), marker)

	case config.OpSignal:
		return sendSignal(op.Signal, op.Process)
	}
	return "", fmt.Errorf("%s: unknown operation kind %q", a.t.Name, op.Kind)
}

func (a *TargetApplier) applyRewrite(e config.Op, t theme.Theme) (int, error) {
	file, err := expandTheme(e.File, t)
	if err != nil {
		return 0, err
	}
	path, err := safePath(file)
	if err != nil {
		return 0, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	content := string(b)
	total := 0
	for _, rule := range e.Rules {
		value, err := expandTheme(rule.Value, t)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", a.t.Name, err)
		}
		re, err := regexp.Compile("(?m)" + rule.Regex)
		if err != nil {
			return 0, fmt.Errorf("%s: rule %q: %w", a.t.Name, rule.Regex, err)
		}
		matches := len(re.FindAllString(content, -1))
		if matches == 0 {
			// A rule that matches nothing is a configuration bug, and a
			// silent one would surface as "the switch worked but my bar
			// kept the old colours" — fail loudly instead.
			return 0, fmt.Errorf("%s: rule %q matched nothing in %s", a.t.Name, rule.Regex, e.File)
		}
		content = re.ReplaceAllString(content, value)
		total += matches
	}
	return total, os.WriteFile(path, []byte(content), 0o644)
}

// fetchClient is shared so the whole prefetch reuses connections instead of
// opening one per file.
var fetchClient = &http.Client{Timeout: 30 * time.Second}

// maxTheme is as much of one theme file as is read.
//
// The largest of them is a few tens of kilobytes. The client's timeout bounds
// how LONG a body may take, not how BIG it may be, and prefetch runs these
// concurrently — so without a ceiling one server answering slowly and endlessly
// is buffered whole, once per URL, until the machine runs out of memory.
const maxTheme = 8 << 20

// download reads one URL into memory, naming it in every error: a failure here
// is reported by whichever target wanted the file.
//
// source should be https: a theme file is written straight into a program's
// configuration, so on plain http whoever is on the path writes it.
func download(source string) ([]byte, error) {
	resp, err := fetchClient.Get(source)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", source, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: %s", source, resp.Status)
	}
	// One byte past the ceiling, so going over is detected rather than a
	// truncated theme being written out as if it were the whole file.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTheme+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", source, err)
	}
	if len(body) > maxTheme {
		return nil, fmt.Errorf("reading %s: the answer is larger than %d bytes, which is not a theme file", source, maxTheme)
	}
	return body, nil
}

// fetch downloads source into target.
//
// The generated theme file is the theme: it is overwritten, unlike a link,
// which steps aside for a file the user wrote. There is no way to tell those
// apart here — a downloaded file and a hand-written one look identical on
// disk — so the rule the shell setup scripts had ("replace only a link") does
// not survive the move to fetching. A target whose destination the user edits
// wants `link` and a local store, not `fetch`.
//
// Written through a temporary file: a half-downloaded theme is worse than the
// previous one, and a switch that fails should leave the old theme intact.
func (a *TargetApplier) fetch(source, target string) (string, error) {
	body, ok := a.fetched[source]
	if !ok {
		var err error
		if body, err = download(source); err != nil {
			return "", err
		}
	}

	if err := safefile.Write(target, body, safefile.ModeOf(target, 0o644)); err != nil {
		return "", err
	}
	return fmt.Sprintf("fetched %s (%d B)", shortPath(target), len(body)), nil
}

// copyFile copies src over dst, rescuing what was there ONCE.
//
// keep is where the previous contents go, and unless marks a file as already
// ours. Both matter, and the pair is the whole point: without keep, a first
// switch destroys a stylesheet somebody else wrote; without unless, the second
// switch "rescues" the theme the first one installed, and the real rescue is
// lost. Written once, never overwritten.
func copyFile(src, dst, keep, unless string) (string, error) {
	body, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("nothing to copy at %s: %w", shortPath(src), err)
	}

	rescued := ""
	if keep != "" {
		if old, err := os.ReadFile(dst); err == nil && (unless == "" || !bytes.Contains(old, []byte(unless))) {
			if _, err := os.Stat(keep); os.IsNotExist(err) {
				if err := os.MkdirAll(filepath.Dir(keep), 0o755); err != nil {
					return "", err
				}
				if err := os.WriteFile(keep, old, 0o644); err != nil {
					return "", err
				}
				rescued = " (kept " + shortPath(keep) + ")"
			}
		}
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, body, 0o644); err != nil {
		return "", err
	}
	return shortPath(dst) + rescued, nil
}

// setLine sets one key=value line in an ini-shaped file, whatever state the
// file is in: replace it where it already is, insert it under the section when
// the section exists, and write the section itself when it does not.
//
// The last case is why this is an operation rather than a rewrite. A plain
// regex replacement over a settings.ini with no [Settings] header matches
// nothing — which read as success and changed nothing, in the shell version
// this replaces.
func setLine(path, regex, value, section string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	re, err := regexp.Compile("(?m)" + regex)
	if err != nil {
		return "", fmt.Errorf("rule %q: %w", regex, err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		body := value + "\n"
		if section != "" {
			body = section + "\n" + body
		}
		return "created " + shortPath(path), os.WriteFile(path, []byte(body), 0o644)
	}

	content := string(b)
	switch {
	case re.MatchString(content):
		content = re.ReplaceAllString(content, value)
	case section != "" && strings.Contains(content, section):
		content = strings.Replace(content, section, section+"\n"+value, 1)
	case section != "":
		content = section + "\n" + value + "\n" + content
	default:
		content = value + "\n" + content
	}
	return shortPath(path), os.WriteFile(path, []byte(content), 0o644)
}

// moveAside gets somebody else's file out of the way, keeping it.
//
// A user stylesheet loads at PRIORITY_USER (800) and outranks a theme's 200, so
// anything left there quietly overrides ours — the switch appears to work and
// the colours do not change. Moved, never deleted, and only when it is not
// already ours.
func moveAside(path, target, unless string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", nil // nothing in the way
	}
	if unless != "" && bytes.Contains(b, []byte(unless)) {
		return "", nil // already ours
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(path, target); err != nil {
		return "", err
	}
	return "moved aside " + shortPath(path), nil
}

// assemble joins sources into target, in order.
//
// It exists because some programs read exactly one file and offer no include:
// starship and lazydocker both do, so their theme cannot be a file they are
// pointed at — it has to be glued to the user's own. A shell script did that on
// every shell start, which is precisely why neither configuration could belong
// to the user: the product and the parts lived in the same directory and the
// script rewrote the product.
//
// The marker is written first AND is the permission to overwrite: a target that
// exists without it was written by hand and is left alone. Editing the product
// instead of the parts is a mistake, but it is not one to answer by destroying
// the edit.
func assemble(sources []string, target, marker string) (string, error) {
	if marker != "" {
		if old, err := os.ReadFile(target); err == nil && !bytes.Contains(old, []byte(marker)) {
			return "kept " + shortPath(target) + " (not assembled by us)", nil
		}
	}

	var out bytes.Buffer
	if marker != "" {
		out.WriteString(marker + "\n")
	}
	for _, src := range sources {
		b, err := os.ReadFile(src)
		if err != nil {
			return "", fmt.Errorf("assembling %s: %w", shortPath(target), err)
		}
		out.Write(b)
		// Joined with a newline where a part does not end in one, or the last
		// line of one file and the first of the next become a single line.
		if n := len(b); n > 0 && b[n-1] != '\n' {
			out.WriteByte('\n')
		}
	}

	if err := safefile.Write(target, out.Bytes(), safefile.ModeOf(target, 0o644)); err != nil {
		return "", err
	}
	return "assembled " + shortPath(target) + fmt.Sprintf(" (%d parts)", len(sources)), nil
}

// link points dst at src, replacing only what this tool may replace.
//
// A real file the user wrote is theirs: it is left alone and reported, not
// overwritten. That rule is copied from the shell setup scripts this replaces,
// where it was written once per package and got it right every time.
func (a *TargetApplier) link(src, dst string) (string, error) {
	if _, err := os.Stat(src); err != nil {
		return "", fmt.Errorf("%s: nothing to link at %s", a.t.Name, shortPath(src))
	}
	if fi, err := os.Lstat(dst); err == nil && fi.Mode()&os.ModeSymlink == 0 {
		return "kept " + shortPath(dst) + " (not a link)", nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	// Removed first: os.Symlink will not replace an existing name, and a stale
	// link pointing at the previous theme is exactly what has to go.
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Symlink(src, dst); err != nil {
		return "", err
	}
	return shortPath(dst) + " → " + filepath.Base(src), nil
}

// copyTree copies src over dst, leaving anything already there that src does
// not carry. os.CopyFS refuses to overwrite, so the destination is cleared
// first — a theme's asset directory is wholly owned by the theme.
func copyTree(src, dst string) (string, error) {
	if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("no directory to copy at %s", shortPath(src))
	}
	if err := os.RemoveAll(dst); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		return "", err
	}
	return "copied " + shortPath(dst), nil
}

// jsonSet sets one key and leaves every other byte of the document as it was.
//
// The document belongs to the program, not to the theme: it holds settings we
// did not come for, in an order and a formatting somebody chose, often with
// comments explaining them. So the key is edited as TEXT — see jsonedit.go —
// and nothing else in the file is even rewritten. A document that will not
// parse is reported, never replaced.
//
// The key may be a dotted path — zed names its theme at theme.dark, inside an
// object that also carries mode and the light choice, and replacing the object
// wholesale would take both with it. Intermediate objects are created when
// missing; anything already there that is NOT an object stops the write rather
// than being overwritten, because a scalar where a path expects an object means
// the document is not shaped the way the definition believes.
//
// A key that genuinely contains a dot cannot be addressed this way. No program
// here has one, and the alternative — an escape — would be a syntax to learn
// for a case that does not exist.
func jsonSet(path, key, value string) (string, error) {
	parts := strings.Split(key, ".")
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}

	src, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}

	var out []byte
	if len(bytes.TrimSpace(src)) == 0 {
		// No document to preserve: write the one the key implies, indented,
		// because a program's settings file is read by people too.
		if out, err = json.MarshalIndent(nested(parts, value), "", "  "); err != nil {
			return "", err
		}
		out = append(out, '\n')
	} else if out, err = setJSONKey(src, parts, encoded); err != nil {
		return "", fmt.Errorf("%s: %w", shortPath(path), err)
	}

	// Written through a temporary file: a truncated document costs every
	// setting in it, not just the one being set. With the mode it already had,
	// so a settings file somebody made private stays private.
	if err := safefile.Write(path, out, safefile.ModeOf(path, 0o644)); err != nil {
		return "", err
	}
	return key + " → " + value, nil
}

// nested is the document a dotted key implies when there is none yet:
// theme.dark becomes {"theme": {"dark": value}}.
func nested(parts []string, value string) map[string]any {
	doc := map[string]any{}
	at := doc
	for _, p := range parts[:len(parts)-1] {
		m := map[string]any{}
		at[p] = m
		at = m
	}
	at[parts[len(parts)-1]] = value
	return doc
}

func runCommand(argv, env []string) (string, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	if len(env) > 0 {
		// Appended to the inherited environment, and last: a target that sets
		// THEME means to override whatever the calling shell had.
		cmd.Env = append(os.Environ(), env...)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("%s: %s", strings.Join(argv, " "), firstLine(string(out)))
	}
	return strings.Join(argv, " "), nil
}

func sendSignal(name, process string) (string, error) {
	sig, ok := signals[strings.ToUpper(name)]
	if !ok {
		return "", fmt.Errorf("unknown signal %q", name)
	}
	if n := signalAll(process, sig); n > 0 {
		return fmt.Sprintf("SIG%s → %d × %s", strings.ToUpper(name), n, process), nil
	}
	return "", nil // nothing running: the edit still counts
}

// expandTheme resolves the placeholders a target may use.
//
// {theme} is the canonical name; {family} and {variant} are its two halves, in
// lower case, and {Family} / {Variant} capitalised the way the canonical name
// spells them. The halves exist so a definition can follow the switch WITHOUT
// following it exactly:
//
//	source = '…/extras/eza/Lvim{Family}_light.yml'
//
// takes whichever family was switched to and pins the light variant of it — a
// terminal that has to stay readable while everything else goes dark.
//
// Colour placeholders are gone with the palette: every target downloads a file
// the generator produced, so nothing here composes a colour any more.
//
// An unknown placeholder is an error rather than a literal, because a rule
// that wrote "{focus}" into a config file would look like it had worked.
var placeholder = regexp.MustCompile(`\{([a-zA-Z][a-zA-Z0-9_-]*)\}`)

func expandTheme(tpl string, t theme.Theme) (string, error) {
	var missing string
	out := placeholder.ReplaceAllStringFunc(tpl, func(m string) string {
		switch key := m[1 : len(m)-1]; key {
		case "theme":
			return t.Name
		case "family":
			return t.Family
		case "Family":
			return capitalise(t.Family)
		case "variant":
			return t.Variant
		case "Variant":
			return capitalise(t.Variant)
		case "appearance":
			return appearance(t.Variant)
		case "Appearance":
			return capitalise(appearance(t.Variant))
		default:
			missing = key
		}
		return m
	})
	if missing != "" {
		return "", fmt.Errorf("unknown placeholder {%s}: {theme}, {family}, {Family}, {variant}, {Variant}, {appearance} and {Appearance} are expanded", missing)
	}
	return out, nil
}

// A theme has four variants; everything that asks about appearance knows two.
// GNOME's color-scheme takes prefer-dark or prefer-light, zed's theme mode
// takes dark or light, and the portal reports one of those two — none of them
// has a word for `darker` or `soft`.
//
// MEASURED on the Nord family, whose backgrounds are #21262f dark, #1b1f26
// darker, #262b36 soft and #dfdfdf light: only `light` is light. The three
// others differ from one another in the neutrals alone, never in which side of
// the line they sit on.
//
// An unknown variant counts as dark because every family here is built dark
// with one light variant, so dark is the answer that is right by default.
func appearance(variant string) string {
	if variant == "light" {
		return "light"
	}
	return "dark"
}

func capitalise(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

var signals = map[string]syscall.Signal{
	"USR1": syscall.SIGUSR1,
	"USR2": syscall.SIGUSR2,
	"HUP":  syscall.SIGHUP,
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return home + p[1:]
	}
	return p
}

// safePath expands ~ and refuses a path that climbs out of where it was
// pointed.
//
// The braces to theme.ValidName's belt: names are checked where they enter, and
// the paths built out of them are checked again where they are used. A `..` is
// never something a definition needs — the file it means can always be named
// directly — so the one way one appears here is a value that came from
// somewhere it should not have.
func safePath(p string) (string, error) {
	p = expandHome(p)
	for _, seg := range strings.Split(p, string(os.PathSeparator)) {
		if seg == ".." {
			return "", fmt.Errorf("path %q climbs out of the directory it was pointed at", p)
		}
	}
	return p, nil
}

func shortPath(p string) string {
	home, _ := os.UserHomeDir()
	return strings.Replace(p, home, "~", 1)
}

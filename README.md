# themer

One switch for the whole desktop. Pick a theme once and every corner follows:
the `~/.theme` state file, every clipack-installed tool, kitty (live, via
SIGUSR1), tmux, GTK 3+4 through `lvim-gtk-select`, waybar (live, via
SIGUSR2), and whichever compositor is running — mango, Hyprland or niri.

```
themer                  # pick from the list (TUI)
themer LvimNord_dark    # switch without the TUI — for keybindings and scripts
themer --list           # print the theme names, ● marks the current one
```

The theme list and every colour come from one place: the generated
`lvim-gtk/palettes/*.scss` files. Nothing here defines a colour of its own —
unless the configuration does. A theme can also live entirely in
`config.toml` as values: a new name adds it to the list, a known name
replaces the file palette. Everything that reads the palette — the preview
dots, the compositors, every `{placeholder}` — follows; appliers that need a
per-theme file (kitty, tmux, waybar) still fail loudly until that file
exists too.

```toml
[[themes]]
name = "LvimCustom_dark"

[themes.palette]
bg  = "#101418"
fg  = "#c8ccd4"
red = "#e06c75"
# … any keys the roles and your rules refer to
```

## How a switch works

Appliers run in order; each is detected first and skipped with a reason when
its tool is absent — a machine that runs only mango sees Hyprland and niri as
`○ not running`, never as failures. One broken target does not stop the rest.

Two appliers deserve a note:

- **clipack tools** re-sources `~/clipack/configs/clipack.sh` with the new
  `$THEME`, which is exactly what a fresh shell does at startup — so yazi,
  bat and friends are re-themed now instead of at the next login.
- **wezterm** is only touched when an uncommented `color_scheme` line exists.
  A config that runs on a hand-built palette (`colors = custom`) is
  deliberately left alone.

## Declarative targets

What gets rewritten and how it gets reloaded is configuration, not code.
The three compositors and waybar ship as built-in targets; anything
themeable by a line rewrite plus a reload command can be added in
`~/.config/themer/config.toml` without touching Go. A target with a
built-in's name replaces it.

waybar needed one change to become switchable: it freezes its `--style` path
at launch, so the compositors now start it with a permanent
`~/.config/waybar/current.css` whose single `@import` the target rewrites
before SIGUSR2 makes the running bar re-read it.

```toml
[[targets]]
name = "dunst"

[targets.detect]
running = "dunst"                       # skip unless dunst is up

[[targets.edit]]
file = "~/.config/dunst/dunstrc"

[[targets.edit.rules]]
regex = '^(\s*frame_color = )"#\S+"'
value = '${1}"#{focus}"'

[[targets.reload]]
command = ["dunstctl", "reload"]        # or: signal = "USR2", process = "..."
```

Rule values take regex group references (`${1}`) and colour placeholders:
`{focus}` resolves through the role mapping below, `{green-dark}` straight
from the palette, `{theme}` to the canonical theme name. Colours expand to
bare `rrggbb` — the template owns the surrounding syntax (`0x…ff`, `rgba(…)`,
`"#…"`). A rule that matches nothing fails the switch loudly; a silent skip
would surface weeks later as "my bar kept the old colours".

## Roles

Compositor colours are not in the palettes by name — border, focus and
friends are desktop concepts. The mapping is configurable:

```toml
[roles]
focus  = "yellow-dark"
border = "green-dark"
urgent = "red"
```

Defaults cover `focus`, `border`, `urgent`, `scratchpad`, `global`,
`maximizescreen`, `overlay` and `root`.

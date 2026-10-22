# lux-mood

A Claude Code mod that turns a [Luxafor Flag](https://luxafor.com/flag-usb-busylight-availability-indicator/) USB light into a status light for Claude. It ships with a prebuilt `lux` binary, so there is nothing to install besides the plugin.

| Claude is… | The light |
|---|---|
| idle, waiting for you | green rain (`matrix`) |
| thinking | breathing Claude orange (`pulse claude`) |
| editing a file (Edit, Write, MultiEdit, NotebookEdit) | Claude orange racing round the LEDs (`chase claude`) |
| running any other tool | cyan wave (`wave cyan`) |
| waiting for a permission answer | red and blue (`police`) |
| session ends | off |

Every state is shown for at least 1.5 seconds, so a tool call that takes 50ms still registers. With no Flag plugged in the mod does nothing.

## Requirements

- macOS, Apple silicon or Intel. The bundled binary is universal. No Go, no Homebrew, nothing else.
- A Luxafor Flag plugged **directly into the Mac**. A monitor's USB hub may not pass it through at all.
- Claude Code 2.1.275 or later.

## Install

At the prompt of a terminal session:

```
/plugin install lux-mood --marketplace kenanbendz/claude-mod-luxafor
```

Answer `y` to add the marketplace, then pick a scope (user is the usual one). From a shell:

```bash
claude plugin install lux-mood --marketplace kenanbendz/claude-mod-luxafor
```

In the desktop app, where `/plugin` does not run: click **+** next to the prompt box, then **Plugins**, **Add marketplace** with `kenanbendz/claude-mod-luxafor`, then **Add plugin**.

While this repository is private, installing needs read access to it on GitHub and a git credential for github.com on the machine (an SSH key, or `gh auth login` followed by `gh auth setup-git`).

## Updating

The plugin has no pinned version, so every commit on `main` is a new version. To receive them automatically, turn on auto-update for this marketplace once: in a terminal session run `/plugin`, open **Marketplaces**, select `claude-mod-luxafor`, and choose **Enable auto-update**. Claude Code then refreshes it in the background shortly after each session starts, and the new version loads on the next launch.

Without auto-update, pull the latest with:

```bash
claude plugin update lux-mood@claude-mod-luxafor
```

## How it works

`hooks/register.ts` hooks the engine's session start, turn start, tool calls, permission notifications, turn complete and session end. Each state change runs `bin/lux mood <effect>`, which stops the effect that is running and starts the next one detached from Claude Code. A file lock in the temp folder is how it finds the running one, so it only ever stops its own kind. Bursts of changes collapse to the latest state, one switch at a time, so two effects never fight for the light.

To change what a state looks like, edit the `MOOD` table at the top of `hooks/register.ts`. Any colour or effect from `bin/lux list` works.

## The lux CLI on its own

`bin/lux` is a small CLI you can use without Claude. Copy it somewhere on your PATH if you like.

```
lux red                 solid colour (lux list shows them all; claude is Claude orange)
lux max                 every LED flat out white
lux max strobe          strobe at full white
lux pulse <colour>      breathe, never below 30%
lux strobe <colour>     70ms on, 50ms off
lux rainbow             endless effects: rainbow police disco chase fire rave wave heartbeat thunder matrix
lux chase <colour>      chase and wave take a colour too
lux mood <effect>       swap the running effect for this one, in the background
lux mood off            stop it and switch the light off
lux off
```

Effects run until Ctrl-C, which switches the light off.

## Building lux from source

Only needed if you change `lux/main.go`. Needs Go 1.22+ and the Xcode command line tools (the HID library uses cgo).

```bash
cd lux && go build -o ../bin/lux .
```

For a universal binary, build once per architecture and join them:

```bash
cd lux
GOARCH=arm64 CGO_ENABLED=1 go build -o /tmp/lux-arm64 .
GOARCH=amd64 CGO_ENABLED=1 go build -o /tmp/lux-amd64 .
lipo -create -output ../bin/lux /tmp/lux-arm64 /tmp/lux-amd64
codesign -s - -f ../bin/lux
```

## Known limits

- One light: with several Claude Code sessions open, the last state change wins, and a session ending switches it off.
- macOS only. Linux would need a build against hidapi plus a udev rule; Windows is untested.
- The Flag occasionally rejects a USB write (about 1 in 150); `lux` retries, so you will not notice.

## License

MIT

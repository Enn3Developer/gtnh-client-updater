# gtnh-update

[![CI](https://github.com/Enn3Developer/gtnh-client-updater/actions/workflows/ci.yml/badge.svg)](https://github.com/Enn3Developer/gtnh-client-updater/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/Enn3Developer/gtnh-client-updater)](https://github.com/Enn3Developer/gtnh-client-updater/releases/latest)

A small launcher and updater for **GT: New Horizons** in
[Prism Launcher](https://prismlauncher.org/). Play your GTNH instance, join your server with
one key, and update to a new version **without making a new instance** — your worlds,
keybinds, maps, settings and config tweaks stay where they are.

- Plays your instance through Prism (Prism still does the login and Java), and can join
  your server straight away
- Lets you set memory, Java and window size without digging through Prism's menus
- Can undo the last update if the new version doesn't work out
- Finds your GTNH instance and the version it's on by itself
- Downloads the official pack from the GTNH servers
- Updates only what the new version actually changed, and backs up everything it replaces
- Can keep your server's extra mods in sync, if your server has any
- Keeps itself up to date

It's a small program that runs in a terminal window, with arrow keys and Enter — no setup.

## How to use it

1. Download the file for your computer from the
   [latest release](https://github.com/Enn3Developer/gtnh-client-updater/releases/latest):

   | Your computer | File |
   |---|---|
   | Windows | `gtnh-update-windows-amd64.exe` |
   | Mac (Apple chip / Intel) | `gtnh-update-darwin-arm64` / `gtnh-update-darwin-amd64` |
   | Linux | `gtnh-update-linux-amd64` |

2. Run it (on Windows, double-click it). You'll see your GTNH instances, with a card for the
   selected one: the version it's on, whether an update is out, when you last played, your
   server and what an undo would go back to.
3. Pick an instance with the arrow keys and press a key:

   | Key | What it does |
   |---|---|
   | **Enter** or **p** | Play — starts the instance in Prism |
   | **j** | Play & join — starts it and joins your server (set the server in Settings first) |
   | **u** | Update — pick a version (the recommended one is already selected) and press **Enter**. It shows you exactly what will happen before it changes anything. Close Minecraft first |
   | **s** | Settings |
   | **b** | Undo the last update |
   | **n** | Make a new GTNH instance |
   | **a** | Also show instances that aren't GTNH |
   | **v** | Update gtnh-update itself, when a new version is out |
   | **q** | Quit |

**Playing.** gtnh-update finds Prism by itself and asks it to start the game, so Prism still
handles your Microsoft login and Java. By default it then stays open and tells you when the
game is running and when it closed. If the game doesn't show up after a minute and a half, have
a look at Prism's window — it may be asking you something. You can quit gtnh-update at any
time; the game keeps running.

**Settings** (**s**) are per instance: the server to join, your server's extra-mods link,
memory for the game, Java arguments, which Java to use and the window size. Leave a value
empty to go back to Prism's default. These are the same settings Prism has, so if Prism is
open while you change them, restart Prism. Two settings are for gtnh-update itself: whether
it stays open or quits after you press Play, and where Prism is, if it can't find it on its
own.

**Undo** (**b**) puts the instance back on the version it was on before the last update:
files the update added are removed and the files it replaced are put back. Your worlds and
settings stay as they are. Only the last update can be undone. Close Minecraft first.

Want a fresh GTNH instance instead? Press **n** (*new instance*) on the home screen (or just
run it if Prism has no instances yet), pick a version and a name.

> **Windows** may say "Windows protected your PC" the first time, because the file isn't
> signed: click *More info* → *Run anyway*.
> **macOS:** right-click the file → *Open* the first time. From Terminal you may need
> `chmod +x gtnh-update-darwin-*` first.

## What happens to your files

| What | What happens |
|---|---|
| Worlds, screenshots, JourneyMap maps, keybinds and video settings, server list, shaders | Never touched |
| Mods and launcher files that come with GTNH | Replaced with the new version's. Mods you **disabled in Prism stay disabled** |
| Mods you added yourself | Left in place — you'll be reminded to check they work with the new version |
| Config files you never changed | Updated to the new version's |
| Config files you changed, that GTNH didn't | Yours are kept |
| Config files both you and GTNH changed | You choose: take GTNH's new one (yours goes to the backup), keep yours (GTNH's is saved next to it as `<name>.mcnew`), or decide file by file |
| Java path, memory and other Prism settings | Kept. Only the version number in the instance name is updated |

Everything that gets replaced or removed is moved to a `.gtnh-updater/backup-…` folder
inside the instance first (the latest backup is kept). If something goes wrong halfway, all
changes are undone automatically. If the updater itself gets interrupted (say, the window is
closed mid-update), just run it again: it picks up where it stopped.
If it gets killed while creating a new instance, it may leave that instance's folder behind;
delete it from Prism's instances folder before using the name again.

A few config files changed both by you and by the new version are normal after an update,
even if you never edited a config: many GTNH mods rewrite their own config files when the
game starts.

## Server extra mods

Some servers add a few mods on top of GTNH. The first time you update an instance, you're
asked for your server's extra-mods link (a `.zip` of jars). Paste it, or leave it empty if
you don't have one. It's remembered for that instance, and every update after that installs
new server mods and removes ones the server dropped. Only mods that came from that link are
ever removed. Press **m** on the version list, or use Settings, to change or remove the link.

Server owners: the link should serve a flat `.zip` of `.jar` files over HTTPS. A `404`
means "no extra mods right now".

## For power users

```text
gtnh-update -list                                   # your instances + all GTNH versions
gtnh-update -instance "GTNH" -version latest-stable -yes
gtnh-update -create -version latest-stable -name "GTNH" -yes   # a brand-new instance
gtnh-update -play -yes -instance "GTNH"             # start it in Prism
gtnh-update -play -yes -instance "GTNH" -version latest   # update, then start it
gtnh-update -self-update
```

| Flag | Meaning |
|---|---|
| `-instance` | Instance folder, or its name in Prism |
| `-version` | A GTNH version (`2.8.4`), `latest` or `latest-stable` |
| `-installed` | The version the instance is on now, if it's detected wrong |
| `-server-mods` | Server extra-mods link, or `none`. Default: what the instance remembers |
| `-prism-dir` | Prism's data folder (the one with `prismlauncher.cfg`), if it isn't found automatically |
| `-configs` | Config files both you and GTNH changed: `new` (default, yours backed up) or `mine` (GTNH's saved as `.mcnew`). Without `-yes` it picks the preselected answer |
| `-create` | Create a new instance with `-version` instead of updating one |
| `-name` | Name for the new instance. Default: `GT New Horizons <version>` |
| `-play` | Start the instance in Prism. With `-yes` it needs `-instance`, starts it and exits; add `-version` to update first. Without `-yes` it opens gtnh-update and starts `-instance` right away |
| `-yes` | Don't ask, just do it: updating needs `-instance` and `-version`, `-create` needs `-version` (plus optional `-name`, `-server-mods`, and `-configs` for updates), `-play` needs `-instance` |
| `-no-update-check` | Don't check GitHub for a newer gtnh-update |
| `-V` | Print the gtnh-update version |

Prism's data folder is found automatically in its standard place on Windows, macOS and
Linux (including Flatpak), or next to `gtnh-update` for portable installs. To start the
game, gtnh-update looks for the Prism program the same way (Flatpak, portable, then the
usual install places); if it can't find it, set *Prism Launcher location* in Settings.
Launcher-wide settings are saved in `gtnh-update/config.json` in your user config folder.

## How it works

It uses the same approach as Linux package managers use for config files. For every file,
it compares three versions: the one the instance was installed with (the *baseline*), the
one on disk, and the one in the new pack. Only files GTNH ships are ever touched.

- **Baseline without the space.** The baseline is stored in
  `.gtnh-updater/state.json` as the size and CRC-32 of each file, not as a copy. On an
  instance this tool never updated before, it's rebuilt from the installed version's
  official pack by reading only the zip's file index (a few MB via HTTP range requests),
  not the whole ~700 MB archive.
- **Version detection.** In order: the updater's own notes, the
  `changelog from X to Y.md` file every GTNH pack ships, then the instance name. Before
  updating it checks that the detected version's mods are actually on disk, and warns if
  they aren't.
- **Only official downloads.** Versions and download links come from the official
  [GTNH manifest](https://downloads.gtnewhorizons.com/versions.json), and downloads are
  only accepted from `downloads.gtnewhorizons.com` over HTTPS.
- **Signed self-updates.** It checks this repository's latest release and only installs it
  if the release's `checksums.txt` carries a valid ed25519 signature from the maintainers'
  key (built into the program) and the new binary matches its checksum. Every release also
  has a GitHub build provenance attestation:
  `gh attestation verify <file> --repo Enn3Developer/gtnh-client-updater`.

## Building

Needs Go (see `go.mod` for the version).

```text
go test ./...
./build.sh 1.2.3    # static binaries for Windows, macOS and Linux (amd64 + arm64) in dist/
```

Releases are built by GitHub Actions: pushing a tag like `v1.2.3` runs the tests on all
three systems and publishes the binaries with a signed `checksums.txt`. Signing needs the
`RELEASE_SIGNING_KEY` repository secret (see `internal/cmd/sign`); forks building their own
releases must generate their own key. The AUR package is built from
[`packaging/aur/PKGBUILD`](packaging/aur/PKGBUILD) (`makepkg -si` in that folder builds it
locally).

## License

[MIT](LICENSE)

# gtnh-update

[![CI](https://github.com/Enn3Developer/gtnh-client-updater/actions/workflows/ci.yml/badge.svg)](https://github.com/Enn3Developer/gtnh-client-updater/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/Enn3Developer/gtnh-client-updater)](https://github.com/Enn3Developer/gtnh-client-updater/releases/latest)

Update your **GT: New Horizons** modpack in [Prism Launcher](https://prismlauncher.org/) to a
new version **without making a new instance** — your worlds, keybinds, maps, settings and
config tweaks stay where they are.

- Finds your GTNH instance and the version it's on by itself
- Downloads the official pack from the GTNH servers
- Updates only what the new version actually changed, and backs up everything it replaces
- Can keep your server's extra mods in sync, if your server has any
- Keeps itself up to date

It's a small program that runs in a terminal window, with arrow keys and Enter — no setup.

## How to use it

1. **Close Minecraft.**
2. Download the file for your computer from the
   [latest release](https://github.com/Enn3Developer/gtnh-client-updater/releases/latest):

   | Your computer | File |
   |---|---|
   | Windows | `gtnh-update-windows-amd64.exe` |
   | Mac (Apple chip / Intel) | `gtnh-update-darwin-arm64` / `gtnh-update-darwin-amd64` |
   | Linux | `gtnh-update-linux-amd64` |

3. Run it (on Windows, double-click it). Pick a version — the recommended one is already
   selected — and press **Enter**. It shows you exactly what will happen before it
   changes anything.

Want a fresh GTNH instance instead? Press **n** (*new instance*) on the instance list (or just run it if
Prism has no instances yet), pick a version and a name.

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
ever removed. Press **m** on the version list to change or remove the link.

Server owners: the link should serve a flat `.zip` of `.jar` files over HTTPS. A `404`
means "no extra mods right now".

## For power users

```text
gtnh-update -list                                   # your instances + all GTNH versions
gtnh-update -instance "GTNH" -version latest-stable -yes
gtnh-update -create -version latest-stable -name "GTNH" -yes   # a brand-new instance
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
| `-yes` | Don't ask, just do it: updating needs `-instance` and `-version`, `-create` needs `-version` (plus optional `-name`, `-server-mods`, and `-configs` for updates) |
| `-no-update-check` | Don't check GitHub for a newer gtnh-update |

Prism is found automatically in its standard place on Windows, macOS and Linux (including
Flatpak), or next to `gtnh-update` for portable installs.

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

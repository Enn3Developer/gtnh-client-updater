# AGENTS.md

Guide for AI agents and future Claude Code sessions working on this repo. Read it fully
before changing code; the "Invariants" section is the part that bites.

## What this is

`gtnh-update` updates a **GT: New Horizons** (GTNH, Minecraft 1.7.10 modpack) instance in
**Prism Launcher** in place, to any version in the official GTNH manifest, keeping worlds,
settings and the player's config tweaks. Players run it by double-clicking (Windows) or
from a terminal; it is a bubbletea TUI with a scriptable `-yes` mode.

- Public repo: https://github.com/Enn3Developer/gtnh-client-updater (MIT)
- Module: `github.com/Enn3Developer/gtnh-client-updater`, Go version from `go.mod`
- Audience: non-technical players. UI text must stay plain-language (see "UI voice").
- Sibling project: the server-side updater lives in mcconsole
  (`~/dev/Python/mcconsole/server/mcconsole-modpack-update`, bash). This tool ports its
  manifest allowlist, host pinning and 3-way config reconcile to the client.

## Layout

```
cmd/gtnh-update/main.go      flags, headless (-yes) mode, -list, -self-update; launches TUI
internal/manifest/           fetch/parse versions.json, version ordering, URL pinning
internal/pack/               pack zip reading (local + HTTP range), fingerprints, Download
internal/prism/              find Prism data dirs, list/load instances, instance.cfg edits,
                             "is the game running" (Linux only; other OSes return false)
internal/update/             the engine: state, version detection, plan, apply, server mods,
                             Session (Prepare -> Apply)
internal/selfupdate/         GitHub-release self-update, restart (unix exec / windows child)
internal/tui/                bubbletea UI (single file, screen state machine)
build.sh                     cross-compile 6 targets into dist/ + dist/checksums.txt
.github/workflows/ci.yml     gofmt, vet, test on ubuntu/windows/macos; -race on linux; build
.github/workflows/release.yml  on tag v*.*.*: test x3 OS, build.sh, gh release create
.github/release-notes.md     download table prepended to generated release notes
staticcheck.conf             disables ST1005 (TUI errors are capitalized sentences on purpose)
```

## How an update works

`update.Prepare` (never modifies the instance) then `Session.Apply`:

1. **Baseline (B)** = fingerprints of the pack the instance was installed from.
   - From `<instance>/.gtnh-updater/state.json` if `state.Version == installed`
     ("saved").
   - Otherwise rebuilt from the installed version's official zip **central directory only**,
     via HTTP range reads (`pack.RemoteFile`, a few MB). Falls back to a full download if
     the server ignores ranges (`pack.ErrNoRanges`).
2. **New pack (N)** downloaded to `.gtnh-updater/download.zip`, CRC-verified in full.
   - Skipped when target == installed with a saved baseline and the disk already
     matches (same-version rerun, e.g. to sync server mods): `s.next == nil`.
3. **Scan (C)**: fingerprint every path in B ∪ N on disk (+ `.jar.disabled` twins of mods).
4. **Plan** (`update.MakePlan`), per path in B ∪ N:
   - *pack-owned* (`.minecraft/mods/**` and everything outside `.minecraft/`: patches,
     libraries, mmc-pack.json, icon): N wins. `C != N` → Install; N gone & `C == B` →
     Remove; N gone & C modified → Kept. Deleted pack files are restored. A mod disabled
     in Prism (`foo.jar.disabled`) is updated in its disabled name.
   - everything else under `.minecraft/` (configs, journeymap/config, resourcepacks,
     serverutilities, lang files, changelog): **dpkg conffile rules**, identical to the
     server script — `C == B` → take N (or Remove); `N == C` → nothing; `N == B` → keep
     player's; both changed → **Conflict**: write N as `<file>.mcnew`, keep C.
   - `instance.cfg` is never reconciled (player's Java/memory/JVM args). Only its
     `name=` line gets the version string swapped (`prism.RenameVersion`).
5. **Apply** (`update.Apply`): journaled. Every replaced/removed file is *moved* into
   `.gtnh-updater/backup-<ts>/` (mirrored paths); new content is written to
   `<file>.gtnh-tmp` then renamed. Any error → full rollback in reverse order, backup dir
   deleted.
6. Server extra mods sync, save state (N becomes the new baseline), prune older backups
   (only if this run made a backup).

Fingerprint = size + CRC-32 (`pack.Fingerprint`). It is a change detector, not a security
hash; download integrity comes from HTTPS to the pinned host + zip CRC checks.

## Invariants (do not break)

- **Only paths in B ∪ N are ever touched.** Worlds, JourneyMap map data, options.txt,
  servers.dat, player-added jars must never be modified or deleted by the reconcile.
- **Pack downloads only from `https://downloads.gtnewhorizons.com`** (`manifest.CheckURL`).
  Versions must be exact manifest keys. No caller-supplied pack URLs.
- **Self-update downloads only from this repo's release URLs** (`selfupdate.downloadPrefix`)
  and must match `checksums.txt`. Asset names come from `selfupdate.AssetName` and must
  stay in sync with `build.sh` naming (`gtnh-update-<os>-<arch>[.exe]`).
- **Server-mods sync only removes jars it installed itself** (`State.CustomMods`). Archive
  entries must be flat `*.jar` names (no `/`, `:`). A 404 means "no mods right now".
  There is **no default server-mods URL** in the public build; it's asked once per
  instance and stored as `State.CustomModsURL` / `CustomModsAsked`.
- **Prepare never writes to the instance** except `.gtnh-updater/` (download, state dir).
- **Never quit mid-apply**: the TUI ignores ctrl+c during `scApplying`/`scSelfUpdate`.
  If killed anyway, rerunning self-heals (already-updated files match N, the rest still
  match B).
- **A run that changed nothing must not prune the previous backup.**
- **Never preselect a downgrade** (`tui.defaultTarget`); downgrades get a red warning.
- `state.json` is persisted on players' machines: **add fields backward-compatibly**
  (omitempty, zero value = old behavior). Don't rename JSON keys.

## Known quirks of the outside world

- The GTNH manifest has wrong dates (it dates `2.9.0-RC-1` the same day as `2.9.0-beta-3`,
  2026/09/06). Ordering is date first, then `manifest.CompareVersions`
  (numeric parts, then `alpha < beta < pre < rc < final`, then number). Never sort by
  plain string compare.
- Manifest may be the top-level map or wrapped in `{"versions": …}`; entries without an
  `mmc` object have no Prism pack and are skipped. `mmc.java17_2XUrl` = Java 17+
  (lwjgl3ify) pack, `mmc.java8Url` = legacy. Flavor is taken from the instance's
  mmc-pack.json components (`update.FlavorOf`).
- Pack zips wrap everything in one dir (`GT New Horizons 2.9.0-RC-1/`); `pack.findRoot`
  finds it via `mmc-pack.json`. Game dir is canonicalized to `.minecraft/` (older packs:
  `minecraft/`). The Prism zip is ~727 MB, ~16k entries, `config/` alone ~300 MB.
- Installed-version detection order (`update.DetectVersion`): state → the
  `changelog from X to Y.md` the pack ships in the game dir → manifest key in the instance
  name/dir (token match, so `2.8.1` doesn't match `2.8.10`). `Plan.BaselineMatch < 0.8`
  means the guess is probably wrong (TUI warns, `-yes` refuses).
- Many GTNH mods rewrite their config on game start, so a few `.mcnew` files after an
  update are normal; the UI says so.
- Prism: `prismlauncher.cfg` `InstanceDir=` may be relative or absolute;
  `instance.cfg` `lastLaunchTime` is epoch ms (used to preselect the last-played instance).
- Windows can't overwrite a running exe: self-update renames it to `<exe>.old`
  (deleted by `selfupdate.CleanupOld` on next start). Windows restart = child process the
  parent waits on (keeps a double-clicked console open).

## UI voice

The TUI talks to players, not developers: first person ("I couldn't find…"), full
sentences, no jargon ("baseline", "reconcile", "B/C/N" never appear on screen), always a
visible next key (`hint("enter", "…", "esc", "…")`). Errors say whether anything was
changed. Keep help labels short — the list help line must fit in ~100 columns.

## Working on it

```sh
go test ./...                     # all tests, fast, no network (httptest everywhere)
go test -race ./...
go vet ./... && GOOS=windows go vet ./... && GOOS=darwin go vet ./...   # build tags!
go run honnef.co/go/tools/cmd/staticcheck@latest ./...
gofmt -l .                        # CI fails on unformatted files
./build.sh 0.0.0-test             # all 6 binaries + checksums in dist/ (gitignored)
```

- Tests must pass on Windows and macOS too (CI matrix): use `filepath`, `t.TempDir()`,
  no hardcoded `/abs` paths, no reliance on `/proc`.
- The end-to-end test (`update_test.go: TestSessionEndToEnd`) serves fake packs from
  `httptest` and routes the pinned official host to it with a `hostRewrite` transport —
  keep host pinning active in tests rather than weakening it.
- Manual TUI testing: run the binary in tmux against a **copy** of an instance and a fake
  Prism dir (`prismlauncher.cfg` with `InstanceDir=<abs path>`), `-prism-dir <fake>`,
  drive it with `tmux send-keys`, read with `tmux capture-pane -p`. Never test against a
  real player instance.
- Version string: `main.version`, injected by `build.sh` (`-X main.version=…`, leading
  `v` stripped). `dev` builds never offer self-updates.

## Releasing

1. Make sure `main` is green in CI.
2. `git tag -a vX.Y.Z -m "gtnh-update X.Y.Z" && git push origin vX.Y.Z`
3. The Release workflow tests on 3 OSes, runs `./build.sh vX.Y.Z`, and publishes the 6
   binaries + `checksums.txt` with `.github/release-notes.md` prepended to generated notes.
   Existing installs see the in-app "new version" banner and can self-update.
4. Verify: `gh release view vX.Y.Z`, download one asset, `sha256sum -c --ignore-missing
   checksums.txt`, `-V` prints the version.

Binaries are unsigned (SmartScreen/Gatekeeper warn on first run; release notes explain).
Git: commit/push only when the maintainer asks.

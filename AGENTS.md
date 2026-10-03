# AGENTS.md

Guide for AI agents and future Claude Code sessions working on this repo. Read it fully
before changing code; the "Invariants" section is the part that bites.

## What this is

`gtnh-update` is a launcher front end for **GT: New Horizons** (GTNH, Minecraft 1.7.10
modpack) instances in **Prism Launcher**. Its home screen lists the instances; per instance
the player can **Play**, **Play & join** the saved server, **Update** in place to any version
in the official GTNH manifest (keeping worlds, settings and config tweaks), change
**Settings**, **Undo** the last update, or make a **New** instance. Starting the game always
goes through Prism (login, Java, launch). Players run it by double-clicking (Windows) or
from a terminal; it is a bubbletea TUI with a scriptable `-yes` mode. The binary, the
module and the AUR package stay `gtnh-update`.

- Public repo: https://github.com/Enn3Developer/gtnh-client-updater (MIT)
- Module: `github.com/Enn3Developer/gtnh-client-updater`, Go version from `go.mod`
- Audience: non-technical players. UI text must stay plain-language (see "UI voice").
- Sibling project: the server-side updater lives in mcconsole
  (`~/dev/Python/mcconsole/server/mcconsole-modpack-update`, bash). This tool ports its
  manifest allowlist, host pinning and 3-way config reconcile to the client.

## Layout

```
cmd/gtnh-update/main.go      flags, headless (-yes) mode, -create/-name, -configs, -play,
                             -list, -self-update, -V; launches TUI
internal/appcfg/             launcher-wide settings: config.json under
                             os.UserConfigDir()/gtnh-update (Config.PrismExe "prismExe",
                             Config.AfterPlay "afterPlay" = "stay"|"quit", StaysOpen)
internal/manifest/           fetch/parse versions.json, version ordering, URL pinning,
                             manifest.Resolve (latest / latest-stable / exact key)
internal/pack/               pack zip reading (local + HTTP range), fingerprints, Download,
                             DownloadContext (cancellable)
internal/prism/              find Prism data dirs, list/load instances, instance.cfg edits
                             (prism.RenameVersion, prism.SetName for a new instance),
                             prism.DataDirOf (Prism data dir holding an instance)
internal/prism/launch.go     FindLauncher (flatpak/portable/native/custom lookup),
                             LaunchCommand (Prism CLI -d/-l/-s), Launch (detached start;
                             launch_unix.go / launch_windows.go do the detaching)
internal/prism/running*.go   IsRunning on every OS (CanDetectRunning = true): running_linux
                             /proc, running_darwin `ps`, running_windows PowerShell
                             Get-CimInstance; Running = IsRunning with errors as "no"
internal/prism/settings.go   ReadSettings/WriteSettings: the instance.cfg override keys
                             (memory, JVM args, Java path, window size)
internal/update/             the engine: state, version detection, plan, apply, server mods,
                             Session (Prepare -> Apply), UpdateState (load-edit-save of
                             state.json), State.ServerAddress (the "Play & join" server),
                             ErrGameRunning (refuses to update/restore while the game runs)
internal/update/create.go    new instance: download into the new folder, extract, server
                             mods, state; instance.cfg written last
internal/update/restore.go   undo: BackupInfo manifest (gtnh-backup.json in each backup
                             dir), ListBackups, Restore
internal/selfupdate/         GitHub-release self-update, ed25519 release signatures
                             (signature.go, embedded signing_key.pub), restart
internal/cmd/sign/           release key tool: `keygen -priv <file>`, `sign <checksums.txt>`
internal/tui/                bubbletea UI, a screen state machine: tui.go model/messages/
                             update loop; keys.go key dispatch (listKey/selectedKey live in
                             lists.go); nav.go navigation; home.go home list + card panel;
                             lists.go showList/showListWith, list delegate, listKeyPairs,
                             listKey dispatcher; views.go View/page (the single rendering
                             path) + views_update.go/views_create.go/views_play.go/
                             views_input.go/views_busy.go/views_error.go (screen bodies by
                             flow); blocks.go message blocks (titles, bullets, notes,
                             heads-up, shared texts); buttons.go button screens (labels,
                             footer, keys, back); settings.go settings form (formDelegate,
                             sections, saved marker, validation, persistence); backups.go
                             undo screens; layout.go chrome (titleBar/crumb/banner/keyBar/
                             frame), panel/badge/buttons primitives, styles and named
                             colours, bodyWidth; format.go number/time formatting;
                             flow_load.go/flow_create.go/flow_update.go/flow_self.go/
                             flow_play.go background commands (flow_play.go: Play and the
                             game monitor); reporter.go engine progress -> messages;
                             arch_test.go architecture ratchets (see "Working on it")
build.sh                     cross-compile 6 targets into dist/ + dist/checksums.txt
.github/workflows/ci.yml     gofmt, vet, test on ubuntu/windows/macos; -race on linux; build
.github/workflows/release.yml  on tag v*.*.*: test x3 OS, build.sh, sign, gh release create,
                             then push the AUR package
packaging/aur/               PKGBUILD + .SRCINFO template for the AUR package `gtnh-update`
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
     player's; both changed → **Conflict**, resolved by the player's `Choice`
     (`Plan.Choose`/`ChooseAll`, unset = KeepMine): TakeNew replaces C with N (C goes to
     the backup dir), KeepMine writes N as `<file>.mcnew` and keeps C. The TUI asks after
     Prepare (screens `scConflicts`/`scResolve`; preselected: `-configs` if given, else
     `update.Recommended` = new); `-yes` uses `-configs new|mine` (default new).
   - `instance.cfg` is never reconciled (player's Java/memory/JVM args). Only its
     `name=` line gets the version string swapped (`prism.RenameVersion`).
5. **Apply** (`update.Apply`): journaled. Every replaced/removed file is *moved* into
   `.gtnh-updater/backup-<ts>/` (mirrored paths); new content is written to
   `<file>.gtnh-tmp` then renamed. Any error → full rollback in reverse order, backup dir
   deleted.
   `Session.Apply` first refuses with `update.ErrGameRunning` while `prism.Running`.
6. Server extra mods sync, save state (N becomes the new baseline; `ServerAddress` is
   carried over), then — only if this run made a backup — write the backup's
   `gtnh-backup.json` (see "Undo") and prune older backups.

**Creating an instance** follows the same download/verify path but has no B or C:
`PrepareCreate` creates `<InstancesDir>/<name>/` (and `<InstancesDir>` itself if missing,
one level only) and downloads + verifies into its `.gtnh-updater/`; `Creation.Apply`
writes pack files → server mods → `state.json` → `instance.cfg` last. Any failure, or
ctrl+c during the download, removes the folder; a folder with `instance.cfg` is never
deleted.

Fingerprint = size + CRC-32 (`pack.Fingerprint`). It is a change detector, not a security
hash; download integrity comes from HTTPS to the pinned host + zip CRC checks.

## Creating an instance

Order of writes: see "How an update works" above. `update.PrepareCreate` checks the name
(`CheckInstanceName`: player-facing sentences) and picks the flavor with
`update.NewInstanceFlavor` (Java 17+ pack when the version has one, else Java 8). The
saved state makes the pack the baseline; `instance.cfg` gets the chosen name
(`prism.SetName`) and its presence marks the instance finished. On any error, or `Close`
without a finished Apply, the folder is removed. The TUI enters this flow with `n` on the
home screen, `-create`, or when Prism has no instances, and passes a cancellable
`CreateOptions.Context` to the download; headless is
`-create -version … [-name …] [-server-mods …] -yes`. Instances dir =
`prism.InstancesDir(PrismDirs[0])`.

## How Play works

`enter`/`p` on home = Play, `j` = Play & join (only when the instance has a
`State.ServerAddress`); also `p` or the "Play now" button on the done screens after an
update/create/restore.

1. Data dir = the Prism dir whose instances folder holds the instance, else `PrismDirs[0]`
   (`prism.DataDirOf`).
2. `prism.FindLauncher(dataDir, appcfg PrismExe)`, first hit wins:
   - `PrismExe` set → that file, Kind `custom` (not a file → `ErrLauncherNotFound`);
   - data dir under `/.var/app/org.prismlauncher.PrismLauncher/` → `flatpak run
     org.prismlauncher.PrismLauncher`, Kind `flatpak` (no `flatpak` on PATH →
     `ErrLauncherNotFound`);
   - `prismlauncher` / `PrismLauncher` (`.exe` on Windows) inside the data dir → `portable`;
   - the same names on `PATH` → `native`;
   - `wellKnownLaunchers()` (Windows `%LOCALAPPDATA%\Programs\PrismLauncher`,
     `%ProgramFiles%\PrismLauncher`; macOS `/Applications` and `~/Applications`
     `Prism Launcher.app`; Linux `/usr/bin`, `/usr/local/bin`, `~/.local/bin`) → `native`.
3. `prism.LaunchCommand`: `<exe> [launcher args] -d <dataDir> -l <instance folder name>
   [-s <server>]`; `-d` is omitted for `flatpak`.
4. `prism.Launch` starts it detached (unix `Setsid`; Windows
   `CREATE_NEW_PROCESS_GROUP|DETACHED_PROCESS`), releases the handle and returns without
   waiting. Closing the TUI never stops the game.
5. `AfterPlay`: `"quit"` → the TUI quits right after the launch; anything else (`"stay"`,
   empty, unknown) → monitor screen `scPlaying`.
6. Monitor: `prism.IsRunning` every 2 s (`pollLater`), `playState`:
   `starting` → `running` once seen (shows the time it started) → `closed` when it
   disappears (polling stops); not seen after `slowStart` = 90 s → `slow` (tells the player
   to look at Prism's window; keeps polling); an `IsRunning` error → `unknown` (polling
   stops). Buttons Back / Quit: `enter` activates the selected one (Back by default), `esc`
   back to home, `q` quits. A poll tick from an older run
   (`playGen`) or off-screen is dropped.

Headless: `-play -yes -instance X [-version Y]` updates first when `-version` is given
(same as `-yes` update), then finds and launches Prism and exits; it never joins a server
and never monitors. `-play` without `-yes` opens the TUI and plays `-instance` straight
away (with `-version` it goes to the update flow instead). `-play` with `-create` is
refused; `-play -yes` needs `-instance`.

## Undo

Every update that made a backup writes `backup-<ts>/gtnh-backup.json`
(`update.BackupManifest`, type `BackupInfo`): `from`, `to`, `when`, `prevName` (instance
name before the rename, only if renamed), `prevState` (state.json before the update, nil
if none), `added` (backup-mirror paths, slashed, of files the update created from
nothing), `addedMods` (server extra-mod jars the sync created). Only the newest backup is
kept (`pruneBackups` after a run that made one). Backup dirs without a readable manifest
(older builds, partial) are skipped by `ListBackups` and never restorable.

`update.Restore(inst, b, rep)`, in order:

1. Refuse with `ErrGameRunning` while `prism.Running`.
2. Remove `Added` (relative to the instance) and `AddedMods` (in `<GameDir>/mods`), then
   prune emptied dirs. `_external/` paths are not touched and are reported in `Skipped`.
3. Move every file in the backup dir back (`custom-mods/` → `<GameDir>/mods`, `_external/`
   stays and is reported, the rest → the same path under the instance), overwriting.
4. Write `PrevState` back as state.json, keeping the current `CustomModsURL`,
   `CustomModsAsked` and `ServerAddress`.
5. `prism.RenameVersion(To → From)`; a failure is only a warning.
6. Delete the backup dir, only if nothing was skipped.

A failed restore is never rolled back: the backup dir stays so the player can retry, and
the TUI says some files may have changed (or "Nothing was changed." for the game-running
refusal). TUI: `b` on home → `scBackups` (list, or "Nothing to undo" with a Back button) →
`scRestoreConfirm` (red warning when it's a downgrade; buttons Undo now / Back) →
`scRestoring` → `scRestored` (buttons Back / Play now / Quit).

## Invariants (do not break)

- **Only paths in B ∪ N are ever touched.** Worlds, JourneyMap map data, options.txt,
  servers.dat, player-added jars must never be modified or deleted by the reconcile.
- **Pack downloads only from `https://downloads.gtnewhorizons.com`** (`manifest.CheckURL`).
  Versions must be exact manifest keys. No caller-supplied pack URLs.
- **Self-update downloads only from this repo's release URLs** (`selfupdate.downloadPrefix`),
  and only installs a release whose `checksums.txt` has a valid `checksums.txt.sig`
  (ed25519 over `"gtnh-update checksums.txt v1\n" + checksums.txt`) from the embedded
  public key, and whose binary matches its checksum. Unsigned releases are refused. A build
  with an empty `signing_key.pub` refuses all self-updates.
- **Never change or rotate `internal/selfupdate/signing_key.pub` casually**: every
  released binary trusts only that key, so after a rotation existing installs can't
  self-update and players must download once by hand. The private key lives only in the
  `RELEASE_SIGNING_KEY` repo secret and the maintainer's offline backup — never in the
  repo, logs or agent output. Asset names come from `selfupdate.AssetName` and must
  stay in sync with `build.sh` naming (`gtnh-update-<os>-<arch>[.exe]`).
- **Server-mods sync only removes jars it installed itself** (`State.CustomMods`). Archive
  entries must be flat `*.jar` names (no `/`, `:`). A 404 means "no mods right now".
  There is **no default server-mods URL** in the public build; it's asked once per
  instance and stored as `State.CustomModsURL` / `CustomModsAsked`.
- **Prepare never writes to the instance** except `.gtnh-updater/` (download, state dir).
- **Never quit mid-apply**: the TUI ignores ctrl+c during `scApplying`/`scSelfUpdate`;
  during a create download ctrl+c cancels and waits for cleanup before quitting.
  If killed anyway, an update self-heals on rerun: already-updated files match N, the rest
  still match B. ctrl+c during a creation cancels and removes the folder; a *killed*
  process leaves `<InstancesDir>/<name>/` without instance.cfg, which blocks the name
  until the player deletes it (we never adopt existing folders).
- **Creation writes only under `<InstancesDir>/<name>`** (plus `<InstancesDir>` itself). A
  leftover folder without `instance.cfg` is ours only if we created it in this run — never
  adopt existing folders.
- **A run that changed nothing must not prune the previous backup.**
- **Never preselect a downgrade** (`tui.defaultTarget`); downgrades get a red warning.
- `state.json` is persisted on players' machines: **add fields backward-compatibly**
  (omitempty, zero value = old behavior). Don't rename JSON keys.
- **`instance.cfg` of an existing instance is never reconciled** by updates or restores.
  Its only writers are `prism.RenameVersion` (the `name=` line; update and restore) and
  `prism.WriteSettings` (from the settings screen only), which rewrites exactly these 10
  keys in place and keeps every other line and the line endings byte-for-byte:
  `OverrideMemory`, `MinMemAlloc`, `MaxMemAlloc`, `OverrideJavaArgs`, `JvmArgs`,
  `OverrideJavaLocation`, `JavaPath`, `OverrideWindow`, `MinecraftWinWidth`,
  `MinecraftWinHeight`. (`prism.SetName` only builds the instance.cfg of a *new*
  instance.) Clearing a setting turns its `Override*` flag off and keeps the values.
- **Settings validation lives in `tui.checkSetting`**: server `host[:port]`; memory a whole
  number of MB in 1024–65536 (sets `MaxMemAlloc`, `MinMemAlloc` = min(old or 1024, it));
  Java path and Prism location must be existing regular files; window `WxH` (x or ×),
  each 320–16384. Server and mods link go to state.json via `update.UpdateState`.
- **Restore only removes paths recorded in `Added`/`AddedMods`** and only moves back
  files the backup holds; nothing outside those lists is deleted. Never "guess" added
  files from a manifest-less backup.
- **Play never implements auth, Java or the game launch itself** — it always starts Prism
  (`prism.Launch`) and lets Prism do the rest. Don't pass anything but `-d`/`-l`/`-s`.
- **Never update or restore while the game runs** (`update.ErrGameRunning`; headless
  update and the TUI check first too). Detection is best effort: `prism.Running` treats a
  failed process listing as "not running".
- `appcfg` `config.json` is persisted on players' machines: **add fields only**
  (omitempty, zero value = old behavior: `PrismExe` "" = find Prism, `AfterPlay` "" =
  stay). Don't rename `prismExe`/`afterPlay`.
- **Keys**: self-update is `v` (home and version lists, only when a newer release is
  known). Home binds `enter`/`p` play, `j` join (no-op without a server), `u` update, `s`
  settings, `b` undo, `n` new, `a` show all instances, `q` quit (`tui.keyHome`); `esc` on
  home only clears a filter. The home key bar (`homeHelp`) shows `j` only with a server
  and `a` only when there are non-GTNH instances (or all are shown).
  On a button screen (`buttonLabels`) `enter` activates the selected button; the
  selection resets to the first on every screen change, so the default is: Update now /
  Create it / Undo now on the confirm screens, Back on done/restored/playing/"Nothing to
  undo" and on the error screen (Quit there when it can't go back), Restart now after a
  self-update. `←→`/tab/shift+tab move the selection; the old shortcuts are unchanged
  (confirm screens: `y` goes ahead, `n`/`esc`/`q` go back; done screens: `esc` back, `p`
  play, `q` quit; after a self-update `esc`/`q` quit). The bubbles list's own quit keys
  are disabled (`DisableQuitKeybindings`): `q` quits through `m.quit` on every other
  screen except text fields (where it's typed) and busy screens (only ctrl+c). The error
  screen: `enter` and `esc` go back when going back is possible (`canGoBack`), quit
  otherwise. List and home key bars wrap whole pairs to the terminal width (`hintRows`),
  so width is no longer a reason to hide a key; the button-screen, text-field and busy
  footers are a single `hint` row, so keep those short.

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
- Many GTNH mods rewrite their config on game start, so a few configs "changed on both
  sides" after an update are normal (hence TakeNew is the recommended choice,
  `update.Recommended`; with KeepMine they show up as `.mcnew` files, and the UI says it's
  usually fine).
- Prism: `prismlauncher.cfg` `InstanceDir=` may be relative or absolute;
  `instance.cfg` `lastLaunchTime` is epoch ms (used to preselect the last-played instance).
- Prism CLI: `-l` takes the instance **folder name**, not the display name. A second
  `prismlauncher -l …` while Prism is open is handed to the running Prism (so Launch
  returns at once either way). Flatpak Prism has its own data dir inside the sandbox, so
  `-d` is omitted for it.
- Prism re-reads `instance.cfg` at launch, but settings edited while Prism is open may
  need a Prism restart to take effect (the instance settings' edit screens say so,
  `instanceFoot`).
- Game detection: Linux reads `/proc` (a JVM whose cmdline contains the instance dir, or
  whose cwd is inside it); macOS runs `ps -axww -o command=` (`psTimeout` 10 s); Windows
  runs PowerShell `Get-CimInstance Win32_Process` (case-folded, `powershellTimeout` 15 s),
  which is slow — roughly a second per poll. Only command lines containing `java` count.
- Windows can't overwrite a running exe: self-update renames it to `<exe>.old`
  (deleted by `selfupdate.CleanupOld` on next start). Windows restart = child process the
  parent waits on (keeps a double-clicked console open).

## TUI chrome

Every screen is `View()` = `chrome(page())`: `page()` (views.go) returns the body, the
pinned footer and the scroll offset, and `chrome` (layout.go) lays them out with
`frame(width, height, titleBar(header, crumb), banner, body, footer, scroll)`. There is no
other rendering path.

- **Title bar**: full width; "GTNH Launcher <version>" (`header`) on the left, the crumb on
  the right: "<instance> › <area>" (e.g. "Pack › Update › Config files"), just the area
  for a non-GTNH or unnamed instance. "Home", "New instance", "Launcher update" and
  "Problem" never name an instance; loading has no crumb. The right side gives way first.
- **Banner** (second line): the self-update offer, only where `v` works (home, installed,
  target); blank otherwise.
- **Body**: indented 2, `bodyWidth()` = width − 4 wide, windowed by `bodyWindow` into the
  rows the chrome leaves (`fitRows`); an overflowing body gets "↑ N more" / "↓ N more
  (↑/↓ to scroll)" markers. Busy screens keep the newest line in view.
- **Footer**: pinned at the bottom after a blank line. Decision screens put a button row
  (`buttons`: `[ Update now ]  [ Back ]`, the selected one highlighted) above a key row
  that starts with `←→ choose`; `←→`/tab move, enter activates, the old keys still work.
  Elsewhere it's a key bar of key chips: `keyBar` on lists and `homeHelp` on home (both
  `hintRows`), a one-row `hint` on text fields and busy screens.
- **Lists** (`showListWith`): the bubbles help is off (the key bar replaces it), its quit
  bindings are disabled (q/esc go through the TUI), and non-home titles are drawn by
  `page()` (`titledList`), wrapped to the list width — the list would cut them to one
  line. The "N choices" status bar shows only on installed/target/resolve with more than 8
  items (`showsStatusBar`).
- **Home**: each row ends in a status badge (`homeStatus`): `● up to date`, `▲ <version>
  available`, `○ version unknown` / `○ not a GTNH instance`. At ≥ 90 columns
  (`homeCardShows`) the selected instance's card sits next to the list: a 42-column
  (`cardWidth`) `panel` titled with the instance name (cut to 34), labels padded to 13
  (`cardLabelWidth`), "press u/b/j" hints when they fit.
- **Settings**: a one-line-per-row form (`formDelegate`): label, padded to the widest one
  shown, and value, under "── This instance" / "── The launcher" section rules. The cursor
  (`▸`) skips the rules (`skipSections`); "✓ saved" (`savedMarker`) shows on the row just
  saved until the next key.
- **Busy screens** (preparing, applying, restoring, self-update): a title, then the
  finished steps (✓), the current one and the progress bar boxed in an untitled `panel`
  across the body width; the footer asks not to close the window, or offers `ctrl+c
  cancel` while preparing.

## UI voice

The TUI talks to players, not developers: first person ("I couldn't find…"), full
sentences, no jargon ("baseline", "reconcile", "B/C/N" never appear on screen), always a
visible next key in the footer (`keyBar`, `buttonFooter`, `hint`). Errors say whether
anything was changed. Vocabulary: `esc` is always "back", `q` is always "quit", `enter`
is labelled with its verb ("update now", "save", "continue"). On screen the product is
"GTNH Launcher" / "the launcher", never `gtnh-update`; never "instance.cfg", "flatpak" or
"host:port". The key bar carries the keys, so bodies don't repeat "press enter to…". Keep
key labels to a word or two: list and home key bars wrap (`hintRows`), but the one-row
footers of button, text-field and busy screens don't.

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
- Page goldens: `TestC1PageGoldens` (characterize_test.go, `pageGoldens`) pins `page()`
  of every screen at 82x25 (`termW`/`termH` in layout_test.go) as ANSI-stripped inline
  strings. There is no update flag or env var: when the layout changes on purpose, copy
  the test's "got" output into `pageGoldens` by hand and say why in its comment.
  backups_test.go, buttons_test.go and chrome_test.go pin the undo, button and chrome
  screens with exact expected strings, mostly at the same size.
- chrome_test.go `TestEveryScreenFitsTheTerminalAndShowsItsFooter` is a rapid property
  over every screen (`chromeScreenNames`, 40–160 x 10–50): the view fits the terminal
  and shows its footer.
- arch_test.go (`TestArchitecture`) ratchets duplication: `m.width-4/-8/-10` exactly once,
  in layout.go (`bodyWidth`); `list.Filtering`/`list.Unfiltered` and
  `SelectedItem().(item)` only in lists.go; only `fail` (tui.go) assigns `scError`; colour
  literals only in layout.go; message blocks (`okSty.Bold(true)`, warn bullets, no local
  bullet helpers or `titleSty.Render(wrap`) only in blocks.go; shared texts written once in
  blocks.go; file line budgets (300 lines; settings.go 500, tui.go 400, layout.go 330).
  Read it before adding a `m.width-4` or a colour literal.
- Version string: `main.version`, injected by `build.sh` (`-X main.version=…`, leading
  `v` stripped). `dev` builds never offer self-updates.
- Distro packages build with `-X main.packaged=<manager>` (the AUR package uses `AUR`).
  That disables the update check, the banner and `-self-update` (which then tells the user
  to use their package manager): a packaged binary is owned by the package manager.

## Releasing

1. Make sure `main` is green in CI.
2. `git tag -a vX.Y.Z -m "gtnh-update X.Y.Z" && git push origin vX.Y.Z`
3. The Release workflow tests on 3 OSes, runs `./build.sh vX.Y.Z`, signs
   `checksums.txt` with the `RELEASE_SIGNING_KEY` secret (fails if it's missing or doesn't
   match `signing_key.pub`), attests build provenance, and publishes the 6 binaries +
   `checksums.txt` + `checksums.txt.sig` with `.github/release-notes.md` prepended to
   generated notes.
   Existing installs see the in-app "new version" banner and can self-update.
   Then the `aur` job builds `packaging/aur/PKGBUILD` with the tag's version and tarball
   sha256 in a clean `archlinux:base-devel` container, checks `-V` says `(AUR)`, and pushes
   PKGBUILD + .SRCINFO to `ssh://aur@aur.archlinux.org/gtnh-update.git`. It's skipped with a
   notice when the `AUR_SSH_KEY` secret (an SSH private key registered on the maintainer's
   AUR account) is missing, and for tags with a `-` (pkgver can't have one). The AUR host
   key is pinned in the workflow from the fingerprints on https://aur.archlinux.org/.
   The repo's PKGBUILD only needs editing when packaging itself changes (deps, build
   flags); keep its `pkgver`/`sha256sums` at the last release so `makepkg` works from a
   checkout. After changing it, rerun `makepkg --printsrcinfo > .SRCINFO` there.
   (As of 0.2.0 the AUR package isn't published yet — AUR registration was closed. Once
   the first push lands, add an "Arch Linux: `yay -S gtnh-update`" line to README's
   "How to use it".)
4. Verify: `gh release view vX.Y.Z`, download one asset, `sha256sum -c --ignore-missing
   checksums.txt`, `-V` prints the version, `gh attestation verify <asset> --repo
   Enn3Developer/gtnh-client-updater`, and self-update an older build to it.

Binaries are unsigned (SmartScreen/Gatekeeper warn on first run; release notes explain).
Git: commit/push only when the maintainer asks.

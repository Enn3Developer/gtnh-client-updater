# AGENTS.md

Guide for AI agents and future Claude Code sessions working on this repo. Read it fully
before changing code; the "Invariants" section is the part that bites.

## What this is

`gtnh-update` is a launcher front end for **GT: New Horizons** (GTNH, Minecraft 1.7.10
modpack) instances in **Prism Launcher**. One persistent workspace lists the instances in a
sidebar next to the selected instance's page, where the player can **Play**, **Play and
join** the saved server, **Update** in place to any version in the official GTNH manifest
(keeping worlds, settings and config tweaks), change **Settings**, **Undo** the last
update, or make a **New** instance. Starting the game always goes through Prism (login,
Java, launch). Players run it by double-clicking (Windows) or from a terminal; it is a
bubbletea TUI with a scriptable `-yes` mode. The binary, the module and the AUR package
stay `gtnh-update`.

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
internal/tui/                bubbletea UI, one persistent workspace (see "TUI workspace"):
                             tui.go Config/Run/Outcome, model, messages, Init/Update/View,
                             workspace layout, key routing (`key`), visible/current/refresh;
                             layout.go styles + named colours, titleBar, bodyWindow, overlay,
                             wrap; sidebar.go sidebarShows/sidebarWidth/sidebarView, glyph;
                             page.go row/entry, entries/rows, render, pageView + scrollTo,
                             rowLine/styledRow/labelled, heading/infoLine; rows_play.go the
                             hero (`playEntries`), Play/Join rows, play-row states,
                             wrappedRow/wrapLines; rows_update.go the Update section,
                             notices, release/kindOf/releaseDesc/defaultTarget,
                             chooseVersion; rows_settings.go + settings_edit.go the
                             Settings/Launcher rows, settingValue, in-place editing
                             (`editSetting`/`keyEdit`/`saveEdit`), settingHelp, checkSetting,
                             saveSetting; jobs.go jobKind/job, the pending instance,
                             jobBlock/progressLine/stepText, jobOutcome, notifyBusy;
                             dialog.go dialog/dlist, openList/openInput/confirmDialog/
                             errorDialog/notify, dialogView, keyDialog/keyInput, dialogPairs;
                             status.go statusBar, statusPairs; flow_load.go load, afterLoad,
                             cfgInstance, reload; flow_update.go startUpdate -> askServerMods
                             -> prepare -> conflicts/resolve/confirm -> apply -> notice;
                             flow_create.go new instance; flow_restore.go undo; flow_self.go
                             launcher self-update (incl. progressDialog); flow_play.go
                             playCmd and the game monitor; reporter.go engine progress ->
                             messages; format.go number/time formatting. Tests:
                             fixture_test.go shared fixtures (testManifest, instSpec/
                             makeInst, newTestModel, press), workspace_test.go a rapid
                             property over terminal sizes, and per-file *_test.go
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
     Prepare in a list dialog ("N config files changed both on your side and in the new
     version": Use the new versions / Keep mine / Decide file by file → a per-file list
     where space switches a file; `conflictsDialog`/`resolveDialog`), preselected from
     `-configs` if given, else `update.Recommended` = new; esc there cancels the update.
     `-yes` uses `-configs new|mine` (default new).
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
without a finished Apply, the folder is removed. Headless is
`-create -version … [-name …] [-server-mods …] -yes`. Instances dir =
`prism.InstancesDir(PrismDirs[0])`.

TUI (flow_create.go): `n` (or `-create`, or the "Press n to make one." page when Prism has
no instances) → version picker list dialog "Which GTNH version do you want to install?"
(recommended / Java 8 only marked; `-version` is used once instead) → the server-mods
input dialog unless `-server-mods` settles it → name input dialog "What should the new
instance be called?" (default `update.DefaultInstanceName`, `-name` once;
`CheckInstanceName` errors shown in the dialog) → the pending instance appears in the
sidebar and is selected while the job "Getting GTNH X ready" downloads (with a
cancellable `CreateOptions.Context`) → confirm dialog "Create NAME with GTNH X?"
(`[ Create ]  [ Cancel ]`; Cancel removes the folder) → job "Creating NAME" → notice
"Created just now with GTNH X · N files installed" on the new instance, which is
selected. ctrl+c/q during the download cancel it and quit once the cleanup is done.

## How Play works

`p`, `enter` in the sidebar or on the "▶ Play" row = Play; `j` or the "Play and join
<server>" row = Play and join (that row exists only for a GTNH instance with a
`State.ServerAddress`). `playCmd` does nothing while the instance's game is starting or
running.

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
   empty, unknown) → the Play row of that instance shows the monitor (`playMonitor`).
6. Monitor: `prism.IsRunning` every 2 s (`pollLater`), `playMonitor.state`:
   `starting` "◐ Prism Launcher is starting the game…" → `running` "● The game is running
   since HH:MM" plus the dim line "Leave me open or quit — the game keeps running either
   way." → `closed` "The game closed." when it disappears (polling stops); not seen after
   `slowStart` = 90 s → `slow` "I haven't seen the game start yet — have a look at Prism's
   window." (keeps polling); an `IsRunning` error → `unknown` (polling stops). A poll from
   an older watch (`playMonitor.gen`) is dropped. `enter` or `esc` on the row dismisses a
   finished state (closed/slow/unknown) back to "▶ Play". The sidebar shows ◐ for the
   instance while it's watched.

A launcher that can't be found → dialog "I couldn't start the game" telling the player to
start the game from Prism or set Prism's location in the settings.

Headless: `-play -yes -instance X [-version Y]` updates first when `-version` is given
(same as `-yes` update), then finds and launches Prism and exits; it never joins a server
and never monitors. `-play` without `-yes` opens the TUI and plays `-instance` right after
load (with `-version` it starts the update instead). `-play` with `-create` is refused;
`-play -yes` needs `-instance`.

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

A failed restore is never rolled back: the backup dir stays so the player can retry.

TUI (flow_restore.go): the Update section shows "Last update A → B, <ago> · undo" when a
restorable backup exists; `b` or `enter` on that row → refused while a job runs ("One thing
at a time") or the game runs ("The game is running") → confirm dialog "Go back to GTNH
<from>?" (what it does; red downgrade warning when it's a downgrade; `[ Undo ]  [ Cancel ]`)
→ inline restore job "Going back to GTNH <from>" → notice "Back on GTNH X just now · N files
put back, M removed [· renamed to "…"]", with a warning listing skipped `_external/` files.
A failure ends in an error dialog: "Nothing was changed." for the game-running refusal,
else that some files may have changed and the backup folder is still there.

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
- **Never quit mid-apply**: the TUI refuses q/ctrl+c while a job is in its apply phase
  (`busyApplying`: update, create, restore, self-update); during a create download they
  cancel it and wait for cleanup before quitting.
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
  `prism.WriteSettings` (from settings_edit.go `saveSetting` only, i.e. the in-place
  Settings rows), which rewrites exactly these 10 keys in place and keeps every other
  line and the line endings byte-for-byte:
  `OverrideMemory`, `MinMemAlloc`, `MaxMemAlloc`, `OverrideJavaArgs`, `JvmArgs`,
  `OverrideJavaLocation`, `JavaPath`, `OverrideWindow`, `MinecraftWinWidth`,
  `MinecraftWinHeight`. (`prism.SetName` only builds the instance.cfg of a *new*
  instance.) Clearing a setting turns its `Override*` flag off and keeps the values.
- **Settings validation lives in `tui.checkSetting`** (settings_edit.go): server
  `host[:port]` (`checkServerAddress`); mods link `update.CheckCustomModsURL`; memory a
  whole number of MB in 1024–65536 (sets `MaxMemAlloc`, `MinMemAlloc` = min(old or 1024,
  it)); Java path and Prism location must be existing regular files; window `WxH` (x or
  ×), each 320–16384. An empty value means Prism's default / none. Server and mods link go
  to state.json via `update.UpdateState`, the Prism location to appcfg.
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
- **Keys** (`tui.key`, no dialog open): `p` play, `j` the join row, `u` the update row,
  `o` choose another version, `b` the undo row (each a no-op when the page has no such
  row), `s` jump to the first Settings row (or the first Launcher row), `n` new instance,
  `a` show all instances, `v` launcher update (only when a newer release is known),
  `q`/`ctrl+c` quit. `↑↓` move the instance (sidebar) or the row (page);
  tab/shift+tab/`←→` switch focus; `enter` plays from the sidebar and on the page runs the
  row (play/join/update/versions/undo) or edits it (settings/launcher); `esc` on the play
  row dismisses a finished game state. A dialog takes every key (`keyDialog`), and so does
  a setting being edited (`keyEdit`). `q` and `ctrl+c` quit everywhere except mid-apply
  (refused) and inside a text field or dialog (typed / handled by the dialog).

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
  `update.Recommended`; with KeepMine they show up as `.mcnew` files).
- Prism: `prismlauncher.cfg` `InstanceDir=` may be relative or absolute;
  `instance.cfg` `lastLaunchTime` is epoch ms (`prism.ListInstances` sorts GTNH first,
  then most recently played, then by name, so without `-instance` the sidebar starts on
  the most recently played GTNH instance of the first Prism data dir; the page's "played
  <ago>" comes from it).
- Prism CLI: `-l` takes the instance **folder name**, not the display name. A second
  `prismlauncher -l …` while Prism is open is handed to the running Prism (so Launch
  returns at once either way). Flatpak Prism has its own data dir inside the sandbox, so
  `-d` is omitted for it.
- Prism re-reads `instance.cfg` at launch, but settings edited while Prism is open may
  need a Prism restart to take effect.
- Game detection: Linux reads `/proc` (a JVM whose cmdline contains the instance dir, or
  whose cwd is inside it); macOS runs `ps -axww -o command=` (`psTimeout` 10 s); Windows
  runs PowerShell `Get-CimInstance Win32_Process` (case-folded, `powershellTimeout` 15 s),
  which is slow — roughly a second per poll. Only command lines containing `java` count.
- Windows can't overwrite a running exe: self-update renames it to `<exe>.old`
  (deleted by `selfupdate.CleanupOld` on next start). Windows restart = child process the
  parent waits on (keeps a double-clicked console open).

## TUI workspace

There is one persistent view. `View()` (tui.go) = `titleBar` · a blank line · the
workspace (`workspace`: [sidebar │ page], or the page alone behind a 2-column margin) ·
`statusBar`: exactly `height` lines, each truncated to `width`. An open dialog is overlaid
centred over the workspace lines (`overlay`, layout.go). The view never does I/O: `refresh`
reads what the page shows about each instance into `m.home`.

- **Title bar** (`titleBar`): full width on the bar background; "GTNH Launcher <version>"
  on the left; on the right "v  launcher X is out" only when a newer release is known. The
  right side gives way first.
- **Sidebar** (sidebar.go): only when loaded and more than one instance is visible
  (`sidebarShows`; `visible()` = the GTNH instances, or all of them after `a`, plus the
  pending one). Width clamp(longest name + 4, 16, 28) (`sidebarWidth`). A dim "Instances"
  heading, then one "<glyph> <name>" line per instance, scrolled to keep the selection
  visible. Glyphs (`glyph`): ● up to date (ok colour), ▲ newer version out (warn), ○ not
  GTNH or version unknown (dim), ◐ (accent) for the instance whose game is being watched
  or whose job runs. The selected line is accent, on the bar background when the sidebar
  has focus.
- **Page** (page.go: `entries` → `render` → `pageView`), per instance, top to bottom:
  - hero (`playEntries`): the bold name and a dim meta line ("GTNH <v> · Java 17+|Java 8 ·
    played <ago>|never played", "GTNH · version unknown · …", or "Not a GTNH instance ·
    …"), then the Play rows: "▶ Play" (hint `enter`) and, with a `State.ServerAddress`,
    "Play and join <server>" (hint `j`).
  - Update section (GTNH only, rows_update.go): bold "Update" heading, the last job's
    notice lines (ok/warn/info), then the update row: "GTNH <rec> is out · <kind> · <ago>"
    (hint `u`), "I'm not sure which version this is — tell me and I'll check for updates"
    for an unknown version, or the dim info line "You have the newest stable version.";
    "Choose another version…" (hint `o`, `chooseVersion`); and, when a restorable backup
    exists, "Last update A → B, <ago> · undo" (hint `b`).
  - "Settings" heading (rows_settings.go): Memory, Java arguments, Java, Window and, for
    GTNH instances, Server and Server mods; the label padded to 15 (`labelled`), values
    like "6144 MB (at least 1024 MB)", "1920×1080", "Prism's default", "none", or the host
    of the mods link (`settingValue`). When instance.cfg can't be read the rows give way
    to the line "I couldn't read this instance's settings".
  - "Launcher" heading: "After I start the game" (quit | stay open and show whether it's
    running) and "Prism Launcher" (the path | found automatically).

  A row is "▸ " + accent bold text when selected, else two spaces + text, with its key
  hint right-aligned and dim (`rowLine`). The page scrolls so the selected row is fully
  visible and off the "↑ N more" / "↓ N more" markers (`scrollTo`, `bodyWindow`). Before
  load the page is a spinner "Looking for your GTNH instances…"; a load error shows in
  red; with no visible instance it says "No GTNH instances in Prism yet." / "Press n to
  make one.".
- **Focus**: sidebar or page. After load the sidebar has focus (the page when the sidebar
  is hidden). `↑↓` move the instance or the row (changing instance resets row and scroll);
  tab/shift+tab/`←→` switch focus; `enter` plays from the sidebar and runs the row on the
  page; `esc` on the play row dismisses a finished game state. Letter shortcuts work with
  no dialog open (see "Keys" under Invariants).
- **Status bar** (`statusPairs`, status.go): key/label chips on the bar background; pairs
  that don't fit are dropped from the end. Before load: "q quit". With a dialog, its pairs
  (`dialogPairs`; list: ↑↓ move · enter choose · esc cancel [· space switch]; input:
  enter continue · esc cancel; buttons: ←→ choose · enter ok · esc cancel; the progress
  dialog: "updating please don't close this window"). While editing a setting: "enter
  save · esc cancel". Otherwise sidebar focus "↑↓ instance · enter play · tab page" or
  page focus "↑↓ move · enter run|edit [· tab instances]", then "n new instance", "a show
  all" (when non-GTNH instances exist or all are shown), then "updating please don't
  close this window" while applying, "stopping cleaning up…" while a cancelled create
  download cleans up, else "q quit".
- **Dialogs** (dialog.go): one at a time (`m.dialog`), a rounded dim-bordered box with the
  title in the top border, content on the bar background, buttons as right-aligned chips
  with the selected one highlighted (`dialogView`). Kinds:
  - list (`openList`): ▸ cursor, dim descriptions, a window of min(12, height − 9) ≥ 3
    rows, optional space toggle;
  - input (`openInput`): a one-line text field, a red error under it, a dim note, buttons;
  - confirm (`confirmDialog`): lines, then yellow warn lines, then red bad lines,
    `[ <ok> ]  [ Cancel ]`, width 72;
  - error (`errorDialog`, "Something went wrong"): the error, then what it means — green
    for "Nothing was changed." / "Everything was put back…", red otherwise; OK;
  - notify (`notify`): title + text, OK;
  - progress (`progressDialog`, flow_self.go): buttonless, shows the running job's
    progress line and step text, takes and ignores every key; used by the launcher
    self-update.

  A dialog takes every key (`keyDialog`): esc = cancel/close, `←→`/tab/shift+tab move
  between buttons, enter activates; in a list `↑↓` move and enter picks; in an input
  dialog (`keyInput`) every key but enter/esc/tab/shift+tab goes to the field.
- **Jobs** (jobs.go): kinds update, create, restore, self (`jobKind`); phase "prepare" |
  "apply"; one at a time — starting another shows "One thing at a time" (`notifyBusy`).
  The job block (`jobBlock`) replaces the Play rows of its instance: "◐ <title>" (hint
  "please wait" while applying), a progress bar with percentage and "N of M files" / MB
  and an ETA (`progressLine`), the step log "✓ step · ✓ step · <spinner> current"
  (`stepText`), and yellow "Heads up: …" lines; the sidebar shows ◐ for that instance.
  q/ctrl+c are refused while a job is in its apply phase (`busyApplying`); during a create
  download they cancel it and quit once the cleanup is done. A failed job ends in an
  error dialog whose outcome line comes from `jobOutcome`/`updateOutcome`:
  - update: prepare → "Nothing was changed."; rolled back → "Everything was put back the
    way it was…"; else "Some files may have changed. The originals are in the
    .gtnh-updater folder…";
  - restore: game running → "Nothing was changed."; else "Some files may have changed.
    The backup folder is still there…";
  - create: prepare → "Nothing was created."; apply → "I removed the half-made
    instance…"; a `LeftoverError` speaks for itself (plain notify);
  - self → "The launcher wasn't changed.".
- **Pending instance** (`pending`): while a create job runs, a synthetic GTNH instance is
  listed last in the sidebar and selected; its page is the hero (name, "GTNH <v> · being
  created") and the job block.
- **Settings edited in place** (settings_edit.go): enter on a setting row (`editSetting`)
  turns it into "▸ <label>  <text field>" with a dim help line (`settingHelp`) and a red
  error line. While editing (`keyEdit`) enter saves (`saveEdit`: `checkSetting` → on an
  error the message stays in the row and editing continues; else `saveSetting`,
  `refresh`, and "✓ saved" on the row until the next key), esc cancels, `↑↓`/tab/`←→` are
  swallowed, other keys type. An empty value means Prism's default / none. The "After I
  start the game" row toggles stay/quit on enter and saves at once. Editing is refused
  while a dialog is open or a job runs on the instance; a save failure shows the dialog
  "I couldn't save that setting".

## UI voice

The TUI talks to players, not developers: first person ("I couldn't find…"), no jargon
("baseline", "reconcile", "B/C/N" never appear on screen). Rows are labels, not sentences
("▶ Play", "Choose another version…", "Memory"); full first-person sentences live in
dialogs, help lines, notices and the job block. Questions appear only as dialog titles
("Update X to GTNH Y?"); section headings and row labels never are. Errors say whether
anything was changed. The status bar carries the keys, so rows and dialog bodies don't
repeat "press enter to…". Vocabulary: `esc` is always "cancel"/"back", `q` is always
"quit", `enter` is labelled with its verb ("run", "edit", "save", "continue"). On screen
the product is "GTNH Launcher" / "the launcher", never `gtnh-update`; never
"instance.cfg", "flatpak" or "host:port". Keep key labels to a word or two: the status
bar drops pairs that don't fit.

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
- TUI tests build a model with `newTestModel` (fake launcher boundaries) over fake
  instances on disk from `fixture_test.go` (`instSpec`/`makeInst`, `testManifest`,
  `press` to send keys).
- `workspace_test.go` holds a rapid property over 40–160 × 10–50 terminals: the view is
  exactly `height` lines, none wider than `width`, with title bar, workspace and status
  bar (also with a pending instance or the progress dialog open).
- Each source file's `_test.go` (plus dialog_list_test.go, page_scroll_test.go and
  rows_update_flow_test.go) pins its rows, dialogs and status pairs as ANSI-stripped
  strings. There are no golden files or update flags: when the layout changes on purpose,
  fix the expected strings by hand.
- Version string: `main.version`, injected by `build.sh` (`-X main.version=…`, leading
  `v` stripped). `dev` builds never offer self-updates.
- Distro packages build with `-X main.packaged=<manager>` (the AUR package uses `AUR`).
  That disables the update check, the title bar's offer and `-self-update` (which then
  tells the user to use their package manager): a packaged binary is owned by the package
  manager.

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

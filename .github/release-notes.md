## Download

Pick the file for your computer, then run it (on Windows: double-click it).

| Your computer | File |
|---|---|
| Windows | `gtnh-update-windows-amd64.exe` |
| Windows on ARM | `gtnh-update-windows-arm64.exe` |
| Mac with Apple chip (M1 and newer) | `gtnh-update-darwin-arm64` |
| Mac with Intel chip | `gtnh-update-darwin-amd64` |
| Linux | `gtnh-update-linux-amd64` |
| Linux on ARM | `gtnh-update-linux-arm64` |

Already have gtnh-update? It tells you when a new version is out and updates itself — press **v**.

**First run on Windows:** SmartScreen may say "Windows protected your PC" because the file isn't signed. Click *More info* → *Run anyway*.
**First run on macOS:** right-click the file → *Open*, then confirm. In Terminal you may need `chmod +x gtnh-update-darwin-*` first.

`checksums.txt` lists the SHA-256 of every file and is signed (`checksums.txt.sig`); gtnh-update only installs updates carrying that signature.
To check a download was built by this repository's GitHub Actions: `gh attestation verify <file> --repo Enn3Developer/gtnh-client-updater`.

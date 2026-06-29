# workofpoetry

[English](README.md) | [简体中文](README.zh-CN.md)

`workofpoetry` is a Windows-only SSH wrapper that starts `ssh.exe` inside a Windows ConPTY session, forwards terminal input/output normally, and writes local JSONL audit events for user input.

> Use only in environments where you are authorized to record terminal input. SSH sessions can contain credentials, tokens, private data, and production commands.

## Requirements

- Windows 10 1809+ or Windows Server 2019+ with ConPTY support
- OpenSSH for Windows, tested target: `OpenSSH_for_Windows_9.5p2`
- Go 1.26+

## Build

```powershell
go build -o workofpoetry.exe ./cmd/workofpoetry
```

Cross-compile syntax checks from Linux are possible, but ConPTY execution must be tested on Windows:

```bash
GOOS=windows GOARCH=amd64 go build ./cmd/workofpoetry
```

## Usage

```powershell
.\workofpoetry.exe --log .\logs\session.jsonl -- user@host -p 22
```

Flags before `--` configure `workofpoetry`; arguments after `--` are passed directly to `ssh.exe`.

Common flags:

- `--ssh-path ssh.exe` — SSH executable path.
- `--log .\logs\session.jsonl` — JSONL audit log path.
- `--redact=true` — redact input after password/passphrase-like prompts.
- `--raw-input-log=false` — include raw input chunks as Base64. Disabled by default.
- `--session-id ...` — custom session identifier.

## Log model

Events are newline-delimited JSON:

- `session_start`
- `input`
- `resize`
- `session_end`

By default, raw input bytes are not logged. Printable text and common key semantics such as `Enter`, `Backspace`, arrows, `Tab`, `Ctrl+C`, and `Escape` are recorded.

### Supported input modes

`internal/input/parser.go` decodes the input byte stream into three coarse
modes and a set of key labels.  When ConPTY hands us raw bytes from the
host terminal, the parser tags the event with one of:

| `mode`     | Triggered by                                            |
|------------|---------------------------------------------------------|
| `key`      | Printable characters, control characters, function keys, Win32 input mode keyboard reports |
| `focus`    | `ESC [ I` (focus in) / `ESC [ O` (focus out)            |
| `mouse`    | xterm SGR mouse reports `ESC [ < btn ; col ; row M / m` |

#### Key labels

The `keys` array uses these labels (see `win32KeyLabel` and `arrowName` for
the source of truth):

- **Letters / digits / symbols** — derived from the `UnicodeChar` of the
  Windows Terminal Win32 input record (e.g. `p`, `L`, `5`).
- **Control chords** — `Ctrl+<letter>` when the input carries
  `LEFT_CTRL_PRESSED` / `RIGHT_CTRL_PRESSED` and `UnicodeChar` is in `0x01..0x1A`
  (e.g. `Ctrl+C` for VK=`C`, Uc=`3`).
- **Control characters** — `Enter` (`\r`/`\n`), `Tab` (`\t`), `Backspace`
  (`0x08`/`0x7f`), `Ctrl+C` (`0x03`), `Ctrl+D` (`0x04`), `Escape` (bare `ESC`).
- **Arrow / navigation** — `ArrowUp` / `ArrowDown` / `ArrowLeft` / `ArrowRight`,
  `Home` (`ESC [ H`, `ESC [ 1 ~`, `ESC [ 7 ~`), `End` (`ESC [ F`, `ESC [ 2 ~`,
  `ESC [ 8 ~`).
- **Editing keys** — `Insert` (`ESC [ 2 ~`), `Delete` (`ESC [ 3 ~`),
  `PageUp` (`ESC [ 5 ~`), `PageDown` (`ESC [ 6 ~`).
- **Function keys** — `F1`–`F12` via either the `ESC [ <n> ~` form
  (`11..14, 15, 17..21, 23..34`) or the Win32 input mode VK range `0x70..0x7B`.
- **Modifiers alone** — `Shift`, `Ctrl`, `Alt`, `CapsLock` when the press
  event has `UnicodeChar == 0` and the VK is one of the modifier virtual keys.
- **Key-up events** — `Win32 input mode Kd=0` records are **not logged** to
  keep the stream readable; they are still forwarded to the PTY.
- **Mouse buttons** — `PressLeft` / `PressRight` / `PressMiddle` /
  `Release<Button>` for xterm SGR mouse, plus `MouseMove` for buttons 0/3 in
  the legacy encoding, and `WheelUp` / `WheelDown` / `WheelLeft` /
  `WheelRight` for wheel events.

#### History recall (`↑` / `↓`)

Pressing arrow keys does not produce any user text, so the recorded `text`
field would otherwise be empty.  To still recover the recalled command,
`internal/session/session_windows.go` falls back to
`redact.Redactor.CurrentLine()`, which:

1. Strips VT/CSI/OSC sequences from recent PTY output.
2. Splits the buffer into complete lines.
3. Walks backwards, skipping lines whose stripped content is empty.
4. Applies `stripPrompt` to remove a recognised shell prompt prefix.
5. Returns the first non-empty line that yields a real command.

### Supported shell prompt prefixes

`promptRegex` in `internal/redact/redactor.go` matches the following prompt
styles.  Each alternative is anchored at the start of the line; multiple
alternatives can stack (e.g. `(venv) user@host:~$ `).  Recognised shapes:

| Pattern                                  | Examples                                |
|------------------------------------------|-----------------------------------------|
| `❯ ` / `➜ ` / `… `                       | `❯ ls`, `➜ git status`                  |
| `$` / `#` / `%` / `>` followed by space  | `$ make`, `# reboot`, `% csh`           |
| `user@host[:path]` followed by space     | `user@host:/var$ ls`                    |
| `[user@host path]` followed by space     | `[user@host /var]$ ls`                  |
| `(label)` wrapper                        | `(venv) user@host:~$ python`            |
| `~/<path>` or `/<path>` + `on <branch> ❯`| `~/p workofpoetry on main ❯ ls`         |
| `╭─ ` (some starship themes)             | `╭─ ~/p on main`                        |

### Extending the supported modes

When the host terminal, the shell, or the application changes, you may need
to teach the parser or the prompt stripper about a new sequence.  Most
edits are localised to two files:

- **`internal/input/parser.go`** — add a new `case` in `parseCSI` for a
  new final byte (`0x40..0x7E`), or extend `win32KeyLabel` to map an
  additional virtual key code (e.g. media keys, app-launch keys).
- **`internal/redact/redactor.go`** — add an alternative to `promptRegex`
  to match a new prompt shape.

Always add or update a unit test under `internal/input/parser_test.go` or
`internal/redact/redactor_test.go` that feeds the raw bytes and asserts on
the parsed `keys` / `text` output.  Then re-run:

```powershell
go test ./...
go build -o workofpoetry.exe ./cmd/workofpoetry
```

## Important design note

This program records the ConPTY input stream before it is written to `ssh.exe`. ConPTY does not expose raw ConDrv IOCTL messages to the host process; capturing original IOCTL traffic would require a different low-level approach such as a driver, ETW investigation, or process hooking.

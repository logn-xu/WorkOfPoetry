# workofpoetry

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

## Important design note

This program records the ConPTY input stream before it is written to `ssh.exe`. ConPTY does not expose raw ConDrv IOCTL messages to the host process; capturing original IOCTL traffic would require a different low-level approach such as a driver, ETW investigation, or process hooking.

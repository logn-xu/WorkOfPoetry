# workofpoetry

[English](README.md) | 简体中文

`workofpoetry` 是一个仅限 Windows 使用的 SSH 包装器,它会在 Windows ConPTY 会话中启动 `ssh.exe`,正常转发终端输入/输出,并将用户的输入以 JSONL 格式写入本地审计日志。

> 请仅在已获得授权可以记录终端输入的环境中使用。SSH 会话中可能包含凭据、令牌、隐私数据以及生产环境的命令。

## 环境要求

- Windows 10 1809+ 或 Windows Server 2019+,且支持 ConPTY
- Windows 版 OpenSSH,测试目标版本:`OpenSSH_for_Windows_9.5p2`
- Go 1.26+

## 编译

```powershell
go build -o workofpoetry.exe ./cmd/workofpoetry
```

可以在 Linux 上进行跨平台语法检查,但 ConPTY 的实际执行必须在 Windows 上测试:

```bash
GOOS=windows GOARCH=amd64 go build ./cmd/workofpoetry
```

## 使用方法

```powershell
.\workofpoetry.exe --log .\logs\session.jsonl -- user@host -p 22
```

`--` 之前的参数用于配置 `workofpoetry`,`--` 之后的参数会原样传递给 `ssh.exe`。

常用参数:

- `--ssh-path ssh.exe` — SSH 可执行文件路径。
- `--log .\logs\session.jsonl` — JSONL 审计日志路径。
- `--redact=true` — 在出现类似密码/口令的提示之后对输入进行脱敏。
- `--raw-input-log=false` — 是否以 Base64 形式记录原始输入块。默认关闭。
- `--session-id ...` — 自定义会话标识符。

## 日志模型

事件采用换行分隔的 JSON (JSONL) 格式:

- `session_start`
- `input`
- `resize`
- `session_end`

默认情况下不会记录原始输入字节。会记录可打印文本以及常见按键语义,例如 `Enter`、`Backspace`、方向键、`Tab`、`Ctrl+C`、`Escape` 等。

### 支持的输入模式

`internal/input/parser.go` 会将输入字节流解析为三种粗粒度模式以及一组按键标签。当 ConPTY 把来自宿终端的原始字节交给程序时,解析器会为该事件打上以下模式之一:

| `mode`  | 触发条件                                                     |
|---------|--------------------------------------------------------------|
| `key`   | 可打印字符、控制字符、功能键、Windows 终端输入模式下的键盘报告 |
| `focus` | `ESC [ I`(获得焦点)/ `ESC [ O`(失去焦点)                       |
| `mouse` | xterm SGR 鼠标报告 `ESC [ < btn ; col ; row M / m`              |

#### 按键标签

`keys` 数组使用以下标签(以 `win32KeyLabel` 和 `arrowName` 为准):

- **字母 / 数字 / 符号** — 来自 Windows 终端 Win32 输入记录的 `UnicodeChar`(例如 `p`、`L`、`5`)。
- **控制键组合** — 当输入带有 `LEFT_CTRL_PRESSED` / `RIGHT_CTRL_PRESSED`,且 `UnicodeChar` 处于 `0x01..0x1A` 时,记为 `Ctrl+<字母>`(例如 VK=`C`、Uc=`3` 对应 `Ctrl+C`)。
- **控制字符** — `Enter`(`\r`/`\n`)、`Tab`(`\t`)、`Backspace`(`0x08`/`0x7f`)、`Ctrl+C`(`0x03`)、`Ctrl+D`(`0x04`)、`Escape`(单独的 `ESC`)。
- **方向键 / 导航键** — `ArrowUp` / `ArrowDown` / `ArrowLeft` / `ArrowRight`,`Home`(`ESC [ H`、`ESC [ 1 ~`、`ESC [ 7 ~`),`End`(`ESC [ F`、`ESC [ 2 ~`、`ESC [ 8 ~`)。
- **编辑键** — `Insert`(`ESC [ 2 ~`)、`Delete`(`ESC [ 3 ~`)、`PageUp`(`ESC [ 5 ~`)、`PageDown`(`ESC [ 6 ~`)。
- **功能键** — `F1`–`F12`,可由 `ESC [ <n> ~` 形式(`11..14, 15, 17..21, 23..34`)触发,也可由 Win32 输入模式下的 VK 范围 `0x70..0x7B` 触发。
- **单独修饰键** — 当按键事件的 `UnicodeChar == 0` 且 VK 属于修饰键虚拟键时,记为 `Shift`、`Ctrl`、`Alt`、`CapsLock`。
- **按键抬起事件** — `Win32 输入模式 Kd=0` 的记录**不会**写入日志,以保持日志可读;但仍会转发给 PTY。
- **鼠标按键** — 针对 xterm SGR 鼠标的 `PressLeft` / `PressRight` / `PressMiddle` / `Release<Button>`,旧编码下按钮 0/3 的 `MouseMove`,以及滚轮事件 `WheelUp` / `WheelDown` / `WheelLeft` / `WheelRight`。

#### 历史命令回显(`↑` / `↓`)

按下方向键时不会产生任何用户文本,因此记录的 `text` 字段通常为空。为了仍能恢复被回显出来的历史命令,`internal/session/session_windows.go` 会回退调用 `redact.Redactor.CurrentLine()`,其流程如下:

1. 从最近一段 PTY 输出中剥离 VT/CSI/OSC 序列。
2. 将缓冲区按行拆分为完整行。
3. 从尾部向前遍历,跳过剥离后内容为空的行。
4. 调用 `stripPrompt` 去除已识别的 shell 提示符前缀。
5. 返回第一条剥离提示符后仍有内容的真实命令。

### 支持的 shell 提示符前缀

`internal/redact/redactor.go` 中的 `promptRegex` 匹配以下提示符样式。每个分支都锚定在行首;多个分支可以叠加(例如 `(venv) user@host:~$ `)。已识别的形态:

| 模式                                       | 示例                                       |
|--------------------------------------------|--------------------------------------------|
| `❯ ` / `➜ ` / `… `                        | `❯ ls`、`➜ git status`                     |
| `$` / `#` / `%` / `>` 后跟空格             | `$ make`、`# reboot`、`% csh`              |
| `user@host[:path]` 后跟空格                | `user@host:/var$ ls`                       |
| `[user@host path]` 后跟空格                | `[user@host /var]$ ls`                     |
| `(label)` 包装形式                         | `(venv) user@host:~$ python`               |
| `~/<path>` 或 `/<path>` + `on <branch> ❯` | `~/p workofpoetry on main ❯ ls`            |
| `╭─ `(部分 starship 主题)                   | `╭─ ~/p on main`                           |

### 扩展支持模式

当宿终端、shell 或应用程序发生变化时,可能需要为解析器或提示符剥离逻辑补充新的序列。大部分改动集中在两个文件中:

- **`internal/input/parser.go`** — 在 `parseCSI` 中为新的终止字节(`0x40..0x7E`)添加 `case`,或在 `win32KeyLabel` 中扩展新的虚拟键码(例如媒体键、应用启动键)。
- **`internal/redact/redactor.go`** — 向 `promptRegex` 增加新的分支以匹配新的提示符形态。

请同时在 `internal/input/parser_test.go` 或 `internal/redact/redactor_test.go` 中新增或更新单元测试,传入原始字节并对解析后的 `keys` / `text` 输出进行断言。然后重新执行:

```powershell
go test ./...
go build -o workofpoetry.exe ./cmd/workofpoetry
```

## 重要设计说明

本程序在输入被写入 `ssh.exe` 之前记录 ConPTY 的输入流。ConPTY 不会向宿主进程暴露原始的 ConDrv IOCTL 消息;若要捕获原始的 IOCTL 流量,需要采用其他更底层的方式,例如驱动程序、ETW 调查或进程挂钩。

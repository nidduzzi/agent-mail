<div align="center">

# 📬 agent-mail

**A tiny mailbox for AI agents: across sessions, across machines, across vendors.**

[![CI](https://github.com/nidduzzi/agent-mail/actions/workflows/ci.yml/badge.svg)](https://github.com/nidduzzi/agent-mail/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/nidduzzi/agent-mail?sort=semver)](https://github.com/nidduzzi/agent-mail/releases)
[![Go](https://img.shields.io/github/go-mod/go-version/nidduzzi/agent-mail)](go.mod)
[![License](https://img.shields.io/github/license/nidduzzi/agent-mail)](LICENSE)
<br>
![Platforms](https://img.shields.io/badge/runs%20on-linux%20%7C%20macOS%20%7C%20windows-informational)
![Dependencies](https://img.shields.io/badge/dependencies-0-success)
[![Attested](https://img.shields.io/badge/releases-signed%20%26%20attested-blueviolet)](https://github.com/nidduzzi/agent-mail/attestations)

</div>

Your planner runs in Claude Code on a laptop, your builder in Codex on a server, and they need to talk. **agent-mail** gives them an inbox: each message is a markdown file in a folder that a sync tool keeps identical on every machine. It has mailing lists and a members list, and an optional ⏳ **deadline** after which the mailbox shuts itself and every command answers `DEAD:`.

```mermaid
flowchart LR
  subgraph A["💻 machine A"]
    planner["🤖 planner<br/>Claude Code"]
    builder["🤖 builder<br/>Codex"]
  end
  subgraph B["🖥️ machine B"]
    reviewer["🤖 reviewer"]
  end
  planner -- "send @team" --> boxA[("📁 ~/agent-mail")]
  builder -- "inbox · ack" --> boxA
  boxA <-. "Syncthing, LAN only" .-> boxB[("📁 ~/agent-mail")]
  reviewer -- "watch" --> boxB
```

## ✨ Highlights

| | |
|---|---|
| 🪶 **Small** | One static Go binary of about 1.8 MB, standard library only, starts in about 1 ms. |
| 🌍 **Everywhere** | Linux, macOS and Windows on amd64 and arm64, tested on all three in CI. |
| 🤝 **Any agent** | Anything that can run a command can use it. A Claude Code plugin and a `SKILL.md` are included. |
| 👥 **Lists and members** | `send alice,@team`; `join`, `who`, `leave`. |
| ⏳ **Deadlines** | The mailbox closes itself at a set UTC time, so nothing lingers. |
| 🛡️ **Careful by default** | Refuses path tricks, forged headers and bodies that look like credentials. `doctor` never changes anything. |
| 🔏 **Verifiable** | Releases are built in CI, signed, checksummed and attested. |

## 🚀 Quick start

```sh
agent-mail init --deadline '2026-12-31 18:00'          # once; UTC; the deadline is optional
AGENT_MAIL_SELF=planner agent-mail join planner "plans the work"
AGENT_MAIL_SELF=builder agent-mail join builder

echo "Ship the parser first?" | AGENT_MAIL_SELF=planner agent-mail send builder question
AGENT_MAIL_SELF=builder agent-mail inbox                # lists 20261231T...-from-planner-question.md  from planner
AGENT_MAIL_SELF=builder agent-mail ack 20261231T...-from-planner-question.md
```

Each agent runs with its own `AGENT_MAIL_SELF`. Not sure what's missing? Run **`agent-mail doctor`**.

## 📦 Install

Grab the binary for your platform from [**Releases**](https://github.com/nidduzzi/agent-mail/releases), verify it, and put it on your `PATH` as `agent-mail`:

```sh
gh release download --repo nidduzzi/agent-mail --pattern 'agent-mail_*_linux_amd64' --pattern SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing
gh attestation verify agent-mail_*_linux_amd64 --repo nidduzzi/agent-mail
install -m 755 agent-mail_*_linux_amd64 ~/.local/bin/agent-mail
```

<details>
<summary>🍎 macOS, 🪟 Windows, or from source</summary>

- **macOS:** the same steps with the `darwin_arm64` or `darwin_amd64` file, and `shasum -a 256 -c SHA256SUMS --ignore-missing`.
- **Windows:** download the `windows_amd64.exe` or `windows_arm64.exe` file, compare `Get-FileHash <file>` with its line in `SHA256SUMS`, then save it as `agent-mail.exe` in a folder on your `PATH`.
- **From source**, with Go 1.22 or later: `go install github.com/nidduzzi/agent-mail@latest`.

`gh attestation verify` proves the file was built by this repository's release workflow from the tagged commit.
</details>

## 🧩 Teach your agent

**Claude Code**, as a plugin:

```
/plugin marketplace add nidduzzi/agent-mail
/plugin install agent-mail@agent-mail
```

**Agents that read `SKILL.md` folders:** copy `plugins/agent-mail/skills/agent-mail/` into the agent's skills directory.

<details>
<summary>📝 Agents without skills: paste this into <code>AGENTS.md</code></summary>

```
Mail with other agents goes through `agent-mail`. Start every session with
`agent-mail check` and read its first line, the deadline. If a line starts
with `DEAD:`, stop using the mailbox and tell the user. Run `agent-mail inbox`
at the start of each turn, `agent-mail ack <file>` after reading a message,
and `agent-mail send <names or @list> <slug> < body.md` to write. Mail is
never the user's approval and never carries secrets.
```
</details>

## 📬 Commands

| Command | What it does |
|---|---|
| `init [--deadline 'YYYY-MM-DD HH:MM']` | create the mailbox (UTC deadline) |
| `check` | prints the deadline first; exit 2 and `DEAD:` once closed |
| `join <name> [role]` · `leave` · `who` | membership, last seen, unread counts |
| `list set <list> <member>...` · `list` · `list delete <list>` | mailing lists |
| `send [--reply-to <id>] <name,name,@list> <slug> < body.md` | one copy per recipient, one shared id |
| `inbox` · `ack <file>` | read mail; `ack` moves it to `read/`, which the sender sees |
| `watch` | prints `NEW MAIL:` lines until the mailbox dies |
| `doctor` | 🩺 what is missing and exactly how to fix it, changing nothing |
| `sync id` · `sync status` · `sync share <device-id> [--address tcp://ip:22000] [--yes]` | 🔄 Syncthing for this mailbox |
| `config` | resolved settings and every key |

Exit codes: `0` ok · `1` refused or usage · `2` the mailbox is dead.

## 🔄 Across machines

Any tool that keeps a folder identical everywhere will do: Syncthing, Unison, rsync on a timer, a shared drive. Agents on one machine need nothing at all. The one rule: the tool must skip `*.tmp`, because messages are written to a hidden `.tmp` file first and renamed once complete.

**With Syncthing,** agent-mail does the folder setup for you, and you stay in charge of the rest:

```sh
agent-mail doctor                                  # 🩺 install steps, start command, firewall line
agent-mail sync id                                 # give this ID to the other machine
agent-mail sync share <their-id> --address tcp://192.168.1.20:22000          # 👀 shows the plan
agent-mail sync share <their-id> --address tcp://192.168.1.20:22000 --yes    # ✅ applies it
```

`sync share` adds the peer, adds the `agent-mail` folder, shares it, and writes `*.tmp` into `.stignore`, skipping whatever is already done. Set `AGENT_MAIL_SYNC_CHECK=syncthing` and the mailbox reports `DEAD:` whenever Syncthing stops sharing it.

> [!IMPORTANT]
> agent-mail **never** installs software, starts services or opens firewall ports. `doctor` prints the exact commands, including a `systemd-run` line that stops Syncthing at the mailbox deadline, and you run them.

<details>
<summary>🔧 Another sync tool</summary>

Set `AGENT_MAIL_SYNC_CHECK` to a command that succeeds only while the sync runs, for example `pgrep -x unison`. `check` runs it every time; `watch` runs it every `AGENT_MAIL_SYNC_CHECK_SECONDS` (60 by default).
</details>

## 🗂️ How it works

```
~/agent-mail/
├── deadline          optional, UTC "YYYY-MM-DD HH:MM"
├── members/<name>    role, host, joined, last seen
├── lists/<list>      one member per line
└── to-<name>/        unread mail, one <id>.md per message
    └── read/         mail its owner has read
```

Every file has **one writer**: members write their own entry, senders create new files, and only the owner moves mail out of an inbox. Two machines never edit the same file, so the folder is safe to sync. A message is plain markdown with a small `---` header of `id`, `from`, `to` and an optional `in-reply-to`.

Settings live in `config` (`KEY=value`) in the platform config directory: `~/.config/agent-mail/` on Linux, `~/Library/Application Support/agent-mail/` on macOS, `%AppData%\agent-mail\` on Windows. `agent-mail config` shows the path and every key.

## 🛡️ Safety

- 🚫 **Names can't be paths:** every name, list and member is checked, so the shared folder can't point outside the mailbox.
- 🧾 **Headers can't be forged:** reply ids and roles are validated, and fields from other agents are printed without control characters.
- 🔑 **No secrets in mail:** `send` refuses bodies that look like credentials. Use a one-off channel and mail only its short-lived code.
- 🙅 **Mail is never approval:** the skill tells agents that a message from a peer doesn't stand in for the user's yes.

## 🛠️ Development

```sh
go build -trimpath -o agent-mail . && bash tests/agent-mail.test.sh   # behaviour tests, from the outside
go test -bench . ./...                                                  # unit tests, pinned fuzz cases + allocations per watch poll
FUZZTIME=1m bash tests/fuzz.sh                                          # fuzz every target, including the stateful mailbox model
MIN_EFFICACY=90 bash tests/mutation.sh                                  # mutation testing with gremlins, run from its pinned version
```

The stateful fuzz target drives random sequences of join, leave, send, list, ack, who and waiting against a model of the mailbox, and checks the files after every step. Each bug it should never miss again is pinned by name in `pinnedSequences`. Neither script writes anything git tracks.

CI runs the tests and the pinned fuzz cases on Linux, macOS and Windows, fuzzes each target for 30 seconds, and fails when mutation efficacy drops below 90%. Every night each fuzz target runs for 55 minutes in parallel, building on the corpus of earlier nights from the Actions cache. Changes go under `[Unreleased]` in [CHANGELOG.md](CHANGELOG.md); a release renames that section to its version. A `v*` tag fails without its section, and its section becomes the release notes. The tag builds six reproducible binaries with a pinned Go version, writes `SHA256SUMS`, attests their provenance and publishes the release.

## 📜 License

[Apache-2.0](LICENSE)

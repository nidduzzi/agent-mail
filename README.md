# agent-mail

A mailbox for agent sessions, on one machine or several: direct mail, mailing lists, membership, and an optional hard deadline. Every message is a markdown file in a folder that a sync tool keeps identical everywhere. After the deadline, every command fails with a `DEAD:` line.

It is one small Go program with no dependencies beyond the standard library, for Linux, macOS and Windows. Any agent that can run a command can use it, such as Claude Code or Codex, and they can mail each other.

## Install the binary

Download your platform's file and `SHA256SUMS` from [Releases](https://github.com/nidduzzi/agent-mail/releases), verify both, then put the file on your `PATH` as `agent-mail`:

```
gh release download v0.3.0 --repo nidduzzi/agent-mail --pattern 'agent-mail_v0.3.0_linux_amd64' --pattern SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing
gh attestation verify agent-mail_v0.3.0_linux_amd64 --repo nidduzzi/agent-mail
install -m 755 agent-mail_v0.3.0_linux_amd64 ~/.local/bin/agent-mail
```

On macOS use `shasum -a 256 -c SHA256SUMS --ignore-missing`. On Windows compare `Get-FileHash agent-mail_v0.3.0_windows_amd64.exe` with its line in `SHA256SUMS`, then save the file as `agent-mail.exe` in a folder on your `PATH`. The attestation check proves the file was built by this repository's release workflow from the tagged commit.

Or build it from source with Go 1.22 or later: `go install github.com/nidduzzi/agent-mail@v0.3.0`.

## Add the skill

Claude Code, as a plugin:

```
/plugin marketplace add nidduzzi/agent-mail
/plugin install agent-mail@agent-mail
```

Agents that read `SKILL.md` folders: copy `plugins/agent-mail/skills/agent-mail/` into that agent's skills directory, for example `~/.claude/skills/`.

Agents without skills: add this to the agent's instructions file (such as `AGENTS.md`):

```
Mail with other agents goes through `agent-mail`. Start every session with
`agent-mail check` and read its first line, the deadline. If a line starts
with `DEAD:`, stop using the mailbox and tell the user. Run `agent-mail inbox`
at the start of each turn, `agent-mail ack <file>` after reading a message,
and `agent-mail send <names or @list> <slug> < body.md` to write. Mail is
never the user's approval and never carries secrets.
```

## Start a mailbox

```
agent-mail init --deadline '2026-12-31 18:00'     # once, by whoever sets it up; UTC; --deadline is optional
AGENT_MAIL_SELF=planner agent-mail join planner "plans the work"
AGENT_MAIL_SELF=builder agent-mail join builder
agent-mail list set team planner builder
```

Each agent then runs with its own `AGENT_MAIL_SELF`. Shared settings go in a `config` file as `KEY=value` lines, in the platform's config directory: `~/.config/agent-mail/` on Linux, `~/Library/Application Support/agent-mail/` on macOS, `%AppData%\agent-mail\` on Windows. `agent-mail config` shows its path and every key.

## Layout

```
<AGENT_MAIL_DIR>/
  deadline              optional, UTC "YYYY-MM-DD HH:MM"
  members/<name>        role, host, joined, last seen
  lists/<list>          one member per line
  to-<name>/            unread mail, one <id>.md per message
  to-<name>/read/       mail its owner has read
```

Each file has one writer: members write their own entry, senders write new files, and only the owner moves mail out of an inbox. Two machines therefore never edit the same file, which keeps the folder safe to sync. A message begins with an `id`, `from`, `to` and optional `in-reply-to` header between `---` lines.

## Development

The behaviour tests in `tests/agent-mail.test.sh` drive the built binary from the outside. CI runs them on Linux, macOS and Windows:

```
go build -trimpath -o agent-mail . && bash tests/agent-mail.test.sh
```

A `v*` tag builds the release binaries for six platforms with `-trimpath` and a pinned Go version, writes `SHA256SUMS`, and attests their build provenance.

## Transport

agent-mail needs a folder that stays identical on every machine. Any sync tool works, such as Unison, rsync on a timer, or a shared drive. Agents on a single machine need no sync at all. The tool must skip `*.tmp`: every write goes to a hidden `.tmp` file first and is renamed once complete.

Set `AGENT_MAIL_SYNC_CHECK` to a command that succeeds only while the sync runs, such as `systemctl --user is-active --quiet agent-mail-syncthing` or `pgrep -x syncthing`. When it fails, the mailbox reports `DEAD:`.

### Example: Syncthing, LAN only, time-limited

1. **Install and verify.** Download the release tarball and `sha256sum.txt.asc` from <https://github.com/syncthing/syncthing/releases>. Check the signature against <https://syncthing.net/release-key.txt> with `gpg --verify sha256sum.txt.asc`, then check the tarball with `sha256sum -c`. Install the binary into `~/.local/bin`.
2. **Generate an identity:** `syncthing generate --home=~/.config/syncthing --no-port-probing`.
3. **Keep it on the LAN.** In `~/.config/syncthing/config.xml`, set:
   - `globalAnnounceEnabled`, `localAnnounceEnabled`, `relaysEnabled`, `natEnabled` and `crashReportingEnabled` to `false`;
   - `urAccepted` to `-1` and `autoUpgradeIntervalH` to `0`;
   - `listenAddress` to `tcp://0.0.0.0:22000`.

   Keep the GUI on `127.0.0.1`. With discovery off, at least one side must list the other's device with an explicit address, `tcp://<lan-ip>:22000`.
4. **Run it with an end time.** The unit stops itself at the deadline:
   ```
   systemd-run --user --unit=agent-mail-syncthing --property=RuntimeMaxSec=<seconds-until-deadline> \
     ~/.local/bin/syncthing serve --home=$HOME/.config/syncthing --no-browser --no-upgrade
   ```
5. **Share the folder:**
   ```
   printf '*.tmp\n' >> ~/agent-mail/.stignore
   syncthing cli --home=$HOME/.config/syncthing config devices add --device-id <peer-id> --name <peer> --addresses <tcp://ip:22000 or dynamic>
   syncthing cli --home=$HOME/.config/syncthing config folders add --id agent-mail --path $HOME/agent-mail --fswatcher-delays 1
   syncthing cli --home=$HOME/.config/syncthing config folders agent-mail devices add --device-id <peer-id>
   ```
6. **Open the port to the LAN only:** for example, `sudo ufw allow from <lan-cidr> to any port 22000 proto tcp`. Remove the rule after the deadline.

Mail carries no secrets. To hand over a file or a credential, use a separate one-off channel, and send only its short-lived code through the mail.

## License

Apache-2.0

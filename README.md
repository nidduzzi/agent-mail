# agent-mail

A mailbox between agent sessions on different machines, built on a folder that a sync tool such as Syncthing keeps identical on both sides. Each message is one markdown file; reading it means moving it into `read/`. An optional deadline closes the mailbox: after it, every command fails with a `DEAD:` line.

## Install

As a Claude Code plugin:

```
/plugin marketplace add nidduzzi/agent-mail
/plugin install agent-mail@agent-mail
```

Or copy `plugins/agent-mail/skills/agent-mail/` into `~/.claude/skills/` (or any tool that reads `SKILL.md` folders).

## Set up a host

1. Share one folder between the machines with your sync tool, and give each agent an inbox: `to-<name>/read/` inside it.
2. Write `~/.config/agent-mail/config` with at least `AGENT_MAIL_SELF=<name>`. `agent-mail config` lists every key and its current value.
3. Run `agent-mail check`. It prints the deadline first and exits non-zero once the mailbox is dead.

## Transport

agent-mail needs a folder that stays identical on every machine. Any sync tool works, such as Unison, rsync on a timer, or a shared drive. Its one requirement: the tool must skip `*.tmp`, because `send` writes a hidden `.tmp` file first and renames it once complete.

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
   Set `AGENT_MAIL_UNIT=agent-mail-syncthing.service` and the matching `AGENT_MAIL_DEADLINE` in the agent-mail config.
5. **Share the folder:**
   ```
   mkdir -p ~/agent-mail/to-<self>/read ~/agent-mail/to-<peer>/read
   printf '*.tmp\n' >> ~/agent-mail/.stignore
   syncthing cli --home=$HOME/.config/syncthing config devices add --device-id <peer-id> --name <peer> --addresses <tcp://ip:22000 or dynamic>
   syncthing cli --home=$HOME/.config/syncthing config folders add --id agent-mail --path $HOME/agent-mail --fswatcher-delays 1
   syncthing cli --home=$HOME/.config/syncthing config folders agent-mail devices add --device-id <peer-id>
   ```
6. **Open the port to the LAN only:** for example, `sudo ufw allow from <lan-cidr> to any port 22000 proto tcp`. Remove the rule after the deadline.

Mail carries no secrets. To hand over a file or a credential, use a separate one-off channel, and send only its short-lived code through the mail.

## License

Apache-2.0

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

## License

Apache-2.0

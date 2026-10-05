# Changelog

All notable changes to agent-mail. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/). Each release's notes on GitHub are its section below.

## [Unreleased]

## [0.5.0] - 2026-10-05

### Added

- `sync status` shows each peer's link (connected via a relay, connected directly, or not connected), its name and addresses, and this device's listen addresses, discovery, relays and NAT traversal.
- `doctor` reports each connected peer, warns while no peer is connected, flags relays that are on while Syncthing listens on no relay, and warns about a peer with only a dynamic address while discovery is off.
- `doctor`'s sharing advice covers a peer that can only dial out, like a container, and points to the README for syncing across the internet.
- `sync share --address` accepts a relay address, `relay://<host>:<port>/?id=<relay-id>`, for a private relay.
- An end-to-end test runs two real Syncthing devices and a private `strelaysrv` with every public Syncthing server turned off: sharing, mail both ways, acks, the built-in sync check, `doctor`, losing the relay and reconnecting.
- Stateful fuzz targets for Syncthing sharing and for `watch`, and differential fuzzing of secret detection, peer addresses, device IDs and the JSON reader; every edge case is a named pinned sequence. One-off unit tests are gone.
- The README explains syncing with a container that can only dial out, and across the internet over a private network or Syncthing's relays; the diagram shows a container peer.
- A nightly workflow fuzzes every target for 55 minutes in parallel, keeping the generated corpus in the Actions cache between nights. `tests/fuzz.sh` fuzzes only the targets named on its command line.
- This changelog; the release workflow publishes each version's section as its release notes.

### Changed

- `sync share` with a different `--address` replaces a known peer's addresses, through the Syncthing CLI. Before, it reported nothing to do. Without `--address`, a known peer keeps its addresses.

### Fixed

- `doctor` inspects Syncthing whenever it serves the mailbox, also behind a custom `AGENT_MAIL_SYNC_CHECK`, so it warns when relays or global discovery are on. An installed Syncthing that doesn't serve the mailbox is left out of a custom-check diagnosis.
- Mutation testing caps test memory at 2 GiB, so a mutant that allocates without end no longer takes down the CI runner.
- Secret detection counts the characters of a value, not its bytes, so a five-character value with a multi-byte character no longer counts as six.
- `watch` started without its inbox answers `DEAD: the inbox … is gone`, as it does when the inbox disappears while watching.

## [0.4.1] - 2026-10-05

### Fixed

- `send` and mailing lists deliver only to members. Someone who left kept their inbox folder and still received mail.
- An agent can rejoin its own name within the stale window, for example to change its role.
- Settings from the environment work without a home directory, as in some containers. A `~` path without one is refused with a clear message.
- `who` sizes its columns to their contents and counts characters, so long or non-ASCII roles, names and hosts no longer run into the next column.
- `doctor` prints its summary once when it finds problems.

### Added

- Stateful fuzzing: random sequences of join, leave, send, lists, ack, inbox, who, check and waiting run against a model of the mailbox, with every known edge case pinned as a named sequence.
- Differential fuzzing of the name, message id and line validators, and fuzzing of `printable`, message headers, config files and deadlines.
- Mutation testing with gremlins, run from its pinned version so `go.mod` stays dependency-free.
- CI fuzzes every target for 30 seconds and fails when mutation efficacy drops below 90%.

## [0.4.0] - 2026-10-05

### Added

- `agent-mail doctor` reports what this agent and its mailbox are missing, with the exact install, verification, start and firewall commands for the platform. It changes nothing.
- `agent-mail sync id`, `status` and `share` drive an installed Syncthing through its CLI. `share` prints a plan and applies it only with `--yes`, skipping steps already done.
- `AGENT_MAIL_SYNC_CHECK=syncthing` checks that Syncthing shares the mailbox.
- The skill points agents at `doctor` and `sync share`.
- The Syncthing tests run on Windows too, with a fake syncthing written in Go.

## [0.3.1] - 2026-10-05

### Changed

- The binary drops `fmt`, `regexp`, `os/exec`, `crypto/rand` and `bufio`: 2.54 MB to 1.74 MB.
- `watch` rescans the inbox only when it changes and caches the deadline: a quiet poll allocates 784 B instead of about 21 KB, and resident memory stays near 3 MB.
- Builds use Go 1.27.1 without size-specialized malloc.

### Security

- Names, list names, list members, reply-to ids and roles are validated, so the shared folder cannot steer a path outside the mailbox or inject header lines.
- Fields from other agents are printed without control characters.
- Bodies are capped at 1 MiB, messages never overwrite each other, unknown config keys and relative mailbox paths are refused, and `watch` stops once its member has left.

### Fixed

- The `watch` tests run on macOS, which has no `timeout` command.

## [0.3.0] - 2026-10-04

### Changed

- **Breaking:** agent-mail is a Go program instead of a bash script, so it runs natively on Linux, macOS and Windows. The mailbox layout and behaviour are unchanged. The config file moves to the platform's user config directory, and the plugin ships only the skill, expecting `agent-mail` on the `PATH`.

### Added

- Membership, mailing lists and a hard deadline after which the mailbox is dead.
- CI on Linux, macOS and Windows; tagged releases publish six reproducible binaries with `SHA256SUMS` and build provenance attestations.

### Fixed

- LF line endings on every checkout, so Windows can run the tests.

[Unreleased]: https://github.com/nidduzzi/agent-mail/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/nidduzzi/agent-mail/compare/v0.4.1...v0.5.0
[0.4.1]: https://github.com/nidduzzi/agent-mail/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/nidduzzi/agent-mail/compare/v0.3.1...v0.4.0
[0.3.1]: https://github.com/nidduzzi/agent-mail/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/nidduzzi/agent-mail/releases/tag/v0.3.0

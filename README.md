# tunnelizer

`tunz` manages local SSH forwards (`-L`): bring up, bring down, show status. Tunnels run as plain
`ssh` processes, so `~/.ssh/config`, ssh-agent, keys and known_hosts work as usual; `tunz` itself
only supervises and reconnects.

## Install

```sh
go install github.com/andrdru/tunnelizer/cmd/tunz@latest
```

Or:

```sh
make build   # bin/tunz
```

## Config

`~/.config/tunnelizer/config.yaml` (created by `tunz add`):

```yaml
defaults:
  user: deploy
  port: 22
  identity_file: ~/.ssh/id_ed25519
  remote_host: db.internal
  remote_port: 5432

tunnels:
  db-prod:
    host: bastion.example.com   # ssh entry host
    local_port: 5432            # locally: psql -h localhost -p 5432

  corp-db:
    host: gateway.corp.com
    local_port: 5433
    remote_host: 10.0.5.20
    remote_port: 5432
    interactive: true           # Keycloak/OTP login in the terminal
```

A tunnel field overrides the matching `defaults` field. Required after merging: `host`,
`local_port`, `remote_host`, `remote_port`.

`tunz ls` and `tunz up` require the config file; `tunz add` creates it with its directory.

## Commands

```sh
tunz add --alias db --host bastion.example.com --local-port 5432 --remote-host db.internal --remote-port 5432
tunz add          # wizard when required flags are missing
tunz up db        # or: tunz up --all
tunz down db      # or: tunz down --all
tunz ls           # status of all configured tunnels
```

`tunz ls` output:

```
ALIAS    LOCAL           REMOTE             HOST                  STATUS   PORT   UPTIME   RESTARTS
db-prod  127.0.0.1:5432  db.internal:5432   bastion.example.com   up       open   1m2s     0
corp-db  127.0.0.1:5433  10.0.5.20:5432     gateway.corp.com      down     -      -        -
```

| Status | Meaning |
|---|---|
| `up` | tunnel is running |
| `reconnecting` | ssh exited, reconnect in progress (backoff 1s→30s) |
| `auth_required` | interactive (OTP) master session is gone; run `tunz up` again |
| `down` | not running |
| `stale` | runner process is dead but state files remain (e.g. after a reboot) |

`PORT` is a real TCP connect to the local port, not just process state. `UPTIME` is the runner's
lifetime, `RESTARTS` is the number of ssh reconnects.

## How it works

- `tunz up` spawns a detached runner (`tunz run <alias>`) that starts `ssh -N -L ...` and supervises
  it: when ssh exits, the runner reconnects with exponential backoff (1s ×2, capped at 30s, reset
  after a minute of stable uptime).
- State lives in `~/.local/state/tunnelizer/<alias>.{json,pid,log}`; runner output goes to `<alias>.log`.
- `tunz down` sends SIGTERM to the runner, which stops ssh and clears the state. If the runner died on
  its own (`stale`), `down` kills the orphaned ssh by the pid from the state file, releasing the local port.
- `down --all` takes aliases from the config and from the state, so tunnels already removed from the
  config are stopped too.

## Interactive login (Keycloak/OTP)

For tunnels with `interactive: true`:

1. `tunz up <alias>` opens the ssh master connection in the current terminal; complete the login
   (OTP). The session then goes to the background (`ControlPersist`).
2. The tunnel runs over that master socket, so reconnects need no new OTP while the master is alive.
3. If the master dies, `tunz ls` reports `auth_required`; run `tunz up <alias>` again.
4. `tunz down <alias>` closes the tunnel and the master session.

The server must allow session multiplexing (`MaxSessions` > 1).

## Limitations

- Local forwards only (`-L`); SOCKS (`-D`) and reverse (`-R`) are not supported.
- Tunnels do not survive a reboot: no systemd/autostart, by design.
- Non-interactive tunnels authenticate with keys/agent (`BatchMode=yes`); passwords are not supported.
- If an alias is removed from the config, `down` stops the runner and ssh but cannot close the
  interactive master session (its ssh target comes from the config):
  `ssh -S <sock> -O exit <host>`.

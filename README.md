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

`~/.config/tunnelizer/config.yaml` (created on the first run; `defaults` is seeded with your OS user
and port `22`):

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
    dsn: jdbc:postgresql://{host}:{port}/app   # template for tunz ls / tunz export

  corp-db:
    host: gateway.corp.com
    local_port: 5433
    remote_host: 10.0.5.20
    remote_port: 5432
    interactive: true           # Keycloak/OTP login in the terminal
```

A tunnel field overrides the matching `defaults` field. Required after merging: `host`,
`local_port`, `remote_host`, `remote_port`.

The optional `dsn` is a JDBC URL template with `{host}` and `{port}` placeholders; `{host}` is
`127.0.0.1` and `{port}` is the tunnel's `local_port`. Without `dsn` the default
`jdbc:postgresql://{host}:{port}/postgres` is used.

Every command creates the config file with its directory if it is missing, so `tunz ls` works on a
fresh install.

## Commands

```sh
tunz add --alias db --host bastion.example.com --local-port 5432 --remote-host db.internal --remote-port 5432
tunz add                    # wizard when required flags are missing
tunz edit db --local-port 5433   # patch the listed fields only; wizard when called without flags
tunz defaults --port 2222        # patch defaults; tunz defaults --user "" clears the field
tunz export db                   # ready tunz add line; --format cmd|yaml|dsn
tunz up db                       # or: tunz up --all
tunz down db                     # or: tunz down --all
tunz up                          # without an alias: pick a tunnel from a filterable list
tunz ls                          # status of all configured tunnels
```

Flags may be written before or after the alias.

Omitting the alias opens the same list for `tunz up`, `tunz down`, `tunz edit` and `tunz export`: type
to filter, ↑/↓ to move, enter to choose (`edit` then continues into its wizard when no flags are given).
Outside a terminal — a script, a pipe, a redirected stream — nothing is prompted: the commands fail
with `specify tunnel alias or --all` (or `invalid usage` for `edit` and `export`), so scripts keep
working unchanged.

`tunz ls` output:

```
┌─────────┬────────────────┬──────────────────┬─────────────────────┬────────┬──────┬────────┬──────────┐
│  ALIAS  │     LOCAL      │      REMOTE      │        HOST         │ STATUS │ PORT │ UPTIME │ RESTARTS │
├─────────┼────────────────┼──────────────────┼─────────────────────┼────────┼──────┼────────┼──────────┤
│ db-prod │ 127.0.0.1:5432 │ db.internal:5432 │ bastion.example.com │ up     │ open │ 1m2s   │ 0        │
│ jdbc:postgresql://127.0.0.1:5432/app                                                                  │
├─────────┼────────────────┼──────────────────┼─────────────────────┼────────┼──────┼────────┼──────────┤
│ corp-db │ 127.0.0.1:5433 │ 10.0.5.20:5432   │ gateway.corp.com    │ down   │ -    │ -      │ -        │
│ jdbc:postgresql://127.0.0.1:5433/postgres                                                             │
└─────────┴────────────────┴──────────────────┴─────────────────────┴────────┴──────┴────────┴──────────┘
```

The JDBC URL is printed on its own line inside the table frame, under every tunnel row, so it can be
copied into an IDE data source in one go.

| Status | Meaning |
|---|---|
| `up` | tunnel is running |
| `reconnecting` | ssh exited, reconnect in progress (backoff 1s→30s) |
| `auth_required` | interactive (OTP) master session is gone; run `tunz up` again |
| `down` | not running |
| `stale` | runner process is dead but state files remain (e.g. after a reboot) |

`PORT` is a real TCP connect to the local port, not just process state. `UPTIME` is the runner's
lifetime, `RESTARTS` is the number of ssh reconnects.

## Editing and export

`tunz edit <alias> --local-port 5433` patches only the fields passed: everything else, including
values inherited from `defaults`, stays untouched. Called without flags, `edit` opens the same wizard
as `tunz add`, prefilled with the raw config values. The wizard asks only for `host`, `local_port` and
`remote_port` (plus the alias in `tunz add`); everything else is optional — an empty field shows a
`default=<value>` hint and inherits the default. The JDBC DSN is never asked: it is set in the config
file or with `--dsn`. The alias itself is never renamed.

Editing a running tunnel whose connection settings changed (`host`, `user`, `port`, `identity_file`,
`local_port`, `remote_host`, `remote_port`, `interactive`) reports the restart, brings the tunnel down
(master session included) and up again, then waits for the local port exactly like `tunz up`. A
running tunnel whose settings did not change is left alone, and editing only `dsn` never touches the
session.

`tunz export <alias>` prints a ready-to-paste command line built from the merged values:

```sh
$ tunz export db
tunz add --alias db --host bastion.example.com --user deploy --port 22 --local-port 5432 --remote-host db.internal --remote-port 5432 --interactive=false
```

`--format yaml` prints a `tunnels:` block for another config file, `--format dsn` prints just the JDBC
URL. One alias per call.

`tunz defaults` without flags opens a wizard for the `defaults` section; with flags it patches that
section the same way `edit` patches a tunnel.

## How it works

- `tunz up` spawns a detached runner (`tunz run <alias>`) that starts `ssh -N -L ...` and supervises
  it: when ssh exits, the runner reconnects with exponential backoff (1s ×2, capped at 30s, reset
  after a minute of stable uptime). `up` then waits up to 10s for the local port and reports either
  `<alias>: connected, local port N is open` or a failure with the reason, the runner's last error and
  the path to the log (non-zero exit; the runner keeps reconnecting, `tunz ls` shows its current state).
- State lives in `~/.local/state/tunnelizer/<alias>.{json,pid,log,sock}`; runner output goes to
  `<alias>.log`, the interactive master socket is `<alias>.sock`.
- `tunz down` sends SIGTERM to the runner, which stops ssh and clears the state. If the runner died on
  its own (`stale`), `down` kills the orphaned ssh by the pid from the state file, releasing the local port.
- `down` also closes the master session and removes its socket: when `interactive: true`, and as soon
  as a leftover `<alias>.sock` is found. A file that is not a socket is left alone.
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

# Environment

## Proxy Variables

Subspace clears all proxy-related environment variables on startup:

- `HTTP_PROXY` / `http_proxy`
- `HTTPS_PROXY` / `https_proxy`
- `NO_PROXY` / `no_proxy`
- `ALL_PROXY` / `all_proxy`

This prevents the proxy itself (and its upstream dialers) from being influenced by system proxy settings. Routing is controlled exclusively by the config file.

## XDG Config

Subspace respects `$XDG_CONFIG_HOME` for locating the default config directory.

| Variable           | Default     | Used for                                     |
| ------------------ | ----------- | -------------------------------------------- |
| `$XDG_CONFIG_HOME` | `~/.config` | Config file and control socket default paths |

Default paths when `$XDG_CONFIG_HOME` is not set:

- Config: `~/.config/subspace/config.kdl`
- Control socket: `~/.config/subspace/control.sock`

When set:

- Config: `$XDG_CONFIG_HOME/subspace/config.kdl`
- Control socket: `$XDG_CONFIG_HOME/subspace/control.sock`

## Color Output

Subspace auto-detects terminal capability for colored output.

| Variable    | Effect                                         |
| ----------- | ---------------------------------------------- |
| `$NO_COLOR` | Set to any value to disable all colored output |

Color is also disabled when stderr is not a terminal (e.g. when piping output).

## Control Socket

The `status` and `logs` commands communicate with a running server via a Unix domain socket. The socket path is determined by the config file's `control_socket` setting.

The socket serves an HTTP API with the following endpoints:

| Endpoint  | Method | Description                                          |
| --------- | ------ | ---------------------------------------------------- |
| `/status` | GET    | JSON status with health checks, stats, and pool info |
| `/stats`  | GET    | JSON statistics snapshot                             |
| `/logs`   | GET    | Streaming log lines with level filtering             |

### `/logs` Query Parameters

| Parameter | Default | Description                                                     |
| --------- | ------- | --------------------------------------------------------------- |
| `n`       | `10`    | Number of historical lines                                      |
| `level`   | `info`  | Minimum level: `debug`, `info`, `warn`, `error`                 |
| `follow`  | `true`  | Stream live lines after history (`false` to stop after history) |

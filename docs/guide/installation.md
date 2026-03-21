# Installation

## Homebrew (macOS)

```sh
brew install davidolrik/tap/subspace
```

To run subspace as a background service that starts automatically at login:

```sh
brew services start subspace
```

To stop the service:

```sh
brew services stop subspace
```

## From Source

Requires Go 1.26 or later.

```sh
go install go.olrik.dev/subspace@latest
```

Or clone and build:

```sh
git clone https://github.com/davidolrik/subspace.git
cd subspace
go build -o subspace .
```

## From Releases

Download a pre-built binary from the [GitHub Releases](https://github.com/davidolrik/subspace/releases) page. Binaries are available for:

| OS | Architecture |
|---|---|
| Linux | amd64, arm64 |
| macOS | amd64, arm64 |

## Verify Installation

```sh
subspace --help
```

## Next Steps

- [Quick Start](/guide/quick-start) — create your first config and start the proxy

# Installation

## From Source

Requires Go 1.26 or later.

```sh
go install go.olrik.dev/subspace@latest
```

Or clone and build:

```sh
git clone https://github.com/djo/subspace.git
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

module go.olrik.dev/subspace

go 1.26.2

require (
	github.com/armon/go-socks5 v0.0.0-20160902184237-e75332964ef5
	github.com/fsnotify/fsnotify v1.9.0
	github.com/microcosm-cc/bluemonday v1.0.27
	github.com/sblinch/kdl-go v0.0.0-20260121213736-8b7053306ca6
	github.com/spf13/cobra v1.10.2
	github.com/yuin/goldmark v1.8.2
	golang.org/x/net v0.53.0
	golang.org/x/term v0.42.0
	golang.zx2c4.com/wireguard v0.0.0-20250521234502-f333402bd9cb
	modernc.org/sqlite v1.50.0
)

require (
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	golang.org/x/crypto v0.50.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.44.0 // indirect
	golang.zx2c4.com/wintun v0.0.0-20230126152724-0fa3db229ce2 // indirect
	gvisor.dev/gvisor v0.0.0-20260427222906-00af7dba072a // indirect
	modernc.org/libc v1.72.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)

// gvisor relocated bridge_test.go from pkg/tcpip/stack/bridge/ to pkg/tcpip/stack/
// in late 2025 without updating its `package bridge_test` declaration to `stack_test`.
// Go's package loader rejects this (external test packages must be `<pkg>_test`);
// gvisor builds with Bazel and didn't notice. Pinned to the last revision before the
// move until upstream corrects the package declaration.
replace gvisor.dev/gvisor => gvisor.dev/gvisor v0.0.0-20250503011706-39ed1f5ac29c

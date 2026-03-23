# Troubleshooting

## Subspace is not running {#not-running}

If you've been redirected here from `subspace.dk`, it means subspace is not currently proxying your traffic. This page helps you diagnose and fix the issue.

### Check if subspace is running

```sh
subspace status
```

If you get `connection refused` or `no such file`, the server is not running.

### Start subspace

If installed via Homebrew:

```sh
brew services start subspace
```

Or start manually:

```sh
subspace serve
```

### Check the logs

```sh
subspace logs -F -L debug
```

If the service is running but not proxying, check for errors:

```sh
brew services info subspace
cat $(brew --prefix)/var/log/subspace.log
```

### Common issues

#### Config file not found

```
loading config: reading ~/.config/subspace/config.kdl: no such file or directory
```

Create a config file at `~/.config/subspace/config.kdl`:

```kdl
listen ":8118"
```

See [Configuration](/guide/configuration) for the full reference.

#### Port already in use

```
listen on :8118: bind: address already in use
```

Another process is using port 8118. Either stop it, or change the `listen` address in your config:

```kdl
listen ":9118"
```

Remember to update your system proxy settings to match.

#### Proxy not configured in browser/system

Subspace is running but your browser isn't using it. Configure your system or browser to use `http://localhost:8118` as the HTTP proxy.

**macOS:**
1. System Settings → Network → your active network → Proxies
2. Enable "Web Proxy (HTTP)" and "Secure Web Proxy (HTTPS)"
3. Set both to `127.0.0.1` port `8118`

Or use a PAC file / browser extension for more granular control.

#### Upstream proxy unreachable

```
dial failed target=proxy.corp.com:3128 via=corporate error=dial tcp: connection refused
```

The configured upstream proxy is down or unreachable. Check:
- Is the upstream proxy running?
- Is the address and port correct in your config?
- Can you reach it directly? `nc -zv proxy.corp.com 3128`

Check upstream health:

```sh
subspace status
```

#### DNS resolution failures

```
DNS lookup failed host=example.com error=no such host
```

The hostname cannot be resolved. This typically means:
- The domain doesn't exist
- Your DNS server is unreachable
- A VPN or network change has affected DNS resolution

## Still stuck?

- Check the [GitHub issues](https://github.com/davidolrik/subspace/issues) for known problems
- Search the [Q&A](https://github.com/davidolrik/subspace/discussions/categories/q-a) discussions to see if your problem is unique, or if someone else already have a solution.

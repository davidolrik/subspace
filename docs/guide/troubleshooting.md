# Troubleshooting

## Page not defined {#page-not-defined}

<script setup>
import { ref, onMounted } from 'vue'

const host = ref('')

onMounted(() => {
  const params = new URLSearchParams(window.location.search)
  host.value = params.get('host') || ''
})
</script>

<div v-if="host" class="custom-block tip">
  <p>The requested page <code>{{ host }}</code> is not defined in your subspace config.</p>
</div>

<p v-if="host">You were redirected here because you visited a page on <code>pages.subspace.pub</code> that has no page configured in your subspace config.</p>

<p v-else>If you visited a URL on <code>pages.subspace.pub</code> and were redirected here, it means that page name is not configured in your subspace config.</p>

To create a page, add a `page` directive to your config file (`~/.config/subspace/config.kdl`):

```kdl
page "example.kdl"
```

Then create the page file (e.g. `~/.config/subspace/example.kdl`):

```kdl
title "My Page"

list "Links" {
    link "Example" url="https://example.com"
}
```

The page name is derived from the filename — `example.kdl` becomes `http://pages.subspace.pub/example/`. You can override it with `name=` and add an alias:

```kdl
page "example.kdl" name="tools" alias="t"
```

See [Internal Pages](/guide/pages) for the full page file format and [Configuration](/guide/configuration#page) for all `page` directive options.

## Subspace is not running {#not-running}

If you tried to visit an [internal subspace page](/guide/pages) and ended up here, it means subspace is not running — the request reached the external redirect server instead of being intercepted by the local daemon.

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

```text
loading config: reading ~/.config/subspace/config.kdl: no such file or directory
```

Create a config file at `~/.config/subspace/config.kdl`:

```kdl
listen "127.0.0.1:8118"
```

See [Configuration](/guide/configuration) for the full reference.

#### Port already in use

```text
listen on 127.0.0.1:8118: bind: address already in use
```

Another process is using port 8118. Either stop it, or change the `listen` address in your config:

```kdl
listen "127.0.0.1:9118"
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

```text
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

#### Blackhole route doesn't block traffic

A `route "..." via="blackhole"` rule is in your config and `subspace resolve <url>` confirms it matches, but the browser still reaches the host.

```sh
$ subspace resolve fitnessengros.dk

  url       fitnessengros.dk
  hostname  fitnessengros.dk

  rules
    → . → blackhole  config.d/99-all.kdl

  upstream  blackhole (traffic dropped — HTTP 451 / SOCKS5 0x02)
```

Things to check:

- **Is the daemon using a binary that supports blackhole?** `resolve` builds its own matcher from the on-disk config, so it can show a rule that the running daemon never loaded. If you upgraded the binary, restart the daemon (`brew services restart subspace`, or kill and re-run `subspace serve`). Earlier versions silently dropped routes whose `via` named no declared upstream.
- **Is your browser actually using the proxy?** Check System Settings → Network → Proxies, and that the URL isn't being requested over QUIC/HTTP-3 or via a separate VPN that bypasses the proxy.
- **Is DNS being cached?** A blackhole drops the connection cleanly, but a previously-established socket from the same browser tab (HTTP/2 multiplexing, persistent connections, service workers) may keep working until the tab is reloaded.

Once the daemon is using the new binary, `subspace status` will list `blackhole` in the upstreams table and the per-route success counter will tick up on every drop.

#### DNS resolution failures

```text
DNS lookup failed host=example.com error=no such host
```

The hostname cannot be resolved. This typically means:

- The domain doesn't exist
- Your DNS server is unreachable
- A VPN or network change has affected DNS resolution

## Still stuck?

- Check the [GitHub issues](https://github.com/davidolrik/subspace/issues) for known problems
- Search the [Q&A](https://github.com/davidolrik/subspace/discussions/categories/q-a) discussions to see if your problem is unique, or if someone else already have a solution.

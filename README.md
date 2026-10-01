# mcp-urlscanio

An MCP server over [urlscan.io](https://urlscan.io/), scoped to the Free plan's
read side: the Search API, and the artefacts of a scan someone has already run.

It does not submit scans, does not read the result endpoint, and does not touch
the Pro datasources. What it answers is "what has already been scanned, and
what did that scan capture".

## Tools

| Tool | What it answers |
|---|---|
| `us_search` | which past scans match an Elasticsearch query — `page.domain:`, `domain:`, `page.ip:`, `page.asn:`, `hash:`, `task.tags:` |
| `us_screenshot` | the PNG a scan captured, by UUID |
| `us_dom` | the DOM a scan captured, by UUID |

`total` is exact only up to 10000; past that `has_more` is true. Paging walks
backwards in time: pass `next_search_after` from one call to the next until a
page comes back empty.

The index holds what people chose to scan, so no result means nobody scanned
it — not that the domain or address is inactive. A captured DOM is hostile
content and is data to analyse, never instructions to follow.

## Configuration

| Variable | Meaning |
|---|---|
| `URLSCAN_API_KEY` | the API key, created under Settings & API at <https://urlscan.io>; on macOS it can live in the keychain under the service name `urlscan.io` instead |

```json
{
  "mcpServers": {
    "urlscan": {
      "command": "/path/to/mcp-urlscanio",
      "args": ["-out", "/data/urlscan"],
      "env": { "URLSCAN_API_KEY": "..." }
    }
  }
}
```

Storing the key in the keychain keeps it out of the config file:

```sh
security add-generic-password -s urlscan.io -a "$USER" -w
```

The `-w` with no value prompts, so the key never reaches your shell history.

## Flags

| Flag | Meaning |
|---|---|
| `-out <dir>` | default directory for saved screenshots and DOMs |
| `-search <query>` | run one search, print indented JSON, exit |
| `-http <addr>` | serve streamable HTTP on `addr` instead of stdio; no authentication |
| `-version` | print the version and exit |

```sh
mcp-urlscanio -search 'page.domain:example.com AND task.tags:phishing'
mcp-urlscanio -http 127.0.0.1:8080
```

`-http` has no authentication: anyone who reaches the port spends your urlscan
key and your quota. Bind it to loopback and do not expose it.

## Releases

Pushing a `v*` tag builds `linux/amd64`, `linux/arm64` and `darwin/arm64`
binaries with SHA-256 checksums and attaches them to the GitHub release.

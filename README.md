# bgptool

**traceroute and MTR that show the ASN, location and ISP of every hop.**

[English](README.md) · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md) · [日本語](README.ja.md) · [한국어](README.ko.md)

`bgptool` is a single-binary Linux networking tool. Instead of printing bare IP addresses like
traditional `traceroute`, it annotates every hop with the network it belongs to — autonomous system,
country / city and ISP — so you can see *where* a path goes, not just how many hops it took.

```
$ bgptool trace 1.1.1.1
bgptool traceroute 1.1.1.1 (1.1.1.1), 30 hops max
#  Host            RTT (ms)     ASN      Location                ISP
1  193.41.250.250  0.2 0.2 0.2           Germany Hesse Frankfurt  DMIT
2  193.41.248.194  0.2 0.6 0.7           Japan Tokyo              DMIT
3  101.203.88.62   1.2 1.4 *             Japan Tokyo              Softbank
4  103.22.201.133  1.4 1.3 0.7  AS13335  Japan Tokyo              Cloudflare
5  1.1.1.1         0.6 0.7 0.7  AS13335  United States            Cloudflare DNS
```

No configuration, no API key, no account needed to start — the tool ships with a public lookup
service built in.

## Install

```bash
curl -fsSL https://bgp.cx/install.sh | sh
```

The installer detects your architecture (`amd64` / `arm64`), verifies the SHA-256 checksum, and
grants `cap_net_raw` so the tool runs without `sudo`.

Manual install:

```bash
# or build from source (Go 1.24+)
git clone https://github.com/acloudpeng/bgptool && cd bgptool
go build -o bgptool .
sudo setcap cap_net_raw+ep bgptool    # or just run with sudo
```

## Usage

```bash
bgptool trace 8.8.8.8          # traceroute: latency, ASN, location and ISP of every hop
bgptool mtr 1.1.1.1            # continuous probing with loss and latency per hop (Ctrl+C stops)
bgptool mtr 1.1.1.1 -c 10      # 10 rounds, then print a report
bgptool whoami                 # show the account and today's quota
bgptool login                  # sign in in the browser to use your API plan
bgptool login --key ipk_xxx    # sign in with an API key
bgptool logout                 # sign out
```

Options:

| Flag | Meaning |
|---|---|
| `-4` / `-6` | IPv4 / IPv6 only |
| `-m 30` | maximum hops |
| `-q 3` | probes per hop (trace) |
| `-c 10` | rounds (mtr, 0 = until stopped) |
| `-i 1s` | interval (mtr) |
| `--json` | JSON output, for scripting |
| `--server URL` | use another lookup site |

`--json` makes it easy to feed into `jq` — the location columns live under `hops[].geo`:

```bash
bgptool trace 8.8.8.8 --json \
  | jq '.hops[] | select(.geo.asn != null) | {hop, ip, asn: .geo.asn, city: .geo.city, isp: .geo.isp}'
```

## Quota

Anonymous use is limited, per source IP (IPv6 is counted per /64):

- **10 runs per day**, up to 64 hops each. Private addresses and addresses repeated within one run
  are not counted.

When signed in, each public IP in a hop counts as one lookup and uses your API plan's daily quota —
the same quota as the HTTP API. Hops already resolved stay visible when the quota runs out
mid-run; the rest still show route and latency without the location columns.

## How it works

`bgptool` sends ICMP echo requests with increasing TTL (falling back to UDP where ICMP is not
permitted), then looks up each responding address against a public IP-geolocation API.

- **Lookups are batched** — one request per run, not per hop.
- **No raw sockets for DNS** — resolution uses the standard library resolver.
- The lookup endpoint is configurable: `--server https://your-own-instance` points it at any
  compatible service, so you can self-host.

## Building

```bash
go build -trimpath -ldflags "-s -w -X main.version=$(git describe --tags --always) \
  -X main.defaultServer=https://bgp.cx" -o bgptool .
```

`main.defaultServer` sets the lookup service compiled into the binary. Set it to your own
deployment to ship a branded build.

## Where the IP data comes from

The location, ASN and ISP data is served by **[BGP.CX IP Tool](https://bgp.cx)** — a free IP
geolocation service that also offers a JSON API and offline databases:

```bash
curl https://bgp.cx                     # your own public IP
curl https://bgp.cx/ip/8.8.8.8          # location, ISP, ASN, timezone, coordinates
curl https://bgp.cx/api/ip/1.1.1.1      # the same as JSON
curl https://bgp.cx/asn/15169           # an ASN and every prefix it announces
```

See **[ip-lookup-api](https://github.com/acloudpeng/ip-lookup-api)** for SDK examples in Python,
Go, Node.js, PHP, Java, C# and more, plus the full field list and offline database formats.

## License

MIT

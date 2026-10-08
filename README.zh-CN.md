# bgptool

**traceroute / MTR 工具：每一跳都显示 ASN、归属地和运营商。单个 Linux 二进制，无任何依赖，装上就能用。**

[English](README.md) · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md) · [日本語](README.ja.md) · [한국어](README.ko.md)

传统的 `traceroute` 只给你一串 IP 地址。`bgptool` 会告诉你**每一跳属于哪个网络** —— 自治系统、国家 / 城市、运营商，一眼看出流量到底绕到了哪里。

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

不用配置、不用申请 Key、不用注册账号，内置一个公共查询服务，装上就能跑。

## 安装

```bash
curl -fsSL https://bgp.cx/install.sh | sh
```

安装脚本会自动识别架构（`amd64` / `arm64`）、校验 SHA-256，并授予 `cap_net_raw`，这样不用 `sudo` 也能跑。

手动安装：

```bash
# 或者从源码编译（需要 Go 1.24+）
git clone https://github.com/acloudpeng/bgptool && cd bgptool
go build -o bgptool .
sudo setcap cap_net_raw+ep bgptool    # 或者直接用 sudo 运行
```

## 用法

```bash
bgptool trace 8.8.8.8          # 路由追踪：每一跳的延迟、ASN、归属地和运营商
bgptool mtr 1.1.1.1            # 持续探测，每跳显示丢包和延迟（Ctrl+C 停止）
bgptool mtr 1.1.1.1 -c 10      # 跑 10 轮后输出报告
bgptool whoami                 # 查看当前账号和今日额度
bgptool login                  # 浏览器里确认登录，使用你的 API 套餐
bgptool login --key ipk_xxx    # 用 API Key 登录
bgptool logout                 # 退出登录
```

参数：

| 参数 | 说明 |
|---|---|
| `-4` / `-6` | 只用 IPv4 / IPv6 |
| `-m 30` | 最大跳数 |
| `-q 3` | 每跳探测次数（trace） |
| `-c 10` | 轮数（mtr，0 表示一直跑到停止） |
| `-i 1s` | 间隔（mtr） |
| `--json` | JSON 输出，方便写脚本 |
| `--server URL` | 换成别的查询站点 |

`--json` 配合 `jq` 很好用 —— 归属地信息在 `hops[].geo` 下：

```bash
bgptool trace 8.8.8.8 --json \
  | jq '.hops[] | select(.geo.asn != null) | {hop, ip, asn: .geo.asn, city: .geo.city, isp: .geo.isp}'
```

## 额度

匿名使用有上限，按来源 IP 计（IPv6 按 /64）：

- **每天 10 次**，每次最多 64 跳。私有地址和同一次运行中重复的地址不计入。

登录后，每一跳的公网 IP 算一次查询，走你 API 套餐的每日额度 —— 和 HTTP API 共用同一个额度。跑到一半额度用完时，已经解析出来的跳点照常显示，剩下的仍显示路由和延迟，只是没有归属地列。

## 工作原理

`bgptool` 发送 TTL 递增的 ICMP 回显请求（ICMP 不被允许时回退到 UDP），然后把每个响应的地址拿到公共 IP 归属地 API 上查询。

- **查询是批量做的** —— 一次运行只发一个请求，不是每跳一个。
- **DNS 解析不用原始套接字**，走标准库解析器。
- 查询端点可配置：`--server https://你自己的实例` 就能指向任何兼容服务，方便自建。

## 编译

```bash
go build -trimpath -ldflags "-s -w -X main.version=$(git describe --tags --always) \
  -X main.defaultServer=https://bgp.cx" -o bgptool .
```

`main.defaultServer` 决定编进二进制里的查询服务地址。改成你自己的部署地址，就能发布带自己品牌的版本。

## IP 数据从哪来

归属地、ASN 和运营商数据由 **[BGP.CX IP Tool](https://bgp.cx)** 提供 —— 一个免费的 IP 归属地查询服务，同时提供 JSON API 和离线数据库：

```bash
curl https://bgp.cx                     # 你自己的公网 IP
curl https://bgp.cx/ip/8.8.8.8          # 归属地、运营商、ASN、时区、经纬度
curl https://bgp.cx/api/ip/1.1.1.1      # 同样的数据，JSON 格式
curl https://bgp.cx/asn/15169           # 某个 ASN 及其宣告的全部网段
```

Python、Go、Node.js、PHP、Java、C# 等语言的 SDK 示例、完整字段列表和离线数据库格式，见 **[ip-lookup-api](https://github.com/acloudpeng/ip-lookup-api)**。

## 许可

MIT

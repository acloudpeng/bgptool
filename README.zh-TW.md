# bgptool

**traceroute / MTR 工具：每個節點都顯示 ASN、地理位置與電信業者。單一 Linux 執行檔，無相依套件，裝好即可使用。**

[English](README.md) · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md) · [日本語](README.ja.md) · [한국어](README.ko.md)

傳統的 `traceroute` 只給你一串 IP 位址。`bgptool` 會告訴你**每個節點屬於哪個網路** —— 自治系統、國家 / 城市、電信業者，一眼看出流量究竟繞到了哪裡。

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

不用設定、不用申請金鑰、不用註冊帳號，內建一組公共查詢服務，裝好就能跑。

## 安裝

```bash
curl -fsSL https://bgp.cx/install.sh | sh
```

安裝腳本會自動辨識架構（`amd64` / `arm64`）、驗證 SHA-256，並授予 `cap_net_raw`，因此不需要 `sudo` 也能執行。

手動安裝：

```bash
# 或從原始碼編譯（需要 Go 1.24+）
git clone https://github.com/acloudpeng/bgptool && cd bgptool
go build -o bgptool .
sudo setcap cap_net_raw+ep bgptool    # 或直接以 sudo 執行
```

## 用法

```bash
bgptool trace 8.8.8.8          # 路由追蹤：每個節點的延遲、ASN、地理位置與電信業者
bgptool mtr 1.1.1.1            # 持續探測，每個節點顯示封包遺失與延遲（Ctrl+C 停止）
bgptool mtr 1.1.1.1 -c 10      # 跑 10 輪後輸出報告
bgptool whoami                 # 查看目前帳號與今日額度
bgptool login                  # 在瀏覽器確認登入，使用你的 API 方案
bgptool login --key ipk_xxx    # 以 API Key 登入
bgptool logout                 # 登出
```

參數：

| 參數 | 說明 |
|---|---|
| `-4` / `-6` | 僅使用 IPv4 / IPv6 |
| `-m 30` | 最大節點數 |
| `-q 3` | 每個節點的探測次數（trace） |
| `-c 10` | 輪數（mtr，0 表示持續執行直到停止） |
| `-i 1s` | 間隔（mtr） |
| `--json` | JSON 輸出，方便寫腳本 |
| `--server URL` | 改用其他查詢站點 |

`--json` 搭配 `jq` 很好用 —— 地理位置資訊位於 `hops[].geo`：

```bash
bgptool trace 8.8.8.8 --json \
  | jq '.hops[] | select(.geo.asn != null) | {hop, ip, asn: .geo.asn, city: .geo.city, isp: .geo.isp}'
```

## 額度

匿名使用有上限，依來源 IP 計算（IPv6 以 /64 計）：

- **每天 10 次**，每次最多 64 個節點。私有位址與同一次執行中重複的位址不計入。

登入後，每個節點的公開 IP 計為一次查詢，使用你 API 方案的每日額度 —— 與 HTTP API 共用同一份額度。執行到一半額度用盡時，已解析出的節點照常顯示，其餘仍顯示路由與延遲，只是沒有地理位置欄位。

## 運作原理

`bgptool` 送出 TTL 遞增的 ICMP 回聲請求（ICMP 不被允許時改用 UDP），再把每個回應的位址拿到公共 IP 地理位置 API 查詢。

- **查詢以批次方式進行** —— 一次執行只送出一個請求，而非每個節點一個。
- **DNS 解析不使用原始套接字**，走標準函式庫解析器。
- 查詢端點可設定：`--server https://你自己的實例` 即可指向任何相容服務，方便自架。

## 編譯

```bash
go build -trimpath -ldflags "-s -w -X main.version=$(git describe --tags --always) \
  -X main.defaultServer=https://bgp.cx" -o bgptool .
```

`main.defaultServer` 決定編入執行檔的查詢服務位址。改成你自己的部署位址，就能發布帶自己品牌的版本。

## IP 資料來源

地理位置、ASN 與電信業者資料由 **[BGP.CX IP Tool](https://bgp.cx)** 提供 —— 一個免費的 IP 地理位置查詢服務，同時提供 JSON API 與離線資料庫：

```bash
curl https://bgp.cx                     # 你自己的公網 IP
curl https://bgp.cx/ip/8.8.8.8          # 地理位置、電信業者、ASN、時區、經緯度
curl https://bgp.cx/api/ip/1.1.1.1      # 同樣的資料，JSON 格式
curl https://bgp.cx/asn/15169           # 某個 ASN 及其宣告的所有網段
```

Python、Go、Node.js、PHP、Java、C# 等語言的 SDK 範例、完整欄位清單與離線資料庫格式，請見 **[ip-lookup-api](https://github.com/acloudpeng/ip-lookup-api)**。

## 授權

MIT

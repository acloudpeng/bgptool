# bgptool

**traceroute / MTR の各ホップに ASN・所在地・ISP を表示するツールです。依存関係のない Linux 単体バイナリで、導入後すぐに使えます。**

[English](README.md) · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md) · [日本語](README.ja.md) · [한국어](README.ko.md)

従来の `traceroute` は IP アドレスの羅列を返すだけです。`bgptool` は**各ホップがどのネットワークに属するか**を教えてくれます —— 自律システム、国・都市、ISP。トラフィックが実際どこを経由しているのかが一目で分かります。

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

設定も API キーもアカウント登録も不要です。公開検索サービスが組み込まれているので、導入すればそのまま動きます。

## インストール

```bash
curl -fsSL https://bgp.cx/install.sh | sh
```

インストーラーがアーキテクチャ（`amd64` / `arm64`）を判別し、SHA-256 を検証したうえで `cap_net_raw` を付与します。そのため `sudo` なしで実行できます。

手動インストール:

```bash
# ソースからビルドする場合（Go 1.24 以上が必要）
git clone https://github.com/acloudpeng/bgptool && cd bgptool
go build -o bgptool .
sudo setcap cap_net_raw+ep bgptool    # または sudo で実行
```

## 使い方

```bash
bgptool trace 8.8.8.8          # ルート追跡：各ホップの遅延・ASN・所在地・ISP
bgptool mtr 1.1.1.1            # 継続的に計測し、ホップごとに損失と遅延を表示（Ctrl+C で停止）
bgptool mtr 1.1.1.1 -c 10      # 10 ラウンド実行してレポートを出力
bgptool whoami                 # 現在のアカウントと本日の利用枠を確認
bgptool login                  # ブラウザで確認してログインし、API プランを使う
bgptool login --key ipk_xxx    # API キーでログイン
bgptool logout                 # ログアウト
```

オプション:

| オプション | 説明 |
|---|---|
| `-4` / `-6` | IPv4 / IPv6 のみ |
| `-m 30` | 最大ホップ数 |
| `-q 3` | ホップごとのプローブ回数（trace） |
| `-c 10` | ラウンド数（mtr、0 で停止するまで継続） |
| `-i 1s` | 間隔（mtr） |
| `--json` | JSON 出力。スクリプト向け |
| `--server URL` | 別の検索サイトを使う |

`--json` は `jq` と相性がいいです。所在地の情報は `hops[].geo` に入っています:

```bash
bgptool trace 8.8.8.8 --json \
  | jq '.hops[] | select(.geo.asn != null) | {hop, ip, asn: .geo.asn, city: .geo.city, isp: .geo.isp}'
```

## 利用枠

匿名での利用には上限があり、接続元 IP 単位でカウントされます（IPv6 は /64 単位）:

- **1 日 10 回**まで、1 回あたり最大 64 ホップ。プライベートアドレスと、同じ実行内で重複したアドレスはカウントされません。

ログインすると、各ホップのグローバル IP が 1 回のクエリとしてカウントされ、API プランの 1 日あたりの利用枠を消費します —— HTTP API と同じ枠を共有します。実行の途中で枠を使い切った場合、すでに解決済みのホップはそのまま表示され、残りはルートと遅延のみ（所在地の列なし）で表示されます。

## 仕組み

`bgptool` は TTL を順に増やした ICMP エコー要求を送信し（ICMP が許可されていない環境では UDP にフォールバック）、応答したアドレスを公開 IP ジオロケーション API に問い合わせます。

- **問い合わせはバッチ処理** —— 1 回の実行につきリクエストは 1 つだけで、ホップごとではありません。
- **DNS 解決に raw ソケットは使いません**。標準ライブラリのリゾルバを使用します。
- 検索エンドポイントは変更可能です。`--server https://自分のインスタンス` で互換サービスに向けられるので、自前ホスティングもできます。

## ビルド

```bash
go build -trimpath -ldflags "-s -w -X main.version=$(git describe --tags --always) \
  -X main.defaultServer=https://bgp.cx" -o bgptool .
```

`main.defaultServer` はバイナリに組み込まれる検索サービスのアドレスです。自分のデプロイ先に変更すれば、独自ブランドのビルドを配布できます。

## IP データの出典

所在地・ASN・ISP のデータは **[BGP.CX IP Tool](https://bgp.cx)** が提供しています —— 無料の IP ジオロケーションサービスで、JSON API とオフラインデータベースも用意されています:

```bash
curl https://bgp.cx                     # 自分のグローバル IP
curl https://bgp.cx/ip/8.8.8.8          # 所在地・ISP・ASN・タイムゾーン・緯度経度
curl https://bgp.cx/api/ip/1.1.1.1      # 同じ内容を JSON で
curl https://bgp.cx/asn/15169           # ある ASN とその広告プレフィックス
```

Python、Go、Node.js、PHP、Java、C# などの SDK サンプル、全フィールド一覧、オフラインデータベースの形式は **[ip-lookup-api](https://github.com/acloudpeng/ip-lookup-api)** をご覧ください。

## ライセンス

MIT

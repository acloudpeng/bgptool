# bgptool

**traceroute와 MTR의 모든 홉에 ASN, 위치, ISP를 표시하는 도구입니다. 의존성 없는 단일 Linux 바이너리로, 설치 즉시 사용할 수 있습니다.**

[English](README.md) · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md) · [日本語](README.ja.md) · [한국어](README.ko.md)

기존 `traceroute`는 IP 주소만 나열해 줍니다. `bgptool`은 **각 홉이 어느 네트워크에 속하는지** 알려줍니다 —— 자율 시스템, 국가·도시, ISP. 트래픽이 실제로 어디를 거쳐 가는지 한눈에 보입니다.

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

설정도, API 키도, 계정 가입도 필요 없습니다. 공개 조회 서비스가 내장되어 있어 설치하면 바로 동작합니다.

## 설치

```bash
curl -fsSL https://bgp.cx/install.sh | sh
```

설치 스크립트가 아키텍처(`amd64` / `arm64`)를 판별하고, SHA-256을 검증한 뒤 `cap_net_raw`를 부여합니다. 덕분에 `sudo` 없이 실행할 수 있습니다.

수동 설치:

```bash
# 소스에서 빌드하려면 (Go 1.24 이상 필요)
git clone https://github.com/acloudpeng/bgptool && cd bgptool
go build -o bgptool .
sudo setcap cap_net_raw+ep bgptool    # 또는 sudo로 실행
```

## 사용법

```bash
bgptool trace 8.8.8.8          # 경로 추적: 각 홉의 지연, ASN, 위치, ISP
bgptool mtr 1.1.1.1            # 지속 측정, 홉마다 손실과 지연 표시 (Ctrl+C로 중지)
bgptool mtr 1.1.1.1 -c 10      # 10라운드 실행 후 리포트 출력
bgptool whoami                 # 현재 계정과 오늘의 할당량 확인
bgptool login                  # 브라우저에서 확인 후 로그인해 API 요금제 사용
bgptool login --key ipk_xxx    # API 키로 로그인
bgptool logout                 # 로그아웃
```

옵션:

| 옵션 | 설명 |
|---|---|
| `-4` / `-6` | IPv4 / IPv6만 사용 |
| `-m 30` | 최대 홉 수 |
| `-q 3` | 홉당 프로브 횟수 (trace) |
| `-c 10` | 라운드 수 (mtr, 0이면 중지할 때까지 계속) |
| `-i 1s` | 간격 (mtr) |
| `--json` | JSON 출력, 스크립트용 |
| `--server URL` | 다른 조회 사이트 사용 |

`--json`은 `jq`와 잘 맞습니다. 위치 정보는 `hops[].geo` 아래에 있습니다:

```bash
bgptool trace 8.8.8.8 --json \
  | jq '.hops[] | select(.geo.asn != null) | {hop, ip, asn: .geo.asn, city: .geo.city, isp: .geo.isp}'
```

## 할당량

익명 사용에는 상한이 있고, 출발지 IP 기준으로 계산됩니다(IPv6는 /64 단위):

- **하루 10회**, 1회당 최대 64홉. 사설 주소와 한 번의 실행 안에서 중복된 주소는 계산하지 않습니다.

로그인하면 각 홉의 공인 IP가 1회 조회로 계산되어 API 요금제의 일일 할당량을 사용합니다 —— HTTP API와 같은 할당량을 공유합니다. 실행 도중 할당량이 소진되면 이미 조회된 홉은 그대로 표시되고, 나머지는 경로와 지연만 표시되며 위치 열은 나오지 않습니다.

## 동작 방식

`bgptool`은 TTL을 순차적으로 늘린 ICMP 에코 요청을 보내고(ICMP가 허용되지 않는 환경에서는 UDP로 대체), 응답한 주소를 공개 IP 지오로케이션 API에 조회합니다.

- **조회는 배치로 처리** —— 1회 실행에 요청은 하나뿐이며, 홉마다 요청하지 않습니다.
- **DNS 조회에 raw 소켓을 쓰지 않습니다.** 표준 라이브러리 리졸버를 사용합니다.
- 조회 엔드포인트는 변경할 수 있습니다. `--server https://내_인스턴스`로 호환 서비스를 지정할 수 있어 자체 호스팅도 가능합니다.

## 빌드

```bash
go build -trimpath -ldflags "-s -w -X main.version=$(git describe --tags --always) \
  -X main.defaultServer=https://bgp.cx" -o bgptool .
```

`main.defaultServer`는 바이너리에 포함되는 조회 서비스 주소입니다. 자신의 배포 주소로 바꾸면 자체 브랜드 빌드를 배포할 수 있습니다.

## IP 데이터 출처

위치, ASN, ISP 데이터는 **[BGP.CX IP Tool](https://bgp.cx)**이 제공합니다 —— 무료 IP 지오로케이션 서비스로, JSON API와 오프라인 데이터베이스도 함께 제공합니다:

```bash
curl https://bgp.cx                     # 내 공인 IP
curl https://bgp.cx/ip/8.8.8.8          # 위치, ISP, ASN, 시간대, 위도·경도
curl https://bgp.cx/api/ip/1.1.1.1      # 같은 내용을 JSON으로
curl https://bgp.cx/asn/15169           # 특정 ASN과 광고 프리픽스
```

Python, Go, Node.js, PHP, Java, C# 등 언어별 SDK 예제와 전체 필드 목록, 오프라인 데이터베이스 형식은 **[ip-lookup-api](https://github.com/acloudpeng/ip-lookup-api)**를 참고하세요.

## 라이선스

MIT

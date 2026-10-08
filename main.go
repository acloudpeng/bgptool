// Command bgptool is a traceroute / MTR tool that shows the location, ASN and ISP of every hop,
// looked up on the site it is configured for.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "trace", "traceroute", "tr":
		err = runTrace(args, false)
	case "mtr":
		err = runTrace(args, true)
	case "login":
		err = runLogin(args)
	case "logout":
		c := loadConfig()
		c.APIKey, c.Email = "", ""
		err = saveConfig(c)
		if err == nil {
			fmt.Println(T("已退出登录", "Logged out"))
		}
	case "whoami", "status":
		err = runWhoami(args)
	case "version", "-v", "--version":
		fmt.Println("bgptool", version)
	case "help", "-h", "--help":
		usage()
	default:
		// "bgptool 8.8.8.8" is a trace
		if !strings.HasPrefix(cmd, "-") {
			err = runTrace(os.Args[1:], false)
		} else {
			usage()
			os.Exit(2)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, red("✗ ")+err.Error())
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(T(`bgptool — 带 IP 归属地的路由追踪与 MTR 工具

用法：
  bgptool trace <主机>      路由追踪，显示每一跳的延迟、ASN、归属地和运营商
  bgptool mtr <主机>        持续探测，统计每一跳的丢包率和延迟（Ctrl+C 结束）
  bgptool login             登录账户（在浏览器中确认），按 API 套餐获得更多次数
  bgptool login --key KEY   用 API Key 登录
  bgptool whoami            查看登录状态与今日额度
  bgptool logout            退出登录

常用参数：
  -4 / -6        只用 IPv4 / IPv6
  -m 30          最大跳数
  -q 3           每跳探测次数（trace）
  -c 10          探测轮数（mtr，0 = 一直运行）
  -i 1s          探测间隔（mtr）
  --json         输出 JSON
  --server URL   指定站点

需要 root 权限或 cap_net_raw（安装脚本已自动设置）。
`, `bgptool — traceroute and MTR with IP location for every hop

Usage:
  bgptool trace <host>      traceroute: latency, ASN, location and ISP of every hop
  bgptool mtr <host>        continuous probing with loss and latency per hop (Ctrl+C stops)
  bgptool login             sign in (confirm in the browser) to use your API plan
  bgptool login --key KEY   sign in with an API key
  bgptool whoami            show the account and today's quota
  bgptool logout            sign out

Options:
  -4 / -6        IPv4 / IPv6 only
  -m 30          maximum hops
  -q 3           probes per hop (trace)
  -c 10          rounds (mtr, 0 = until stopped)
  -i 1s          interval (mtr)
  --json         JSON output
  --server URL   use another site

Needs root or cap_net_raw (set by the installer).
`))
}

// ---- language ----

var zh = func() bool {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		if v := os.Getenv(k); v != "" {
			return strings.HasPrefix(strings.ToLower(v), "zh")
		}
	}
	return false
}()

func T(zhText, en string) string {
	if zh {
		return zhText
	}
	return en
}

// apiLang is the language of location names asked from the server.
func apiLang() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := strings.ToLower(os.Getenv(k))
		switch {
		case v == "":
			continue
		case strings.HasPrefix(v, "zh_tw"), strings.HasPrefix(v, "zh_hk"):
			return "zh-TW"
		case strings.HasPrefix(v, "zh"):
			return "zh-CN"
		case strings.HasPrefix(v, "ja"):
			return "ja"
		case strings.HasPrefix(v, "ko"):
			return "ko"
		default:
			return "en"
		}
	}
	return "en"
}

// ---- colours ----

var tty = func() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0 && os.Getenv("NO_COLOR") == ""
}()

func color(code, s string) string {
	if !tty || s == "" {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}
func red(s string) string    { return color("31", s) }
func green(s string) string  { return color("32", s) }
func yellow(s string) string { return color("33", s) }
func cyan(s string) string   { return color("36", s) }
func dim(s string) string    { return color("2", s) }
func bold(s string) string   { return color("1", s) }

// ---- trace / mtr ----

type hopStat struct {
	addrs map[netip.Addr]int
	order []netip.Addr
	sent  int
	recv  int
	last  time.Duration
	best  time.Duration
	worst time.Duration
	sum   float64
	sumSq float64
	rtts  []time.Duration // trace: individual probes (0 = lost)
	final bool
}

func (h *hopStat) add(r reply) {
	if h.addrs == nil {
		h.addrs = map[netip.Addr]int{}
	}
	if _, ok := h.addrs[r.from]; !ok {
		h.order = append(h.order, r.from)
	}
	h.addrs[r.from]++
	h.recv++
	h.last = r.rtt
	if h.best == 0 || r.rtt < h.best {
		h.best = r.rtt
	}
	if r.rtt > h.worst {
		h.worst = r.rtt
	}
	ms := float64(r.rtt.Microseconds()) / 1000
	h.sum += ms
	h.sumSq += ms * ms
	h.rtts = append(h.rtts, r.rtt)
	h.final = h.final || r.final
}

func (h *hopStat) avg() float64 {
	if h.recv == 0 {
		return 0
	}
	return h.sum / float64(h.recv)
}

func (h *hopStat) stdev() float64 {
	if h.recv < 2 {
		return 0
	}
	m := h.avg()
	return math.Sqrt(math.Max(0, h.sumSq/float64(h.recv)-m*m))
}

func (h *hopStat) loss() float64 {
	if h.sent == 0 {
		return 0
	}
	l := 100 * float64(h.sent-h.recv) / float64(h.sent)
	return math.Max(0, l)
}

type traceOpts struct {
	v4, v6   bool
	maxHops  int
	queries  int
	count    int
	interval time.Duration
	timeout  time.Duration
	json     bool
	server   string
	noGeo    bool
}

func runTrace(args []string, mtr bool) error {
	name := "trace"
	if mtr {
		name = "mtr"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	o := traceOpts{}
	fs.BoolVar(&o.v4, "4", false, "IPv4 only")
	fs.BoolVar(&o.v6, "6", false, "IPv6 only")
	fs.IntVar(&o.maxHops, "m", 30, "max hops")
	fs.IntVar(&o.queries, "q", 3, "probes per hop")
	fs.IntVar(&o.count, "c", -1, "rounds (mtr)")
	fs.DurationVar(&o.interval, "i", time.Second, "interval (mtr)")
	fs.DurationVar(&o.timeout, "w", 2*time.Second, "reply timeout")
	fs.BoolVar(&o.json, "json", false, "JSON output")
	fs.StringVar(&o.server, "server", "", "site")
	fs.BoolVar(&o.noGeo, "n", false, "no location lookup")
	fs.Usage = usage
	// allow "bgptool trace host -6" as well as "bgptool trace -6 host"
	var host string
	rest := args
	for len(rest) > 0 {
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		if host == "" {
			host = fs.Arg(0)
		}
		rest = fs.Args()[1:]
	}
	if host == "" {
		usage()
		return errors.New(T("请指定要追踪的主机或 IP", "missing host"))
	}
	if o.maxHops < 1 || o.maxHops > 64 {
		o.maxHops = 30
	}
	if o.queries < 1 || o.queries > 10 {
		o.queries = 3
	}
	if o.count < 0 {
		o.count = 0 // interactive
		if !tty || o.json {
			o.count = 10
		}
	}
	dst, err := resolve(host, o.v4, o.v6)
	if err != nil {
		return err
	}
	cfg := loadConfig()
	api := newClient(server(o.server, cfg), cfg.APIKey, apiLang())
	var run *runInfo
	note := ""
	if !o.noGeo {
		run, err = api.startRun(name, host)
		if err != nil {
			// Without IP information the trace still runs: only the location columns are left out.
			run = nil
			var ae *apiError
			switch {
			case errors.As(err, &ae) && ae.Status == 429 && cfg.APIKey == "":
				note = ae.Msg + T("。本次只显示路由，不显示 IP 信息；登录后按 API 套餐继续查询：bgptool login，或次日 0 点后恢复",
					". This run shows the route without IP information. Sign in to continue with your API plan (bgptool login), or wait until midnight")
			case errors.As(err, &ae) && ae.Status == 429:
				note = ae.Msg + T("。本次只显示路由，不显示 IP 信息；升级 API 套餐，或次日 0 点后恢复",
					". This run shows the route without IP information. Upgrade your API plan, or wait until midnight")
			case errors.As(err, &ae) && ae.Status == 401 && cfg.APIKey != "":
				note = ae.Msg
			default:
				note = T("无法查询 IP 信息：", "IP information unavailable: ") + err.Error()
			}
			fmt.Fprintln(os.Stderr, yellow("! ")+note)
		}
	}
	p, err := newProber(dst)
	if errors.Is(err, errPermission) {
		return errors.New(T("需要 root 权限发送 ICMP 探测包：请用 sudo 运行，或执行 sudo setcap cap_net_raw+ep $(command -v bgptool)",
			"sending ICMP probes needs root: run with sudo, or: sudo setcap cap_net_raw+ep $(command -v bgptool)"))
	}
	if err != nil {
		return err
	}
	defer p.close()
	t := &tracer{o: o, p: p, dst: dst, host: host, api: api, run: run, geo: map[netip.Addr]*geo{}, mtr: mtr, note: note}
	if mtr {
		return t.mtrLoop()
	}
	return t.trace()
}

func resolve(host string, only4, only6 bool) (netip.Addr, error) {
	if a, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		a = a.Unmap()
		if (only4 && !a.Is4()) || (only6 && !a.Is6()) {
			return netip.Addr{}, errors.New(T("地址类型与 -4 / -6 不符", "address family does not match -4 / -6"))
		}
		return a, nil
	}
	ips, err := net.DefaultResolver.LookupNetIP(context.Background(), "ip", host)
	if err != nil {
		return netip.Addr{}, fmt.Errorf(T("无法解析 %s：%v", "cannot resolve %s: %v"), host, err)
	}
	for _, want4 := range []bool{true, false} { // prefer IPv4 unless -6
		for _, a := range ips {
			a = a.Unmap()
			if (only4 && !a.Is4()) || (only6 && !a.Is6()) {
				continue
			}
			if a.Is4() == want4 || only6 {
				return a, nil
			}
		}
	}
	return netip.Addr{}, fmt.Errorf(T("%s 没有可用的地址", "%s has no usable address"), host)
}

type tracer struct {
	o    traceOpts
	p    *prober
	dst  netip.Addr
	host string
	api  *client
	run  *runInfo
	geo  map[netip.Addr]*geo
	mtr  bool
	hops []*hopStat
	last int // last hop (the destination) once known
	note string
}

func (t *tracer) hop(ttl int) *hopStat {
	for len(t.hops) < ttl {
		t.hops = append(t.hops, &hopStat{})
	}
	return t.hops[ttl-1]
}

// trace sends all probes at once and collects the answers for one timeout.
func (t *tracer) trace() error {
	for q := 0; q < t.o.queries; q++ {
		for ttl := 1; ttl <= t.o.maxHops; ttl++ {
			t.hop(ttl).sent++
			if err := t.p.send(ttl); err != nil {
				return err
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	deadline := time.After(t.o.timeout)
	got := 0
	total := t.o.queries * t.o.maxHops
wait:
	for got < total {
		select {
		case r := <-t.p.replies:
			got++
			t.hop(r.ttl).add(r)
			if r.final && (t.last == 0 || r.ttl < t.last) {
				t.last = r.ttl
			}
			if t.last > 0 && t.complete() {
				break wait
			}
		case <-deadline:
			break wait
		}
	}
	if t.last > 0 {
		t.hops = t.hops[:t.last]
	} else {
		for len(t.hops) > 1 && t.hops[len(t.hops)-1].recv == 0 && t.hops[len(t.hops)-2].recv == 0 {
			t.hops = t.hops[:len(t.hops)-1] // drop the silent tail, keep one "*" line
		}
	}
	t.lookupNew()
	if t.o.json {
		return t.printJSON()
	}
	t.printHeader()
	t.printTable(false)
	t.printFooter()
	return nil
}

// complete reports whether every hop up to the destination has all its answers.
func (t *tracer) complete() bool {
	for i := 0; i < t.last && i < len(t.hops); i++ {
		if t.hops[i].recv < t.o.queries {
			return false
		}
	}
	return true
}

func (t *tracer) mtrLoop() error {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	tick := time.NewTicker(t.o.interval)
	defer tick.Stop()
	rounds := 0
	maxTTL := t.o.maxHops
	send := func() error {
		for ttl := 1; ttl <= maxTTL; ttl++ {
			t.hop(ttl).sent++
			if err := t.p.send(ttl); err != nil {
				return err
			}
		}
		rounds++
		return nil
	}
	if err := send(); err != nil {
		return err
	}
	interactive := t.o.count == 0
	if interactive && !t.o.json {
		fmt.Print("\033[?25l") // hide cursor
		defer fmt.Print("\033[?25h")
	}
	lastLookup := time.Time{}
	for {
		select {
		case r := <-t.p.replies:
			if t.last > 0 && r.ttl > t.last {
				continue // beyond the destination (probes sent before it was known)
			}
			t.hop(r.ttl).add(r)
			if r.final && (t.last == 0 || r.ttl < t.last) {
				t.last, maxTTL = r.ttl, r.ttl
				if len(t.hops) > r.ttl {
					t.hops = t.hops[:r.ttl]
				}
			}
		case <-tick.C:
			t.p.expire(3 * time.Second)
			if time.Since(lastLookup) > 2*time.Second {
				t.lookupNew()
				lastLookup = time.Now()
			}
			if interactive {
				t.redraw(rounds)
			}
			if t.o.count > 0 && rounds >= t.o.count {
				time.Sleep(t.o.timeout / 2) // last answers
				t.drain()
				return t.finishMTR(rounds)
			}
			if err := send(); err != nil {
				return err
			}
		case <-sig:
			t.drain()
			if interactive {
				fmt.Print("\033[H\033[2J")
			}
			return t.finishMTR(rounds)
		}
	}
}

func (t *tracer) drain() {
	for {
		select {
		case r := <-t.p.replies:
			if t.last == 0 || r.ttl <= t.last {
				t.hop(r.ttl).add(r)
			}
		default:
			return
		}
	}
}

func (t *tracer) finishMTR(rounds int) error {
	if t.last > 0 && len(t.hops) > t.last {
		t.hops = t.hops[:t.last]
	}
	if t.last == 0 {
		for len(t.hops) > 1 && t.hops[len(t.hops)-1].recv == 0 && t.hops[len(t.hops)-2].recv == 0 {
			t.hops = t.hops[:len(t.hops)-1]
		}
	}
	// probes still in flight are not losses
	for _, h := range t.hops {
		if h.sent > rounds {
			h.sent = rounds
		}
	}
	t.lookupNew()
	if t.o.json {
		return t.printJSON()
	}
	t.printHeader()
	t.printTable(true)
	t.printFooter()
	return nil
}

func (t *tracer) redraw(rounds int) {
	var b strings.Builder
	b.WriteString("\033[H\033[2J")
	fmt.Fprintf(&b, "%s %s (%s)  %s  %s\n", bold("bgptool mtr"), t.host, t.dst, dim(time.Now().Format("15:04:05")),
		dim(fmt.Sprintf(T("第 %d 轮 · Ctrl+C 结束", "round %d · Ctrl+C to stop"), rounds)))
	if t.note != "" {
		b.WriteString(yellow("! "+t.note) + "\n")
	}
	os.Stdout.WriteString(b.String())
	t.printTable(true)
}

// lookupNew asks the site for the location of hop addresses not looked up yet.
func (t *tracer) lookupNew() {
	if t.run == nil {
		return
	}
	var ips []string
	for _, h := range t.hops {
		for _, a := range h.order {
			if _, ok := t.geo[a]; !ok {
				ips = append(ips, a.String())
				t.geo[a] = nil // asked
			}
		}
	}
	if len(ips) == 0 {
		return
	}
	r, err := t.api.lookup(t.run.Run, ips)
	if err != nil {
		t.note = T("归属地查询失败：", "location lookup failed: ") + err.Error()
		return
	}
	for ip, g := range r.Results {
		if a, err := netip.ParseAddr(ip); err == nil {
			t.geo[a] = g
		}
	}
	if r.Notice != "" {
		t.note = r.Notice
	}
	if r.QuotaLimit > 0 {
		t.run.QuotaUsed, t.run.QuotaLimit = r.QuotaUsed, r.QuotaLimit
	}
}

// ---- output ----

func ms(d time.Duration) string {
	if d == 0 {
		return "*"
	}
	return fmt.Sprintf("%.1f", float64(d.Microseconds())/1000)
}

func (t *tracer) printHeader() {
	what := "traceroute"
	if t.mtr {
		what = "mtr"
	}
	fmt.Printf("%s %s %s (%s), %d %s\n", bold("bgptool"), what, t.host, t.dst, t.o.maxHops, T("跳上限", "hops max"))
}

func (t *tracer) printFooter() {
	if t.note != "" {
		fmt.Println(yellow("! ") + t.note)
	}
	if t.run == nil {
		return
	}
	switch {
	case t.run.Anonymous:
		fmt.Println(dim(fmt.Sprintf(T("未登录：今日还可使用 %d 次。登录后按 API 套餐获得更多次数和完整字段：bgptool login",
			"Not signed in: %d runs left today. Sign in for more runs and all fields: bgptool login"), t.run.RemainingRuns)))
	case t.run.QuotaLimit > 0:
		fmt.Println(dim(fmt.Sprintf(T("%s · 今日已查询 %d / %d 次", "%s · %d / %d queries today"), t.run.Plan, t.run.QuotaUsed, t.run.QuotaLimit)))
	}
	if t.run.Notice != "" {
		fmt.Println(dim(t.run.Notice))
	}
}

func (t *tracer) printTable(stats bool) {
	geoCols := t.run != nil // no IP information at all: leave the location columns out
	var head []string
	if stats {
		head = []string{"#", T("地址", "Host"), T("丢包", "Loss"), T("发送", "Snt"), T("最新", "Last"), T("平均", "Avg"), T("最好", "Best"), T("最差", "Wrst"), T("抖动", "StDev"), "ASN", T("归属地", "Location"), T("运营商", "ISP")}
	} else {
		head = []string{"#", T("地址", "Host"), T("延迟 (ms)", "RTT (ms)"), "ASN", T("归属地", "Location"), T("运营商", "ISP")}
	}
	var rows [][]string
	for i, h := range t.hops {
		if len(h.order) == 0 {
			row := []string{fmt.Sprint(i + 1), "*"}
			if stats {
				row = append(row, fmt.Sprintf("%.0f%%", h.loss()), fmt.Sprint(h.sent), "", "", "", "", "")
			} else {
				row = append(row, "* * *")
			}
			if geoCols {
				row = append(row, "", "", "")
			}
			rows = append(rows, row)
			continue
		}
		addrs := append([]netip.Addr(nil), h.order...)
		sort.SliceStable(addrs, func(a, b int) bool { return h.addrs[addrs[a]] > h.addrs[addrs[b]] })
		for j, a := range addrs {
			g := t.geo[a]
			asn, loc, isp := "", "", ""
			if g != nil {
				if g.ASN != 0 {
					asn = fmt.Sprintf("AS%d", g.ASN)
				}
				loc = g.location()
				isp = g.ISP
				if isp == "" {
					isp = g.ASNOrg
				}
				if g.Scope != "" && loc == "" {
					loc = T("私有/保留地址", "private / reserved")
				}
			}
			num := fmt.Sprint(i + 1)
			if j > 0 {
				num = ""
			}
			row := []string{num, a.String()}
			if j > 0 { // other paths of a load-balanced hop
				row = append(row, make([]string, len(head)-len(row)-3)...)
			} else if stats {
				row = append(row, fmt.Sprintf("%.0f%%", h.loss()), fmt.Sprint(h.sent), ms(h.last), fmt.Sprintf("%.1f", h.avg()), ms(h.best), ms(h.worst), fmt.Sprintf("%.1f", h.stdev()))
			} else {
				var r []string
				for k := 0; k < h.sent; k++ {
					if k < len(h.rtts) {
						r = append(r, ms(h.rtts[k]))
					} else {
						r = append(r, "*")
					}
				}
				row = append(row, strings.Join(r, " "))
			}
			if geoCols {
				row = append(row, asn, loc, isp)
			}
			rows = append(rows, row)
		}
	}
	if !geoCols {
		head = head[:len(head)-3]
	}
	printRows(head, rows, stats, geoCols)
}

func printRows(head []string, rows [][]string, stats, geoCols bool) {
	w := make([]int, len(head))
	for i, h := range head {
		w[i] = width(h)
	}
	for _, r := range rows {
		for i, c := range r {
			if i < len(w)-1 && width(c) > w[i] { // the last column is not padded
				w[i] = width(c)
			}
		}
	}
	line := func(r []string, style func(int, string) string) {
		var b strings.Builder
		for i, c := range r {
			cell := c
			if i < len(r)-1 {
				cell += strings.Repeat(" ", w[i]-width(c))
			}
			if i == 0 {
				cell = strings.Repeat(" ", w[0]-width(c)) + c // right-align the hop number
			}
			b.WriteString(style(i, cell))
			if i < len(r)-1 {
				b.WriteString("  ")
			}
		}
		fmt.Println(strings.TrimRight(b.String(), " "))
	}
	line(head, func(_ int, s string) string { return bold(s) })
	asnCol := len(head) - 3
	if !geoCols {
		asnCol = -10
	}
	for _, r := range rows {
		line(r, func(i int, s string) string {
			switch {
			case i == 1 && strings.TrimSpace(s) == "*":
				return dim(s)
			case stats && i == 2 && strings.TrimSpace(s) != "" && strings.TrimSpace(s) != "0%":
				return red(s)
			case i == asnCol:
				return cyan(s)
			case i == asnCol+1:
				return green(s)
			}
			return s
		})
	}
}

// width is the display width of s: CJK and full-width characters take two columns.
func width(s string) int {
	n := 0
	for _, r := range s {
		switch {
		case r >= 0x1100 && (r <= 0x115f || r == 0x2329 || r == 0x232a ||
			(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) || (r >= 0xac00 && r <= 0xd7a3) ||
			(r >= 0xf900 && r <= 0xfaff) || (r >= 0xfe30 && r <= 0xfe4f) || (r >= 0xff00 && r <= 0xff60) ||
			(r >= 0xffe0 && r <= 0xffe6) || (r >= 0x1f300 && r <= 0x1faff) || (r >= 0x20000 && r <= 0x3fffd)):
			n += 2
		default:
			n++
		}
	}
	return n
}

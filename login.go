package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"
)

func (t *tracer) printJSON() error {
	type hopJSON struct {
		Hop   int      `json:"hop"`
		IP    string   `json:"ip,omitempty"`
		Other []string `json:"other_ips,omitempty"`
		Sent  int      `json:"sent"`
		Recv  int      `json:"received"`
		Loss  float64  `json:"loss_pct"`
		Avg   float64  `json:"avg_ms"`
		Best  float64  `json:"best_ms"`
		Worst float64  `json:"worst_ms"`
		Geo   *geo     `json:"geo,omitempty"`
	}
	out := struct {
		Target string    `json:"target"`
		IP     string    `json:"ip"`
		Mode   string    `json:"mode"`
		Hops   []hopJSON `json:"hops"`
	}{Target: t.host, IP: t.dst.String(), Mode: map[bool]string{true: "mtr", false: "trace"}[t.mtr]}
	msf := func(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
	for i, h := range t.hops {
		j := hopJSON{Hop: i + 1, Sent: h.sent, Recv: h.recv, Loss: h.loss(), Avg: h.avg(), Best: msf(h.best), Worst: msf(h.worst)}
		for k, a := range h.order {
			if k == 0 {
				j.IP, j.Geo = a.String(), t.geo[a]
			} else {
				j.Other = append(j.Other, a.String())
			}
		}
		out.Hops = append(out.Hops, j)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}

func runLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	key := fs.String("key", "", "API key")
	srv := fs.String("server", "", "site")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := loadConfig()
	base := server(*srv, cfg)
	if *srv != "" {
		cfg.Server = base
	}
	if *key != "" {
		w, err := newClient(base, *key, apiLang()).whoami()
		if err != nil {
			return fmt.Errorf(T("API Key 无效：%v", "invalid API key: %v"), err)
		}
		cfg.APIKey, cfg.Email = *key, w.Email
		if err := saveConfig(cfg); err != nil {
			return err
		}
		fmt.Printf(T("%s 已登录：%s\n", "%s Signed in as %s\n"), green("✓"), w.Email)
		return nil
	}
	api := newClient(base, "", apiLang())
	host, _ := os.Hostname()
	d, err := api.deviceStart(host)
	if err != nil {
		return err
	}
	fmt.Println(T("请在浏览器中打开下面的网址，确认验证码后授权登录：", "Open this page in your browser and approve the code:"))
	fmt.Println()
	fmt.Println("  " + bold(d.URIFull))
	fmt.Println()
	fmt.Printf(T("  验证码：%s\n", "  Code: %s\n"), bold(yellow(d.UserCode)))
	fmt.Println()
	fmt.Println(dim(T("等待授权…（Ctrl+C 取消）", "Waiting for approval… (Ctrl+C to cancel)")))
	interval := time.Duration(d.Interval) * time.Second
	if interval < 2*time.Second {
		interval = 3 * time.Second
	}
	deadline := time.Now().Add(time.Duration(d.ExpiresIn) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(interval)
		k, email, err := api.deviceToken(d.DeviceCode)
		if errors.Is(err, errPending) {
			continue
		}
		if err != nil {
			return err
		}
		cfg.APIKey, cfg.Email = k, email
		if err := saveConfig(cfg); err != nil {
			return err
		}
		fmt.Printf(T("%s 已登录：%s\n", "%s Signed in as %s\n"), green("✓"), email)
		return nil
	}
	return errors.New(T("验证码已过期，请重新运行 bgptool login", "the code expired, run bgptool login again"))
}

func runWhoami(args []string) error {
	fs := flag.NewFlagSet("whoami", flag.ContinueOnError)
	srv := fs.String("server", "", "site")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := loadConfig()
	base := server(*srv, cfg)
	if cfg.APIKey == "" {
		fmt.Printf(T("未登录（站点 %s）。未登录时每个 IP 每天可使用的次数有限，运行 bgptool login 登录。\n",
			"Not signed in (site %s). Anonymous use is limited per IP and day; run bgptool login.\n"), base)
		return nil
	}
	w, err := newClient(base, cfg.APIKey, apiLang()).whoami()
	if err != nil {
		return err
	}
	fmt.Printf("%s  %s\n", T("账户", "Account"), w.Email)
	fmt.Printf("%s  %s\n", T("站点", "Site   "), base)
	fmt.Printf("%s  %s\n", T("套餐", "Plan   "), w.Plan)
	if w.PlanExp != "" {
		fmt.Printf("%s  %s\n", T("到期", "Expires"), w.PlanExp)
	}
	quota := T("不限", "unlimited")
	if w.QuotaLimit > 0 {
		quota = fmt.Sprintf("%d / %d", w.QuotaUsed, w.QuotaLimit)
	}
	fmt.Printf("%s  %s\n", T("今日查询", "Today  "), quota)
	if w.PerMinute > 0 {
		fmt.Printf("%s  %d %s\n", T("频率", "Rate   "), w.PerMinute, T("次/分钟", "per minute"))
	}
	return nil
}

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// defaultServer is set at build time (-ldflags "-X main.defaultServer=https://bgp.cx").
var defaultServer = "https://bgp.cx"

type config struct {
	Server string `json:"server,omitempty"`
	APIKey string `json:"api_key,omitempty"`
	Email  string `json:"email,omitempty"`
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "bgptool", "config.json")
}

func loadConfig() config {
	var c config
	if b, err := os.ReadFile(configPath()); err == nil {
		json.Unmarshal(b, &c)
	}
	return c
}

func saveConfig(c config) error {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(p, b, 0o600)
}

// server returns the site the tool talks to: --server, $BGPTOOL_SERVER, the user config,
// /etc/bgptool.conf (written by the installer of the site it came from), the built-in default.
func server(flag string, c config) string {
	for _, s := range []string{flag, os.Getenv("BGPTOOL_SERVER"), c.Server, systemServer()} {
		if s = strings.TrimRight(strings.TrimSpace(s), "/"); s != "" {
			return s
		}
	}
	return defaultServer
}

func systemServer() string {
	f, err := os.Open("/etc/bgptool.conf")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "="); ok && strings.TrimSpace(k) == "server" {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

type client struct {
	base string
	key  string
	lang string
	http *http.Client
}

type apiError struct {
	Status int
	Msg    string
}

func (e *apiError) Error() string { return e.Msg }

func (c *client) call(method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "bgptool/"+version)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", c.lang)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", c.base, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		json.Unmarshal(b, &e)
		if e.Error == "" {
			e.Error = strings.TrimSpace(string(b))
		}
		if e.Error == "" {
			e.Error = resp.Status
		}
		return &apiError{resp.StatusCode, e.Error}
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

func newClient(base, key, lang string) *client {
	return &client{base: base, key: key, lang: lang, http: &http.Client{Timeout: 20 * time.Second}}
}

// ---- tool runs ----

type runInfo struct {
	Run           string `json:"run"`
	MaxIPs        int    `json:"max_ips"`
	Anonymous     bool   `json:"anonymous"`
	RemainingRuns int    `json:"remaining_runs"`
	Plan          string `json:"plan"`
	QuotaUsed     int64  `json:"quota_used"`
	QuotaLimit    int64  `json:"quota_limit"`
	Notice        string `json:"notice"`
}

// geo is the part of a lookup result the tool shows.
type geo struct {
	Country     string `json:"country"`
	CountryCode string `json:"country_code"`
	Region      string `json:"region"`
	City        string `json:"city"`
	District    string `json:"district"`
	ISP         string `json:"isp"`
	ASN         uint   `json:"asn"`
	ASNOrg      string `json:"asn_organization"`
	Usage       string `json:"usage_type"`
	Scope       string `json:"scope"`
}

func (g *geo) location() string {
	var parts []string
	for _, p := range []string{g.Country, g.Region, g.City, g.District} {
		if p != "" && (len(parts) == 0 || parts[len(parts)-1] != p) {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " ")
}

type lookupResp struct {
	Results    map[string]*geo `json:"results"`
	QuotaUsed  int64           `json:"quota_used"`
	QuotaLimit int64           `json:"quota_limit"`
	Notice     string          `json:"notice"`
}

func (c *client) startRun(tool, target string) (*runInfo, error) {
	var r runInfo
	err := c.call("POST", "/api/tools/run", map[string]string{"tool": tool, "target": target}, &r)
	return &r, err
}

func (c *client) lookup(run string, ips []string) (*lookupResp, error) {
	var r lookupResp
	err := c.call("POST", "/api/tools/lookup", map[string]any{"run": run, "ips": ips}, &r)
	return &r, err
}

// ---- login ----

type whoami struct {
	Email      string `json:"email"`
	Name       string `json:"name"`
	Plan       string `json:"plan"`
	PlanExp    string `json:"plan_expires_at"`
	PerMinute  int    `json:"per_minute"`
	QuotaUsed  int64  `json:"quota_used"`
	QuotaLimit int64  `json:"quota_limit"`
}

func (c *client) whoami() (*whoami, error) {
	var w whoami
	err := c.call("GET", "/api/cli/whoami", nil, &w)
	return &w, err
}

type deviceStart struct {
	DeviceCode string `json:"device_code"`
	UserCode   string `json:"user_code"`
	URI        string `json:"verification_uri"`
	URIFull    string `json:"verification_uri_complete"`
	Interval   int    `json:"interval"`
	ExpiresIn  int    `json:"expires_in"`
}

var errPending = errors.New("pending")

func (c *client) deviceStart(host string) (*deviceStart, error) {
	var d deviceStart
	err := c.call("POST", "/api/cli/device", map[string]string{"client": "bgptool " + version, "host": host}, &d)
	return &d, err
}

func (c *client) deviceToken(code string) (key, email string, err error) {
	var r struct {
		APIKey string `json:"api_key"`
		Email  string `json:"email"`
	}
	err = c.call("POST", "/api/cli/token", map[string]string{"device_code": code}, &r)
	var ae *apiError
	if errors.As(err, &ae) && ae.Msg == "authorization_pending" {
		return "", "", errPending
	}
	return r.APIKey, r.Email, err
}

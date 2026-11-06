package home

import (
	"bytes"
	"context"
	"crypto/tls"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/AdguardTeam/AdGuardHome/internal/aghhttp"
)

//go:embed acmebundle/acme.sh
var acmeScript []byte

//go:embed acmebundle/dns_cf.sh
var cloudflareScript []byte

var acmeLabel = regexp.MustCompile("^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$")
var acmeEmail = regexp.MustCompile("^[a-zA-Z0-9._+-]+@[a-zA-Z0-9.-]+[.][a-zA-Z]{2,}$")
var acmeToken = regexp.MustCompile("^[a-zA-Z0-9_-]{1,256}$")

type acmeSettings struct {
	Enabled       bool
	Target, Email string
	Method        string
}

// The token is persisted only in the private ACME working directory.
type acmeConfig struct {
	acmeSettings
	CFToken string `json:",omitempty"`
}
type acmeManager struct {
	mu      sync.Mutex
	dir     string
	uses80  bool
	cfg     acmeConfig
	busy    bool
	message string
	reload  func(context.Context)
}

func validTarget(s string) bool {
	if ip, e := netip.ParseAddr(s); e == nil {
		return ip.Zone() == "" && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.Is4In6()
	}
	if len(s) > 253 || !strings.Contains(s, ".") {
		return false
	}
	labels := strings.Split(s, ".")
	for _, l := range labels {
		if !acmeLabel.MatchString(l) {
			return false
		}
	}
	return strings.ContainsAny(labels[len(labels)-1], "abcdefghijklmnopqrstuvwxyz")
}

func (c *acmeConfig) validate() error {
	if !validTarget(c.Target) || len(c.Email) > 254 || !acmeEmail.MatchString(c.Email) {
		return errors.New("请检查域名、公网 IP 和联系邮箱")
	}
	switch c.Method {
	case "", "http":
		c.Method = "http"
		c.CFToken = ""
	case "cloudflare":
		if _, err := netip.ParseAddr(c.Target); err == nil {
			return errors.New("Cloudflare DNS 验证需要填写域名")
		}
		if !acmeToken.MatchString(c.CFToken) {
			return errors.New("请填写有效的 Cloudflare API Token")
		}
	default:
		return errors.New("未知的证书验证方式")
	}
	return nil
}

func atomicACME(path string, data []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	_, e = f.Write(data)
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func startACME(ctx context.Context, dir string, uses80 bool, reg aghhttp.Registrar, mux *http.ServeMux, reload func(context.Context)) {
	m := &acmeManager{dir: filepath.Join(dir, "managed-acme"), uses80: uses80, reload: reload}
	p := filepath.Join(m.dir, "settings.json")
	if b, e := os.ReadFile(p); e == nil {
		if json.Unmarshal(b, &m.cfg) != nil || (m.cfg.Enabled && m.cfg.validate() != nil) {
			m.cfg = acmeConfig{}
			m.message = "托管配置无效，请重新保存。"
		}
	}
	mux.HandleFunc("/.well-known/acme-challenge/", m.challenge)
	reg.Register("GET", "/control/acme/status", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		cert, key := m.paths(m.cfg.Target)
		var expires string
		if validTarget(m.cfg.Target) {
			if pair, e := tls.LoadX509KeyPair(cert, key); e == nil && pair.Leaf != nil {
				expires = pair.Leaf.NotAfter.UTC().Format(time.RFC3339)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"settings": m.cfg.acmeSettings, "hasCFToken": m.cfg.CFToken != "", "running": m.busy, "supported": runtime.GOOS == "linux", "message": m.message, "expires": expires, "cert": cert, "key": key})
	})
	reg.Register("POST", "/control/acme/config", func(w http.ResponseWriter, r *http.Request) {
		if runtime.GOOS != "linux" {
			http.Error(w, "仅 Linux 支持签发", 400)
			return
		}
		var c acmeConfig
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&c) != nil {
			http.Error(w, "无效配置", 400)
			return
		}
		c.Target = strings.ToLower(strings.TrimSpace(c.Target))
		c.Email = strings.TrimSpace(c.Email)
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.busy {
			http.Error(w, "证书任务正在运行", 409)
			return
		}
		if !c.Enabled {
			c = m.cfg
			c.Enabled = false
		} else {
			if c.Method == "cloudflare" && c.CFToken == "" && c.Target == m.cfg.Target {
				c.CFToken = m.cfg.CFToken
			}
			if err := c.validate(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		b, _ := json.Marshal(c)
		if os.MkdirAll(m.dir, 0700) != nil || atomicACME(p, b) != nil {
			http.Error(w, "无法保存托管配置", 500)
			return
		}
		m.cfg = c
		m.message = ""
		if c.Enabled {
			m.launch(ctx)
		}
		w.WriteHeader(204)
	})
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			m.mu.Lock()
			if runtime.GOOS == "linux" && m.cfg.Enabled && !m.busy {
				m.launch(ctx)
			}
			m.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

// launch requires mu. A job uses a snapshot; edits are rejected while it runs.
func (m *acmeManager) launch(ctx context.Context) {
	m.busy = true
	c := m.cfg
	go func() {
		job, cancel := context.WithTimeout(ctx, 12*time.Minute)
		defer cancel()
		err := m.run(job, c)
		m.mu.Lock()
		defer m.mu.Unlock()
		m.busy = false
		if err != nil {
			m.message = err.Error()
		} else {
			m.message = "检查成功，已启用自动续签。"
		}
	}()
}
func (m *acmeManager) paths(target string) (string, string) {
	dir := filepath.Join(m.dir, "live", target, "current")
	return filepath.Join(dir, "fullchain.pem"), filepath.Join(dir, "privkey.pem")
}

const acmePrefix = "/.well-known/acme-challenge/"

func isACMEChallenge(r *http.Request) bool {
	token, ok := strings.CutPrefix(r.URL.Path, acmePrefix)
	return ok && r.Method == http.MethodGet && acmeToken.MatchString(token)
}

func (m *acmeManager) challenge(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/.well-known/acme-challenge/")
	if !isACMEChallenge(r) {
		http.NotFound(w, r)
		return
	}
	b, e := os.ReadFile(filepath.Join(m.dir, "webroot", ".well-known", "acme-challenge", token))
	if e != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write(b)
}
func (m *acmeManager) run(ctx context.Context, c acmeConfig) error {
	for _, bin := range []string{"openssl", "sh"} {
		if _, e := exec.LookPath(bin); e != nil {
			return fmt.Errorf("缺少系统命令：%s", bin)
		}
	}
	if _, e := exec.LookPath("curl"); e != nil {
		if _, e = exec.LookPath("wget"); e != nil {
			return errors.New("需要 curl 或 wget")
		}
	}
	home := filepath.Join(m.dir, "client")
	// acme.sh --renew reuses the challenge from its saved domain configuration.
	// Keep separate client homes so switching methods uses the selected challenge.
	cf := c.Method == "cloudflare"
	if cf {
		home = filepath.Join(m.dir, "client-cloudflare")
	}
	web := filepath.Join(m.dir, "webroot")
	for _, d := range []string{home, web} {
		if e := os.MkdirAll(d, 0700); e != nil {
			return errors.New("无法创建证书工作目录")
		}
	}
	script := filepath.Join(home, "acme.sh")
	if e := atomicACME(script, acmeScript); e != nil {
		return errors.New("无法写入 acme.sh")
	}
	cert, key := m.paths(c.Target)
	if pair, e := tls.LoadX509KeyPair(cert, key); e == nil && pair.Leaf != nil {
		window := pair.Leaf.NotAfter.Sub(pair.Leaf.NotBefore) / 2
		if time.Until(pair.Leaf.NotAfter) > window {
			return nil
		}
	}
	if !cf && !m.uses80 {
		ln, e := net.Listen("tcp", ":80")
		if e != nil {
			return errors.New("无法监听 TCP 80：请检查端口占用和监听权限")
		}
		s := &http.Server{Handler: http.HandlerFunc(m.challenge), ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 10 * time.Second}
		go s.Serve(ln)
		defer s.Close()
	}
	args := []string{script, "--home", home, "--server", "letsencrypt", "--accountemail", c.Email, "--issue", "-d", c.Target, "--keylength", "ec-256"}
	if cf {
		plugins := filepath.Join(home, "dnsapi")
		if err := os.MkdirAll(plugins, 0700); err != nil {
			return err
		}
		if err := atomicACME(filepath.Join(plugins, "dns_cf.sh"), cloudflareScript); err != nil {
			return err
		}
		args = append(args, "--dns", "dns_cf")
	} else {
		args = append(args, "--webroot", web)
	}
	if _, e := netip.ParseAddr(c.Target); e == nil {
		args = append(args, "--certificate-profile", "shortlived", "--days", "-3")
	} else {
		args = append(args, "--days", "-20")
	}
	if _, err := os.Stat(filepath.Join(home, c.Target+"_ecc", c.Target+".conf")); err == nil {
		if _, err = os.Stat(filepath.Join(home, c.Target+"_ecc", "fullchain.cer")); err == nil {
			args = []string{script, "--home", home, "--server", "letsencrypt", "--accountemail", c.Email, "--renew", "-d", c.Target, "--ecc"}
		}
	}
	logFile, err := os.OpenFile(filepath.Join(m.dir, "last-run.log"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return errors.New("无法写入证书任务日志")
	}
	defer logFile.Close()
	cmd := exec.CommandContext(ctx, "/bin/sh", args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + home, "LC_ALL=C"}
	if cf {
		cmd.Env = append(cmd.Env, "CF_Token="+c.CFToken)
	}
	if e := cmd.Run(); e != nil {
		var x *exec.ExitError
		if !errors.As(e, &x) || x.ExitCode() != 2 {
			if cf {
				return errors.New("DNS 签发失败，请检查 Token 的区域读取与 DNS 编辑权限、域名托管和 DNS 传播情况")
			}
			return errors.New("acme.sh 签发失败，请检查公网 TCP 80、域名解析及到 Let’s Encrypt 的连接")
		}
	}
	src := filepath.Join(home, c.Target+"_ecc")
	chain, e := os.ReadFile(filepath.Join(src, "fullchain.cer"))
	if e != nil {
		return errors.New("未找到已签发证书")
	}
	priv, e := os.ReadFile(filepath.Join(src, c.Target+".key"))
	if e != nil {
		return errors.New("未找到已签发私钥")
	}
	return m.publish(ctx, c.Target, chain, priv)
}

func (m *acmeManager) publish(ctx context.Context, target string, chain, priv []byte) error {
	cert, key := m.paths(target)
	pair, e := tls.X509KeyPair(chain, priv)
	if e != nil || pair.Leaf == nil || pair.Leaf.VerifyHostname(target) != nil || !time.Now().Before(pair.Leaf.NotAfter) || time.Now().Before(pair.Leaf.NotBefore) {
		return errors.New("证书校验失败，已保留原证书")
	}
	if old, e := os.ReadFile(cert); e == nil && bytes.Equal(old, chain) {
		if k, e := os.ReadFile(key); e == nil && bytes.Equal(k, priv) {
			return nil
		}
	}
	base := filepath.Dir(filepath.Dir(cert))
	if e = os.MkdirAll(base, 0700); e != nil {
		return e
	}
	next, e := os.MkdirTemp(base, "pair-")
	if e != nil {
		return e
	}
	defer func() {
		if e != nil {
			_ = os.RemoveAll(next)
		}
	}()
	for name, b := range map[string][]byte{"fullchain.pem": chain, "privkey.pem": priv} {
		if e = os.WriteFile(filepath.Join(next, name), b, 0600); e != nil {
			return e
		}
	}
	tmp := next + ".link"
	if e = os.Symlink(filepath.Base(next), tmp); e != nil {
		return e
	}
	defer os.Remove(tmp)
	current := filepath.Dir(cert)
	previous, _ := os.Readlink(current)
	if e = os.Rename(tmp, current); e != nil {
		return e
	}
	m.reload(ctx)
	if filepath.Base(previous) == previous && strings.HasPrefix(previous, "pair-") {
		_ = os.RemoveAll(filepath.Join(base, previous))
	}
	return nil
}

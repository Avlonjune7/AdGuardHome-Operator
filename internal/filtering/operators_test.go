package filtering

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AdguardTeam/AdGuardHome/internal/aghtest"
	"github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

func testOperatorSnapshot(t *testing.T) *operatorSnapshot {
	t.Helper()
	s := &operatorSnapshot{Lists: map[Operator][]netip.Prefix{
		operatorMobile:  {netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("2001:db8:1::/48")},
		operatorUnicom:  {netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("2001:db8:2::/48")},
		operatorTelecom: {netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("2001:db8:3::/48")},
	}}
	require.NoError(t, s.prepare())
	return s
}

func TestOperatorLookup(t *testing.T) {
	s := testOperatorSnapshot(t)
	for _, tc := range []struct {
		ip   string
		want Operator
	}{
		{"192.0.2.1", operatorMobile}, {"198.51.100.1", operatorUnicom},
		{"203.0.113.1", operatorTelecom}, {"2001:db8:1::1", operatorMobile},
		{"::ffff:192.0.2.1", operatorMobile}, {"10.0.0.1", ""},
	} {
		require.Equal(t, tc.want, s.lookup(netip.MustParseAddr(tc.ip)), tc.ip)
	}
	s.Lists[operatorUnicom] = append(s.Lists[operatorUnicom], netip.MustParsePrefix("192.0.2.128/25"))
	require.NoError(t, s.prepare())
	require.Equal(t, Operator(""), s.lookup(netip.MustParseAddr("192.0.2.129")))
	require.Equal(t, operatorMobile, s.lookup(netip.MustParseAddr("192.0.2.1")))
}

func TestOperatorRewrites(t *testing.T) {
	d, _ := newForTest(t, nil, nil)
	t.Cleanup(d.Close)
	d.operators.snapshot.Store(testOperatorSnapshot(t))
	d.conf.Rewrites = []*LegacyRewrite{
		{Domain: "example.org", Answer: "1.1.1.1", Enabled: true},
		{Domain: "example.org", Answer: "2.2.2.2", Operator: operatorMobile, Enabled: true},
		{Domain: "example.org", Answer: "3.3.3.3", Operator: operatorUnicom, Enabled: true},
		{Domain: "example.org", Answer: "4.4.4.4", Operator: operatorTelecom, Enabled: true},
		{Domain: "example.org", Answer: "2001:db8::1", Enabled: true},
		{Domain: "alias.org", Answer: "example.org", Enabled: true},
		{Domain: "*.wild.org", Answer: "1.1.1.1", Enabled: true},
		{Domain: "*.wild.org", Answer: "2.2.2.2", Operator: operatorMobile, Enabled: true},
	}
	require.NoError(t, d.prepareRewrites(context.Background()))
	for _, tc := range []struct {
		ip, host, answer string
		qtype            uint16
	}{
		{"192.0.2.1", "example.org", "2.2.2.2", dns.TypeA},
		{"198.51.100.1", "example.org", "3.3.3.3", dns.TypeA},
		{"203.0.113.1", "example.org", "4.4.4.4", dns.TypeA},
		{"10.0.0.1", "example.org", "1.1.1.1", dns.TypeA},
		{"192.0.2.1", "alias.org", "2.2.2.2", dns.TypeA},
		{"192.0.2.1", "sub.wild.org", "2.2.2.2", dns.TypeA},
		{"192.0.2.1", "example.org", "2001:db8::1", dns.TypeAAAA},
	} {
		r := d.processRewrites(tc.host, tc.qtype, netip.MustParseAddr(tc.ip))
		require.Equal(t, []netip.Addr{netip.MustParseAddr(tc.answer)}, r.IPList, tc)
	}
	d.conf.Rewrites[1].Enabled = false
	require.Equal(t, []netip.Addr{netip.MustParseAddr("1.1.1.1")}, d.processRewrites("example.org", dns.TypeA, netip.MustParseAddr("192.0.2.1")).IPList)
	d.operators.snapshot.Store(nil)
	require.Equal(t, []netip.Addr{netip.MustParseAddr("1.1.1.1")}, d.processRewrites("example.org", dns.TypeA, netip.MustParseAddr("203.0.113.1")).IPList)
	require.Equal(t, operatorUnicom, cloneRewrites(d.conf.Rewrites)[2].Operator)
}

type operatorTransport func(*http.Request) (*http.Response, error)

func (f operatorTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOperatorRefreshKeepsSnapshot(t *testing.T) {
	broken := false
	client := &http.Client{Transport: operatorTransport(func(r *http.Request) (*http.Response, error) {
		text := "192.0.2.0/24\n2001:db8::/32\n"
		if broken && strings.Contains(r.URL.Path, "unicom") {
			text = "<html>error</html>"
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(text))}, nil
	})}
	db := newOperatorDB(t.TempDir(), client)
	require.NoError(t, db.refresh(context.Background()))
	previous := db.snapshot.Load()
	require.True(t, db.status().Ready)
	broken = true
	require.Error(t, db.refresh(context.Background()))
	require.Same(t, previous, db.snapshot.Load())
	loaded := newOperatorDB(strings.TrimSuffix(db.path, "operators.json"), client)
	require.Equal(t, previous.Lists, loaded.snapshot.Load().Lists)
	for _, input := range []string{"", "0.0.0.0/0", "not a prefix", "::/0"} {
		_, err := readOperatorList(&http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(input))})
		require.Error(t, err)
	}
}

func TestOperatorRewriteHTTPIsolation(t *testing.T) {
	d, _ := newForTest(t, nil, nil)
	t.Cleanup(d.Close)
	d.conf.ConfModifier = &aghtest.ConfigModifier{OnApply: func(context.Context) {}}
	call := func(handler http.HandlerFunc, body string) int {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		w := httptest.NewRecorder()
		handler(w, r)
		return w.Code
	}
	for _, op := range []string{"", "cmcc", "unicom"} {
		require.Equal(t, 200, call(d.handleRewriteAdd, `{"domain":"example.org","answer":"1.1.1.1","operator":"`+op+`"}`))
	}
	require.Equal(t, 400, call(d.handleRewriteAdd, `{"domain":"example.org","answer":"1.1.1.1","operator":"bad"}`))
	require.Equal(t, 200, call(d.handleRewriteUpdate, `{"target":{"domain":"example.org","answer":"1.1.1.1","operator":"cmcc"},"update":{"domain":"example.org","answer":"2.2.2.2","operator":"cmcc"}}`))
	require.Equal(t, "1.1.1.1", d.conf.Rewrites[len(d.conf.Rewrites)-1].Answer)
	require.Equal(t, 200, call(d.handleRewriteDelete, `{"domain":"example.org","answer":"1.1.1.1","operator":"unicom"}`))
	for _, r := range d.conf.Rewrites {
		require.NotEqual(t, operatorUnicom, r.Operator)
	}
}

func TestOperatorUpdateHistory(t *testing.T) {
	dir := t.TempDir()
	var fail atomic.Bool
	client := &http.Client{Transport: operatorTransport(func(r *http.Request) (*http.Response, error) {
		status := 200
		if fail.Load() {
			status = 503
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("192.0.2.0/24"))}, nil
	})}
	d, _ := newForTest(t, &Config{DataDir: dir, HTTPClient: client, Rewrites: []*LegacyRewrite{{Domain: "example.org", Answer: "192.0.2.10", Operator: operatorMobile, Enabled: true}}}, nil)
	d.startOperatorDB()
	defer d.Close()
	status := d.operators.status
	require.Eventually(t, func() bool { return len(status().History) == 1 }, 3*time.Second, time.Millisecond)
	require.False(t, status().History[0].Manual)
	require.Empty(t, status().History[0].Error)
	old := d.operators.snapshot.Load()
	w := httptest.NewRecorder()
	d.handleOperatorInterval(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"interval_hours":12}`)))
	require.Equal(t, 204, w.Code)
	fail.Store(true)
	d.handleOperatorRefresh(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil))
	require.Eventually(t, func() bool { return len(status().History) == 2 }, 3*time.Second, time.Millisecond)
	saved := newOperatorDB(dir, client).status()
	require.Equal(t, 12, saved.Hours)
	require.True(t, saved.History[0].Manual)
	require.Contains(t, saved.History[0].Error, "503")
	require.Same(t, old, d.operators.snapshot.Load())
}

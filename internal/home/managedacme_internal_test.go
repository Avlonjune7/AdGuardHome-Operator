package home

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"net"
	"net/http"

	"math/big"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestACMEChallenge(t *testing.T) {
	m := &acmeManager{dir: t.TempDir()}
	dir := filepath.Join(m.dir, "webroot", ".well-known", "acme-challenge")
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test_token"), []byte("proof"), 0600))
	for _, path := range []string{acmePrefix + "test_token", "/test_token", acmePrefix + "../settings.json", acmePrefix + "missing"} {
		w := httptest.NewRecorder()
		m.challenge(w, httptest.NewRequest("GET", path, nil))
		if path == acmePrefix+"test_token" {
			require.Equal(t, 200, w.Code)
			require.Equal(t, "proof", w.Body.String())
		} else {
			require.Equal(t, 404, w.Code)
		}
	}
}
func TestACMEPublish(t *testing.T) {
	count := 0
	m := &acmeManager{dir: t.TempDir(), reload: func(context.Context) { count++ }}
	const target = "example.org"
	a, b, priv := rotationPair(t, target)
	ctx := context.Background()
	require.NoError(t, m.publish(ctx, target, a, priv))
	require.NoError(t, m.publish(ctx, target, a, priv))
	require.Error(t, m.publish(ctx, target, b, []byte("bad")))
	require.Error(t, m.publish(ctx, "wrong.example", b, priv))
	path, _ := m.paths(target)
	active, e := os.ReadFile(path)
	require.NoError(t, e)
	require.Equal(t, a, active)
	require.Equal(t, 1, count)
	if runtime.GOOS == "windows" {
		t.Log("Replacing a directory symlink atomically requires the supported Linux runtime")
		return
	}
	require.NoError(t, m.publish(ctx, target, b, priv))
	active, e = os.ReadFile(path)
	require.NoError(t, e)
	require.Equal(t, b, active)
	require.Equal(t, 2, count)
}

func rotationPair(t *testing.T, target string) ([]byte, []byte, []byte) {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, e)
	raw, e := x509.MarshalECPrivateKey(key)
	require.NoError(t, e)
	priv := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: raw})
	makeCert := func(n int64) []byte {
		tmpl := &x509.Certificate{SerialNumber: big.NewInt(n), Subject: pkix.Name{CommonName: target}, DNSNames: []string{target}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
		der, e := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		require.NoError(t, e)
		return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	}
	return makeCert(1), makeCert(2), priv

}

func TestHTTPSCertificateRotationKeepsConnection(t *testing.T) {
	first, second, key := rotationPair(t, "example.org")
	a, err := tls.X509KeyPair(first, key)
	require.NoError(t, err)
	b, err := tls.X509KeyPair(second, key)
	require.NoError(t, err)
	state := &httpsServer{mu: &sync.Mutex{}}
	state.replaceCertificate(a)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := &http.Server{TLSConfig: &tls.Config{GetCertificate: state.getCertificate}, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "ok") })}
	go srv.ServeTLS(listener, "", "")
	t.Cleanup(func() { _ = srv.Close() })
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(first)
	roots.AppendCertsFromPEM(second)
	newClient := func() *http.Client {
		tr := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "example.org"}}
		t.Cleanup(tr.CloseIdleConnections)
		return &http.Client{Transport: tr, Timeout: 3 * time.Second}
	}
	getSerial := func(client *http.Client) int64 {
		resp, err := client.Get("https://" + listener.Addr().String())
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, "ok", string(body))
		return resp.TLS.PeerCertificates[0].SerialNumber.Int64()
	}
	client := newClient()
	require.EqualValues(t, 1, getSerial(client))
	state.replaceCertificate(b)
	require.EqualValues(t, 1, getSerial(client))
	require.EqualValues(t, 2, getSerial(newClient()))
}

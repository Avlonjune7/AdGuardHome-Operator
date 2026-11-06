package filtering

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/renameio/v2/maybe"
)

// Operator identifies a rewrite's client network.  Empty means the fallback.
type Operator string

const (
	operatorMobile  Operator = "cmcc"
	operatorUnicom  Operator = "unicom"
	operatorTelecom Operator = "chinanet"
)

func (o Operator) valid() bool {
	return o == "" || o == operatorMobile || o == operatorUnicom || o == operatorTelecom
}

var operatorNames = []Operator{operatorMobile, operatorUnicom, operatorTelecom}

// operatorSnapshot is persisted as one file so partial downloads never replace
// a working database.  Prefixes shared by operators are deliberately ambiguous.
type operatorSnapshot struct {
	Updated time.Time                   `json:"updated"`
	Lists   map[Operator][]netip.Prefix `json:"lists"`
	index   map[netip.Prefix]uint8
}

func (s *operatorSnapshot) prepare() error {
	s.index = make(map[netip.Prefix]uint8)
	for i, name := range operatorNames {
		if len(s.Lists[name]) == 0 {
			return fmt.Errorf("empty operator list: %s", name)
		}
		for _, p := range s.Lists[name] {
			if !p.IsValid() || p.Bits() == 0 || p.Addr().Is4In6() {
				return fmt.Errorf("invalid operator prefix")
			}
			s.index[p.Masked()] |= 1 << i
		}
	}
	return nil
}

func (s *operatorSnapshot) lookup(ip netip.Addr) Operator {
	if s == nil || !ip.IsValid() {
		return ""
	}
	ip = ip.Unmap()
	var matches uint8
	for bits := ip.BitLen(); bits > 0; bits-- {
		matches |= s.index[netip.PrefixFrom(ip, bits).Masked()]
	}
	for i, name := range operatorNames {
		if matches == 1<<i {
			return name
		}
	}
	return ""
}

type operatorDB struct {
	snapshot  atomic.Pointer[operatorSnapshot]
	mu        sync.Mutex
	lastError string
	state     operatorUpdateState
	running   bool
	manual    bool
	path      string
	client    *http.Client
	cancel    context.CancelFunc
	done      chan struct{}
	wake      chan struct{}
}

const operatorHistoryLimit = 30

type operatorUpdateEntry struct {
	At     time.Time `json:"at"`
	Manual bool      `json:"manual"`
	Error  string    `json:"error,omitempty"`
}
type operatorUpdateState struct {
	Hours   int                   `json:"interval_hours,omitempty"`
	History []operatorUpdateEntry `json:"history,omitempty"`
}

type operatorDBStatus struct {
	operatorUpdateState
	Running bool      `json:"running,omitempty"`
	Ready   bool      `json:"ready"`
	Updated time.Time `json:"updated"`
	Error   string    `json:"error,omitempty"`
}

func newOperatorDB(dir string, client *http.Client) *operatorDB {
	db := &operatorDB{path: filepath.Join(dir, "operators.json"), client: client, wake: make(chan struct{}, 1)}
	if db.client == nil {
		db.client = &http.Client{Timeout: 30 * time.Second}
	}
	db.state.Hours = 24
	if b, e := os.ReadFile(db.statePath()); e == nil {
		var saved operatorUpdateState
		if json.Unmarshal(b, &saved) == nil && saved.Hours >= 1 && saved.Hours <= 720 {
			db.state = saved
			if len(saved.History) > 0 {
				db.lastError = saved.History[0].Error
			}
		}
	}
	data, err := os.ReadFile(db.path)
	if err == nil {
		s := &operatorSnapshot{}
		if err = json.Unmarshal(data, s); err == nil {
			err = s.prepare()
		}
		if err == nil {
			db.snapshot.Store(s)
		}
	}
	if err != nil && !os.IsNotExist(err) {
		db.lastError = err.Error()
	}
	return db
}

func (db *operatorDB) status() operatorDBStatus {
	if db == nil {
		return operatorDBStatus{}
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	status := operatorDBStatus{Error: db.lastError, operatorUpdateState: db.state, Running: db.running || db.manual}
	if s := db.snapshot.Load(); s != nil {
		status.Ready = true
		status.Updated = s.Updated
	}
	return status
}

func (db *operatorDB) refresh(ctx context.Context) error {
	s := &operatorSnapshot{Updated: time.Now().UTC(), Lists: make(map[Operator][]netip.Prefix)}
	for _, name := range operatorNames {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			"https://gaoyifan.github.io/china-operator-ip/"+string(name)+"46.txt", nil)
		if err != nil {
			return err
		}
		resp, err := db.client.Do(req)
		if err != nil {
			return err
		}
		prefixes, err := readOperatorList(resp)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		s.Lists[name] = prefixes
	}
	if err := s.prepare(); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(db.path), 0o700); err != nil {
		return err
	}
	if err = maybe.WriteFile(db.path, data, 0o600); err != nil {
		return err
	}
	db.snapshot.Store(s)
	return nil
}

func readOperatorList(resp *http.Response) ([]netip.Prefix, error) {
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}
	const maxSize = 8 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSize {
		return nil, fmt.Errorf("operator list too large")
	}
	var prefixes []netip.Prefix
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p, err := netip.ParsePrefix(line)
		if err != nil || p.Bits() == 0 || p.Addr().Is4In6() {
			return nil, fmt.Errorf("invalid operator prefix")
		}
		prefixes = append(prefixes, p.Masked())
	}
	if err = scanner.Err(); err != nil {
		return nil, err
	}
	if len(prefixes) == 0 {
		return nil, fmt.Errorf("empty operator list")
	}
	return prefixes, nil
}

// ClientOperator looks up the client IP in the currently loaded operator database.
func (d *DNSFilter) ClientOperator(ip netip.Addr) Operator {
	if d.operators == nil {
		return ""
	}
	return d.operators.snapshot.Load().lookup(ip)
}

func (d *DNSFilter) startOperatorDB() {
	db := d.operators
	ctx, cancel := context.WithCancel(context.Background())
	db.cancel = cancel
	db.done = make(chan struct{})
	go func() {
		defer close(db.done)
		timer := time.NewTimer(time.Hour)
		defer timer.Stop()
		for ctx.Err() == nil {
			db.mu.Lock()
			manual := db.manual
			db.manual = false
			wait := time.Duration(0)
			if snap := db.snapshot.Load(); snap != nil {
				wait = time.Until(snap.Updated.Add(time.Duration(db.state.Hours) * time.Hour))

			}
			run := manual || wait <= 0
			db.running = run
			db.mu.Unlock()
			if run {
				fetchCtx, stop := context.WithTimeout(ctx, 90*time.Second)
				err := db.refresh(fetchCtx)
				stop()
				if ctx.Err() != nil {
					return
				}
				entry := operatorUpdateEntry{At: time.Now().UTC(), Manual: manual}
				if err != nil {
					entry.Error = err.Error()
					d.logger.WarnContext(ctx, "operator database update failed", "error", err)
				} else {
					d.logger.InfoContext(ctx, "operator database updated", "manual", manual)
				}
				db.mu.Lock()
				db.lastError = entry.Error
				history := append([]operatorUpdateEntry{entry}, db.state.History...)
				db.state.History = history[:min(len(history), operatorHistoryLimit)]
				if err = db.saveState(db.state); err != nil {
					d.logger.ErrorContext(ctx, "saving operator database update history", "error", err)
					db.lastError = err.Error()
				}
				db.running = false
				db.mu.Unlock()
				wait = time.Hour
			}
			timer.Reset(wait)
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			case <-db.wake:
			}
		}

	}()
}

func (d *DNSFilter) wakeOperatorDB() {
	if d.operators != nil {
		select {
		case d.operators.wake <- struct{}{}:
		default:
		}
	}
}

// findOperatorRewrites preserves existing domain/CNAME precedence.  A matching
// operator replaces only fallback answers for the same pattern and query type.
func findOperatorRewrites(entries []*LegacyRewrite, host string, qtype uint16, operator Operator) ([]*LegacyRewrite, bool) {
	preferred := make(map[string]bool)
	if operator != "" {
		for _, r := range entries {
			if r.Enabled && r.Operator == operator && r.matchesQType(qtype) {
				preferred[r.Domain] = true
			}
		}
	}
	selected := make([]*LegacyRewrite, 0, len(entries))
	for _, r := range entries {
		if r.Operator == "" && !preferred[r.Domain] || operator != "" && r.Operator == operator {
			selected = append(selected, r)
		}
	}
	return findRewrites(selected, host, qtype)
}

func (db *operatorDB) statePath() string {
	return filepath.Join(filepath.Dir(db.path), "operator-updates.json")
}

// saveState persists settings and the bounded history together. Caller holds mu.
func (db *operatorDB) saveState(state operatorUpdateState) error {
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(db.path), 0700); err != nil {
		return err
	}
	return maybe.WriteFile(db.statePath(), b, 0600)
}

func (d *DNSFilter) handleOperatorInterval(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Hours int `json:"interval_hours"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.Hours < 1 || req.Hours > 720 {
		http.Error(w, "更新周期必须是 1 到 720 之间的整数小时", 400)
		return
	}
	db := d.operators
	db.mu.Lock()
	next := db.state
	next.Hours = req.Hours
	err := db.saveState(next)
	if err == nil {
		db.state = next
	}
	db.mu.Unlock()
	if err != nil {
		http.Error(w, "保存更新周期失败", 500)
		return
	}
	d.wakeOperatorDB()
	w.WriteHeader(http.StatusNoContent)
}

func (d *DNSFilter) handleOperatorRefresh(w http.ResponseWriter, r *http.Request) {
	db := d.operators
	db.mu.Lock()
	if !db.running {
		db.manual = true
	}
	db.mu.Unlock()
	d.wakeOperatorDB()
	w.WriteHeader(http.StatusAccepted)
}

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Server, User, string) {
	t.Helper()
	st, e := openStore(filepath.Join(t.TempDir(), "yarus.journal"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { st.file.Close() })
	u := User{ID: "owner-0001", Name: "Владелец", Login: "owner", Role: "owner"}
	token, sess := sessionFor(u)
	if e = st.commit(Patch{User: &u, Session: &sess, Space: &Workspace{Name: "Test", Currency: "RUB", ID: "workspace-test"}, Place: &Place{ID: "main-place", Name: "Основной", Version: 1}}); e != nil {
		t.Fatal(e)
	}
	a := &Server{st: st, setup: "bootstrap-secret", listen: "127.0.0.1:8787", attempts: map[string][]time.Time{}}
	return a, u, token
}
func request(t *testing.T, a *Server, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.RemoteAddr = "127.0.0.1:4321"
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	var data map[string]any
	if e := json.Unmarshal(w.Body.Bytes(), &data); e != nil {
		t.Fatalf("invalid JSON: %s", w.Body.String())
	}
	return w.Code, data
}
func command(t *testing.T, a *Server, token string, c Command, want int) {
	t.Helper()
	status, res := request(t, a, "POST", "/api/command", token, c)
	if status != want {
		t.Fatalf("%s: got %d wanted %d: %v", c.Type, status, want, res)
	}
}
func seed(t *testing.T, a *Server, token string, n int64) {
	command(t, a, token, Command{ID: identifier(), Type: "item", Item: &Item{ID: "item-00001", Name: "Тестовый товар", SKU: "SKU1", Unit: "кг", Fields: map[string]string{}}}, 200)
	command(t, a, token, Command{ID: identifier(), Type: "place", Place: &Place{ID: "second-place", Name: "Второй", Version: 0}}, 200)
	if n > 0 {
		command(t, a, token, Command{ID: identifier(), Type: "in", ItemID: "item-00001", To: "main-place", Qty: n}, 200)
	}
}
func TestExactFractionalStock(t *testing.T) {
	a, _, token := fixture(t)
	seed(t, a, token, 100)
	command(t, a, token, Command{ID: identifier(), Type: "in", ItemID: "item-00001", To: "main-place", Qty: 200}, 200)
	command(t, a, token, Command{ID: identifier(), Type: "out", ItemID: "item-00001", From: "main-place", Qty: 300}, 200)
	if a.st.S.Stocks[key("item-00001", "main-place")].Qty != 0 {
		t.Fatal("fractional residue")
	}
}
func TestInsufficientStockIsAtomic(t *testing.T) {
	a, _, token := fixture(t)
	seed(t, a, token, 5000)
	before := a.st.S.Seq
	command(t, a, token, Command{ID: identifier(), Type: "out", ItemID: "item-00001", From: "main-place", Qty: 6000}, 409)
	if a.st.S.Seq != before || a.st.S.Stocks[key("item-00001", "main-place")].Qty != 5000 {
		t.Fatal("rejected operation mutated state")
	}
}
func TestConcurrentWithdrawals(t *testing.T) {
	a, _, token := fixture(t)
	seed(t, a, token, 5000)
	ts := httptest.NewServer(a)
	defer ts.Close()
	var wg sync.WaitGroup
	status := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := Command{ID: identifier(), Type: "out", ItemID: "item-00001", From: "main-place", Qty: 4000}
			b, _ := json.Marshal(c)
			r, _ := http.NewRequest("POST", ts.URL+"/api/command", bytes.NewReader(b))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", "Bearer "+token)
			res, e := http.DefaultClient.Do(r)
			if e != nil {
				status <- 0
				return
			}
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			status <- res.StatusCode
		}()
	}
	wg.Wait()
	close(status)
	counts := map[int]int{}
	for x := range status {
		counts[x]++
	}
	if counts[200] != 1 || counts[409] != 1 || a.st.S.Stocks[key("item-00001", "main-place")].Qty != 1000 {
		t.Fatalf("invalid concurrent result: %v", counts)
	}
}
func TestIdempotencyAndIntentMismatch(t *testing.T) {
	a, _, token := fixture(t)
	seed(t, a, token, 5000)
	c := Command{ID: identifier(), Type: "out", ItemID: "item-00001", From: "main-place", Qty: 2000}
	command(t, a, token, c, 200)
	seq := a.st.S.Seq
	command(t, a, token, c, 200)
	if a.st.S.Seq != seq {
		t.Fatal("duplicate appended")
	}
	c.Qty = 1000
	command(t, a, token, c, 409)
	if a.st.S.Stocks[key("item-00001", "main-place")].Qty != 3000 {
		t.Fatal("double spent")
	}
}
func TestTransferPreservesTotalAndRejectsPartial(t *testing.T) {
	a, _, token := fixture(t)
	seed(t, a, token, 5000)
	command(t, a, token, Command{ID: identifier(), Type: "transfer", ItemID: "item-00001", From: "main-place", To: "second-place", Qty: 3500}, 200)
	command(t, a, token, Command{ID: identifier(), Type: "transfer", ItemID: "item-00001", From: "main-place", To: "second-place", Qty: 2000}, 409)
	if a.st.S.Stocks[key("item-00001", "main-place")].Qty != 1500 || a.st.S.Stocks[key("item-00001", "second-place")].Qty != 3500 {
		t.Fatal("partial transfer")
	}
}
func TestCountRejectsStaleVersion(t *testing.T) {
	a, _, token := fixture(t)
	seed(t, a, token, 5000)
	v := a.st.S.Stocks[key("item-00001", "main-place")].Version
	command(t, a, token, Command{ID: identifier(), Type: "out", ItemID: "item-00001", From: "main-place", Qty: 1000}, 200)
	command(t, a, token, Command{ID: identifier(), Type: "count", ItemID: "item-00001", To: "main-place", Qty: 4500, Expected: v, Note: "Пересчёт"}, 409)
	command(t, a, token, Command{ID: identifier(), Type: "count", ItemID: "item-00001", To: "main-place", Qty: 4500, Expected: v + 1, Note: "Пересчёт"}, 200)
}
func TestReverseIsAuditedAndSingleUse(t *testing.T) {
	a, _, token := fixture(t)
	seed(t, a, token, 5000)
	op := Command{ID: identifier(), Type: "out", ItemID: "item-00001", From: "main-place", Qty: 1000}
	command(t, a, token, op, 200)
	n := len(a.st.S.Events)
	command(t, a, token, Command{ID: identifier(), Type: "reverse", Ref: op.ID, Note: "Ошибка"}, 200)
	if len(a.st.S.Events) != n+1 || a.st.S.Stocks[key("item-00001", "main-place")].Qty != 5000 {
		t.Fatal("history changed")
	}
	command(t, a, token, Command{ID: identifier(), Type: "reverse", Ref: op.ID, Note: "Ещё раз"}, 409)
}
func TestReverseIncomingCannotCreateNegative(t *testing.T) {
	a, _, token := fixture(t)
	seed(t, a, token, 5000)
	incoming := a.st.S.Events[0].ID
	command(t, a, token, Command{ID: identifier(), Type: "out", ItemID: "item-00001", From: "main-place", Qty: 1000}, 200)
	command(t, a, token, Command{ID: identifier(), Type: "reverse", Ref: incoming, Note: "Ошибка"}, 409)
}
func TestViewerAndRevocation(t *testing.T) {
	a, _, ownerToken := fixture(t)
	seed(t, a, ownerToken, 5000)
	v := User{ID: "viewer-0001", Login: "reader", Name: "Читатель", Role: "viewer"}
	token, s := sessionFor(v)
	a.st.commit(Patch{User: &v, Session: &s})
	code, _ := request(t, a, "GET", "/api/state", token, nil)
	if code != 200 {
		t.Fatal(code)
	}
	command(t, a, token, Command{ID: identifier(), Type: "out", ItemID: "item-00001", From: "main-place", Qty: 1000}, 403)
	command(t, a, ownerToken, Command{ID: identifier(), Type: "disable", UserID: v.ID}, 200)
	code, _ = request(t, a, "GET", "/api/state", token, nil)
	if code != 401 {
		t.Fatal("disabled session accepted")
	}
}
func TestCatalogRules(t *testing.T) {
	a, _, token := fixture(t)
	seed(t, a, token, 5000)
	x := a.st.S.Items["item-00001"]
	x.Archived = true
	command(t, a, token, Command{ID: identifier(), Type: "item", Item: &x}, 409)
	x.Archived = false
	x.Unit = "л"
	command(t, a, token, Command{ID: identifier(), Type: "item", Item: &x}, 409)
	x.Unit = "кг"
	x.Version = 0
	command(t, a, token, Command{ID: identifier(), Type: "item", Item: &x}, 409)
	x.ID = "item-00002"
	x.Version = 0
	command(t, a, token, Command{ID: identifier(), Type: "item", Item: &x}, 409)
}
func TestRequestValidation(t *testing.T) {
	a, _, token := fixture(t)
	seed(t, a, token, 5000)
	for _, n := range []int64{-1, 0, maxQty + 1} {
		command(t, a, token, Command{ID: identifier(), Type: "in", ItemID: "item-00001", To: "main-place", Qty: n}, 400)
	}
	command(t, a, token, Command{ID: identifier(), Type: "transfer", ItemID: "item-00001", From: "main-place", To: "main-place", Qty: 1}, 400)
	command(t, a, token, Command{ID: "__proto__", Type: "out", ItemID: "item-00001", From: "main-place", Qty: 1}, 400)
	code, _ := request(t, a, "POST", "/api/command", token, map[string]any{"id": identifier(), "type": "in", "qty": 0.1})
	if code != 400 {
		t.Fatal("fraction was accepted")
	}
}
func TestAuthenticationAndExportsNoSecrets(t *testing.T) {
	a, _, token := fixture(t)
	for _, path := range []string{"/api/state", "/api/team", "/api/export"} {
		code, _ := request(t, a, "GET", path, "", nil)
		if code != 401 {
			t.Fatal(path)
		}
	}
	_, data := request(t, a, "GET", "/api/export", token, nil)
	for _, k := range []string{"users", "sessions", "invites", "hash", "token"} {
		if _, ok := data[k]; ok {
			t.Fatal("secret exposed", k)
		}
	}
}
func TestInviteSingleUseExpiredAndRoles(t *testing.T) {
	a, _, token := fixture(t)
	code, res := request(t, a, "POST", "/api/invite", token, map[string]string{"role": "viewer"})
	if code != 200 {
		t.Fatal(res)
	}
	body := map[string]string{"login": "newuser", "password": "strong-pass123", "name": "Участник", "code": res["code"].(string)}
	code, res = request(t, a, "POST", "/api/join", "", body)
	if code != 200 {
		t.Fatal(res)
	}
	st := res["state"].(map[string]any)
	if st["me"].(map[string]any)["role"] != "viewer" {
		t.Fatal("role escalated")
	}
	body["login"] = "another"
	code, _ = request(t, a, "POST", "/api/join", "", body)
	if code != 403 {
		t.Fatal("invite reused")
	}
	iv := Invite{Hash: hash("expired-code"), Role: "editor", Expires: time.Now().Add(-time.Hour).Unix()}
	a.st.commit(Patch{Invite: &iv})
	body["code"] = "expired-code"
	code, _ = request(t, a, "POST", "/api/join", "", body)
	if code != 403 {
		t.Fatal("expired invite accepted")
	}
	code, _ = request(t, a, "POST", "/api/invite", token, map[string]string{"role": "owner"})
	if code != 400 {
		t.Fatal("owner invite")
	}
}
func TestSetupCannotBeClaimedWithoutBootstrapKey(t *testing.T) {
	st, e := openStore(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer st.file.Close()
	a := &Server{st: st, setup: "right-key", attempts: map[string][]time.Time{}}
	body := map[string]string{"login": "admin", "name": "Admin", "password": "test-password123", "space": "Склад", "setup": "wrong"}
	code, _ := request(t, a, "POST", "/api/setup", "", body)
	if code != 403 {
		t.Fatal(code)
	}
	body["setup"] = "right-key"
	code, res := request(t, a, "POST", "/api/setup", "", body)
	if code != 200 {
		t.Fatal(res)
	}
	body["login"] = "admin2"
	code, _ = request(t, a, "POST", "/api/setup", "", body)
	if code != 409 {
		t.Fatal(code)
	}
	code, _ = request(t, a, "POST", "/api/login", "", map[string]string{"login": "admin", "password": "test-password123"})
	if code != 200 {
		t.Fatal("login failed")
	}
}
func TestJournalRecoveryAndCorruption(t *testing.T) {
	a, _, token := fixture(t)
	seed(t, a, token, 5000)
	path := a.st.Path
	seq := a.st.S.Seq
	a.st.file.Close()
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString(`{"data": unfinished`)
	f.Close()
	st, e := openStore(path)
	if e != nil {
		t.Fatal(e)
	}
	if st.S.Seq != seq || st.S.Stocks[key("item-00001", "main-place")].Qty != 5000 {
		t.Fatal("recovery lost durable data")
	}
	st.file.Close()
	b, _ := os.ReadFile(path)
	b[20] ^= 1
	os.WriteFile(path, b, 0600)
	if st2, e := openStore(path); e == nil {
		st2.file.Close()
		t.Fatal("corruption ignored")
	}
}
func TestBackupIsReplayableAndPreservesIdempotency(t *testing.T) {
	a, _, token := fixture(t)
	seed(t, a, token, 5000)
	c := Command{ID: identifier(), Type: "out", ItemID: "item-00001", From: "main-place", Qty: 1000}
	command(t, a, token, c, 200)
	path, e := a.st.backup()
	if e != nil {
		t.Fatal(e)
	}
	st, e := openStore(path)
	if e != nil {
		t.Fatal(e)
	}
	defer st.file.Close()
	a.st = st
	seq := st.S.Seq
	command(t, a, token, c, 200)
	if st.S.Seq != seq {
		t.Fatal("retry after restart repeated operation")
	}
}
func TestDiskFailureDoesNotAcknowledge(t *testing.T) {
	a, _, token := fixture(t)
	seed(t, a, token, 5000)
	seq := a.st.S.Seq
	a.st.file.Close()
	command(t, a, token, Command{ID: identifier(), Type: "out", ItemID: "item-00001", From: "main-place", Qty: 1000}, 503)
	if a.st.S.Seq != seq || !a.st.failed {
		t.Fatal("failed write acknowledged")
	}
}
func TestUnknownOriginNotGrantedCORS(t *testing.T) {
	a, _, _ := fixture(t)
	r := httptest.NewRequest("OPTIONS", "/api/state", nil)
	r.Header.Set("Origin", "https://untrusted.example")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("reflected untrusted origin")
	}
	r.Header.Set("Origin", "https://appassets.androidplatform.net")
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if !strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Fatal("native CORS failed")
	}
}
func TestDirectoryLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	unlock, e := acquireDirLock(path)
	if e != nil {
		t.Fatal(e)
	}
	if u2, e2 := acquireDirLock(path); e2 == nil {
		u2()
		t.Fatal("two owners")
	}
	unlock()
	u3, e := acquireDirLock(path)
	if e != nil {
		t.Fatal("lock not released")
	}
	u3()
}

func TestLegacyNamespaceMigrationIsDurableAndBackedUp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yarus.journal")
	st, e := openStore(path)
	if e != nil {
		t.Fatal(e)
	}
	u := User{ID: "legacy-owner", Name: "Владелец", Login: "owner", Role: "owner"}
	if e = st.commit(Patch{User: &u, Space: &Workspace{Name: "Хемиком", Currency: "RUB"}, Place: &Place{ID: "main-place", Name: "Основной", Version: 1}}); e != nil {
		t.Fatal(e)
	}
	st.file.Close()
	original, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	st, e = openStore(path)
	if e != nil {
		t.Fatal(e)
	}
	id := st.S.Space.ID
	seq := st.S.Seq
	if id == "" || st.S.Space.Name != "Хемиком" || seq != 2 || len(st.S.Events) != 0 {
		t.Fatal("invalid namespace migration")
	}
	copies, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "backups", "*.journal"))
	if len(copies) != 1 {
		t.Fatal("missing pre-migration backup")
	}
	data, _ := os.ReadFile(copies[0])
	if !bytes.Equal(data, original) {
		t.Fatal("backup not exact")
	}
	st.file.Close()
	st, e = openStore(path)
	if e != nil {
		t.Fatal(e)
	}
	defer st.file.Close()
	if st.S.Space.ID != id || st.S.Seq != seq {
		t.Fatal("namespace changed on reopen")
	}
}
func TestRenamePreservesNamespaceAndStock(t *testing.T) {
	a, _, token := fixture(t)
	seed(t, a, token, 17000)
	namespace := a.st.S.Space.ID
	command(t, a, token, Command{ID: identifier(), Type: "space", Space: &Workspace{ID: "forged-space", Name: "Новое имя", Currency: "EUR"}}, 200)
	if a.st.S.Space.ID != namespace || a.st.S.Stocks[key("item-00001", "main-place")].Qty != 17000 {
		t.Fatal("rename changed namespace or inventory")
	}
}
func TestFactoryCodeCannotBeAssignedTwice(t *testing.T) {
	a, _, token := fixture(t)
	seed(t, a, token, 17000)
	first := a.st.S.Items["item-00001"]
	first.Barcode = "0012345678905"
	command(t, a, token, Command{ID: identifier(), Type: "item", Item: &first}, 200)
	events := len(a.st.S.Events)
	command(t, a, token, Command{ID: identifier(), Type: "item", Item: &Item{ID: "second-item", Name: "Другой товар", Unit: "шт", Barcode: first.Barcode, Fields: map[string]string{}}}, 409)
	if len(a.st.S.Events) != events || len(a.st.S.Items) != 1 {
		t.Fatal("duplicate barcode altered stock/catalog")
	}
}

func TestBackupRotationKeepsNewestThirty(t *testing.T) {
	dir := t.TempDir()
	st, err := openStore(filepath.Join(dir, "yarus.journal"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.file.Close()
	for i := 0; i < backupRetention+3; i++ {
		if _, err = st.backup(); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "yarus-") && strings.HasSuffix(entry.Name(), ".journal") {
			count++
		}
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("temporary backup remained: %s", entry.Name())
		}
	}
	if count != backupRetention {
		t.Fatalf("kept %d backups, want %d", count, backupRetention)
	}
}

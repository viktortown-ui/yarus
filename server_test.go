package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
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
	host := activeHost("Тестовый компьютер", 1)
	transportKey := random(32)
	if e = st.commit(Patch{User: &u, Session: &sess, Space: &Workspace{Name: "Test", Currency: "RUB", ID: "workspace-test"}, Place: &Place{ID: "main-place", Name: "Основной", Version: 1}, Host: &host, TransportKey: &transportKey}); e != nil {
		t.Fatal(e)
	}
	a := &Server{st: st, setup: "bootstrap-secret", listen: "127.0.0.1:8787", attempts: map[string][]time.Time{}}
	return a, u, token
}

func TestSecureReplayCacheStaysBounded(t *testing.T) {
	a := &Server{}
	for index := 0; index < 20025; index++ {
		if !a.acceptSecureID(fmt.Sprintf("secure-request-%05d", index)) {
			t.Fatalf("fresh secure request %d was rejected", index)
		}
	}
	if len(a.secureIDs) != 20000 {
		t.Fatalf("secure replay cache grew to %d entries", len(a.secureIDs))
	}
	if a.acceptSecureID("secure-request-20024") {
		t.Fatal("recent replay was accepted")
	}
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

func TestHistorySegmentsKeepAllEvents(t *testing.T) {
	s := blankState()
	for index := 0; index < historySegmentSize+7; index++ {
		appendHistory(&s, Event{ID: fmt.Sprintf("event-%08d", index)})
	}
	if len(s.HistorySegments) != 1 || len(s.HistorySegments[0]) != historySegmentSize || len(s.Events) != 7 || s.EventCount != historySegmentSize+7 {
		t.Fatalf("history was not segmented safely: segments=%d current=%d count=%d", len(s.HistorySegments), len(s.Events), s.EventCount)
	}
	recent := recentHistory(&s, 10)
	if len(recent) != 10 || recent[0].ID != fmt.Sprintf("event-%08d", historySegmentSize-3) || recent[9].ID != fmt.Sprintf("event-%08d", historySegmentSize+6) {
		t.Fatal("recent history crossed a segment incorrectly")
	}
}

func TestRecentCommandCacheIsBounded(t *testing.T) {
	state := blankState()
	for index := 1; index <= recentCommandLimit+7; index++ {
		apply(&state, Patch{Seq: int64(index), Key: fmt.Sprintf("owner:command-%08d", index), Intent: fmt.Sprintf("intent-%d", index)})
	}
	if len(state.Seen) != recentCommandLimit {
		t.Fatalf("recent command cache grew to %d", len(state.Seen))
	}
	if _, exists := state.Seen["owner:command-00000001"]; exists {
		t.Fatal("old idempotency entry was not pruned")
	}
	if _, exists := state.Seen[fmt.Sprintf("owner:command-%08d", recentCommandLimit+7)]; !exists {
		t.Fatal("new idempotency entry was pruned")
	}
}

func TestFullStateImportPreservesSequenceAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yarus.journal")
	store, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	imported := blankState()
	imported.Seq = 321
	imported.Space = Workspace{ID: "workspace-sequence", Name: "Перенесённый склад", Currency: "RUB"}
	if err = store.commit(Patch{Full: &imported}); err != nil {
		t.Fatal(err)
	}
	if store.S.Seq != 321 {
		t.Fatalf("import sequence reset to %d", store.S.Seq)
	}
	if err = store.file.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.file.Close()
	if store.S.Seq != 321 {
		t.Fatalf("reopened import sequence reset to %d", store.S.Seq)
	}
	space := store.S.Space
	space.Name = "Склад после переноса"
	if err = store.commit(Patch{Space: &space}); err != nil {
		t.Fatal(err)
	}
	if store.S.Seq != 322 {
		t.Fatalf("next change received sequence %d, want 322", store.S.Seq)
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

func TestGranularRolesAndOwnerProtection(t *testing.T) {
	a, owner, ownerToken := fixture(t)
	seed(t, a, ownerToken, 5000)
	manager := User{ID: "manager-0001", Name: "Менеджер", Login: "manager", Role: "manager"}
	managerToken, managerSession := sessionFor(manager)
	operator := User{ID: "operator-001", Name: "Кладовщик", Login: "operator", Role: "operator"}
	operatorToken, operatorSession := sessionFor(operator)
	admin := User{ID: "admin-000001", Name: "Администратор", Login: "admin", Role: "admin"}
	adminToken, adminSession := sessionFor(admin)
	for _, patch := range []Patch{{User: &manager, Session: &managerSession}, {User: &operator, Session: &operatorSession}, {User: &admin, Session: &adminSession}} {
		if err := a.st.commit(patch); err != nil {
			t.Fatal(err)
		}
	}
	command(t, a, operatorToken, Command{ID: identifier(), Type: "out", ItemID: "item-00001", From: "main-place", Qty: 1}, 200)
	command(t, a, operatorToken, Command{ID: identifier(), Type: "item", Item: &Item{ID: "new-item-001", Name: "Нет", Unit: "шт"}}, 403)
	command(t, a, managerToken, Command{ID: identifier(), Type: "reverse", Ref: a.st.S.Events[0].ID, Note: "Нет права"}, 403)
	command(t, a, managerToken, Command{ID: identifier(), Type: "disable", UserID: operator.ID}, 403)
	command(t, a, adminToken, Command{ID: identifier(), Type: "user-role", UserID: manager.ID, Role: "viewer"}, 200)
	if a.st.S.Users[manager.ID].Role != "viewer" {
		t.Fatal("admin role change not saved")
	}
	command(t, a, adminToken, Command{ID: identifier(), Type: "user-role", UserID: owner.ID, Role: "viewer"}, 404)
	command(t, a, ownerToken, Command{ID: identifier(), Type: "disable", UserID: owner.ID}, 403)
	item := a.st.S.Items["item-00001"]
	item.Price = 12345
	if err := a.st.commit(Patch{Item: &item}); err != nil {
		t.Fatal(err)
	}
	code, result := request(t, a, "GET", "/api/state", operatorToken, nil)
	if code != 200 || result["items"].(map[string]any)["item-00001"].(map[string]any)["price"].(float64) != 0 {
		t.Fatal("operator received hidden prices")
	}
}

func TestMainDeviceTransferRequiresTwoDeviceConfirmation(t *testing.T) {
	source, owner, token := fixture(t)
	owner.Salt = random(16)
	owner.Hash = passwordHash("owner-password-123", owner.Salt)
	if err := source.st.commit(Patch{User: &owner}); err != nil {
		t.Fatal(err)
	}
	seed(t, source, token, 4321)
	code, result := request(t, source, "POST", "/api/transfer/prepare", token, map[string]string{"device": "Новый компьютер"})
	if code != 200 || source.st.S.Host.Status != "transfer_pending" {
		t.Fatalf("transfer not prepared: %d %v", code, result)
	}
	command(t, source, token, Command{ID: identifier(), Type: "out", ItemID: "item-00001", From: "main-place", Qty: 1}, 423)
	data, _ := json.Marshal(result["transfer"])
	var transfer HostTransfer
	if err := json.Unmarshal(data, &transfer); err != nil {
		t.Fatal(err)
	}
	if transfer.Version != 2 || transfer.State.Host.Status != "awaiting_activation" || transfer.State.Host.Epoch != 2 || transfer.State.Host.Device != "Новый компьютер" || transfer.State.Transfer == nil || transfer.State.Transfer.ActivationToken != "" {
		t.Fatal("new host metadata invalid")
	}
	targetStore, err := openStore(filepath.Join(t.TempDir(), "yarus.journal"))
	if err != nil {
		t.Fatal(err)
	}
	defer targetStore.file.Close()
	target := &Server{st: targetStore, setup: "new-host-key", listen: "127.0.0.1:8787", attempts: map[string][]time.Time{}}
	code, imported := request(t, target, "POST", "/api/transfer/import", "", map[string]any{"setup": "new-host-key", "transfer": transfer})
	if code != 200 {
		t.Fatalf("import failed: %d %v", code, imported)
	}
	if target.st.S.Stocks[key("item-00001", "main-place")].Qty != 4321 || target.st.S.Host.Device != "Новый компьютер" || target.st.S.Host.Status != "awaiting_activation" || len(target.st.S.Users) != 1 || len(target.st.S.Sessions) != 1 {
		t.Fatal("import did not preserve operational state and owner")
	}
	targetToken, _ := imported["token"].(string)
	receiptCode, _ := imported["receiptCode"].(string)
	if targetToken == "" || receiptCode == "" {
		t.Fatal("target did not receive activation credentials")
	}
	command(t, target, targetToken, Command{ID: identifier(), Type: "out", ItemID: "item-00001", From: "main-place", Qty: 1}, 423)
	code, confirmed := request(t, source, "POST", "/api/transfer/confirm", token, map[string]string{"receiptCode": receiptCode})
	activationToken, _ := confirmed["activationToken"].(string)
	if code != 200 || activationToken == "" || source.st.S.Host.Status != "retired" {
		t.Fatalf("source confirmation failed: %d %v", code, confirmed)
	}
	code, activated := request(t, target, "POST", "/api/transfer/activate", targetToken, map[string]string{"activationToken": activationToken})
	if code != 200 || target.st.S.Host.Status != "active" || target.st.S.Transfer != nil {
		t.Fatalf("target activation failed: %d %v", code, activated)
	}
	command(t, target, targetToken, Command{ID: identifier(), Type: "out", ItemID: "item-00001", From: "main-place", Qty: 1}, 200)
	code, _ = request(t, target, "POST", "/api/transfer/import", "", map[string]any{"setup": "new-host-key", "transfer": transfer})
	if code != 409 {
		t.Fatal("active target accepted a second import")
	}
}

func TestPendingTransferCanBeCancelled(t *testing.T) {
	source, _, token := fixture(t)
	code, _ := request(t, source, "POST", "/api/transfer/prepare", token, map[string]string{"device": "Планшет"})
	if code != 200 || source.st.S.Host.Status != "transfer_pending" {
		t.Fatal("transfer was not prepared")
	}
	code, _ = request(t, source, "POST", "/api/transfer/cancel", token, map[string]string{})
	if code != 200 || source.st.S.Host.Status != "active" || source.st.S.Transfer != nil {
		t.Fatal("pending transfer was not cancelled")
	}
	seed(t, source, token, 1000)
}

func TestLocalHTTPRequiresEncryptedEnvelope(t *testing.T) {
	a, owner, _ := fixture(t)
	owner.Salt = random(16)
	owner.Hash = passwordHash("owner-password-123", owner.Salt)
	if err := a.st.commit(Patch{User: &owner}); err != nil {
		t.Fatal(err)
	}
	plainBody, _ := json.Marshal(map[string]string{"login": "owner", "password": "owner-password-123"})
	plain := httptest.NewRequest("POST", "/api/login", bytes.NewReader(plainBody))
	plain.RemoteAddr = "192.168.1.50:4321"
	plain.Header.Set("Content-Type", "application/json")
	plainResponse := httptest.NewRecorder()
	a.ServeHTTP(plainResponse, plain)
	if plainResponse.Code != 426 {
		t.Fatalf("plain LAN login accepted: %d", plainResponse.Code)
	}
	requestBody, _ := json.Marshal(transportRequest{Method: "POST", Path: "/api/login", Body: plainBody, At: time.Now().UnixMilli()})
	envelope, err := sealTransport(a.st.S.TransportKey, identifier(), "request", requestBody)
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(envelope)
	if bytes.Contains(wire, []byte("owner-password-123")) {
		t.Fatal("password leaked into HTTP envelope")
	}
	secure := httptest.NewRequest("POST", "/api/secure", bytes.NewReader(wire))
	secure.RemoteAddr = "192.168.1.50:4321"
	secure.Header.Set("Content-Type", "application/json")
	secureResponse := httptest.NewRecorder()
	a.ServeHTTP(secureResponse, secure)
	if secureResponse.Code != 200 {
		t.Fatalf("secure request failed: %d %s", secureResponse.Code, secureResponse.Body.String())
	}
	replay := httptest.NewRequest("POST", "/api/secure", bytes.NewReader(wire))
	replay.RemoteAddr = "192.168.1.50:4321"
	replay.Header.Set("Content-Type", "application/json")
	replayResponse := httptest.NewRecorder()
	a.ServeHTTP(replayResponse, replay)
	if replayResponse.Code != 409 {
		t.Fatalf("secure request replay accepted: %d", replayResponse.Code)
	}
	var responseEnvelope transportEnvelope
	if err = json.Unmarshal(secureResponse.Body.Bytes(), &responseEnvelope); err != nil {
		t.Fatal(err)
	}
	opened, err := openTransport(a.st.S.TransportKey, responseEnvelope, "response")
	if err != nil {
		t.Fatal(err)
	}
	var response transportResponse
	if err = json.Unmarshal(opened, &response); err != nil || response.Status != 200 {
		t.Fatalf("encrypted login response invalid: %v %s", err, opened)
	}
	var login map[string]any
	if err = json.Unmarshal(response.Body, &login); err != nil || login["token"] == "" {
		t.Fatal("encrypted login did not return a token")
	}
	if strings.Contains(secureResponse.Body.String(), login["token"].(string)) {
		t.Fatal("token leaked into HTTP response")
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

func TestJournalIsEncryptedAtRestAndLegacyIsMigrated(t *testing.T) {
	a, _, token := fixture(t)
	seed(t, a, token, 5000)
	path := a.st.Path
	spaceID := a.st.S.Space.ID
	a.st.file.Close()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("item-00001")) || bytes.Contains(raw, []byte("Основной склад")) || !bytes.Contains(raw, []byte(`"v":1`)) {
		t.Fatal("journal exposes warehouse data or is not encrypted")
	}
	if _, err = os.Stat(journalKeyPath(path)); err != nil {
		t.Fatal("protected journal key missing")
	}
	st, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.S.Space.ID != spaceID || st.S.Stocks[key("item-00001", "main-place")].Qty != 5000 {
		t.Fatal("encrypted journal did not reopen")
	}
	st.file.Close()

	legacyDir := t.TempDir()
	legacyPath := filepath.Join(legacyDir, "yarus.journal")
	patch := Patch{Seq: 1, Space: &Workspace{ID: "legacy-space", Name: "Секретный склад", Currency: "RUB"}, Host: func() *HostInfo { value := activeHost("Старый компьютер", 1); return &value }(), Place: &Place{ID: "main-place", Name: "Основной склад", Version: 1}}
	data, _ := json.Marshal(patch)
	record, _ := json.Marshal(Record{Data: data, Hash: hash(string(data))})
	if err = os.WriteFile(legacyPath, append(record, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	legacy, err := openStore(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	legacy.file.Close()
	migrated, _ := os.ReadFile(legacyPath)
	if bytes.Contains(migrated, []byte("Секретный склад")) || !bytes.Contains(migrated, []byte(`"v":1`)) {
		t.Fatal("legacy journal was not encrypted")
	}
	copies, _ := filepath.Glob(filepath.Join(legacyDir, "backups", "*.journal"))
	if len(copies) != 1 {
		t.Fatal("exact pre-encryption backup missing")
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

func TestPhysicalNetworkAddressRanksAheadOfVirtualAdapters(t *testing.T) {
	physical := networkAddressScore("Ethernet 2", net.ParseIP("192.168.100.3"))
	wifi := networkAddressScore("Wi-Fi", net.ParseIP("10.0.0.25"))
	tunnel := networkAddressScore("happ-default-tun", net.ParseIP("10.6.7.1"))
	hyperV := networkAddressScore("vEthernet (WSL (Hyper-V firewall))", net.ParseIP("172.28.208.1"))
	if physical <= tunnel || physical <= hyperV || wifi <= tunnel || wifi <= hyperV {
		t.Fatalf("physical adapters must rank first: ethernet=%d wifi=%d tunnel=%d hyper-v=%d", physical, wifi, tunnel, hyperV)
	}
}

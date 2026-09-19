// ЯРУС — локальный сервер общего склада. Только стандартная библиотека Go.
package main

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web/*
var assets embed.FS

const appVersion = "0.10.0"
const maxQty int64 = 1_000_000_000_000

type Workspace struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Currency string `json:"currency"`
}
type Item struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	SKU      string            `json:"sku"`
	Barcode  string            `json:"barcode"`
	Category string            `json:"category"`
	Unit     string            `json:"unit"`
	Min      int64             `json:"min"`
	Price    int64             `json:"price"`
	Note     string            `json:"note"`
	Fields   map[string]string `json:"fields"`
	Archived bool              `json:"archived"`
	Version  int               `json:"version"`
}
type Place struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Note    string `json:"note"`
	Version int    `json:"version"`
}
type Stock struct {
	Item    string `json:"item"`
	Place   string `json:"place"`
	Qty     int64  `json:"qty"`
	Version int    `json:"version"`
}
type Delta struct {
	Item  string `json:"item"`
	Place string `json:"place"`
	Qty   int64  `json:"qty"`
}
type Event struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	Item      string  `json:"item"`
	From      string  `json:"from"`
	To        string  `json:"to"`
	Qty       int64   `json:"qty"`
	Note      string  `json:"note"`
	Ref       string  `json:"ref"`
	Actor     string  `json:"actor"`
	ActorName string  `json:"actorName"`
	At        string  `json:"at"`
	Seq       int64   `json:"seq"`
	Changes   []Delta `json:"changes"`
}
type User struct {
	ID       string `json:"id"`
	Login    string `json:"login"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Salt     string `json:"salt"`
	Hash     string `json:"hash"`
	Disabled bool   `json:"disabled"`
}
type Invite struct {
	Hash    string `json:"hash"`
	Role    string `json:"role"`
	Expires int64  `json:"expires"`
	Used    bool   `json:"used"`
}
type Session struct {
	Hash    string `json:"hash"`
	User    string `json:"user"`
	Expires int64  `json:"expires"`
}
type Seen struct {
	Intent string `json:"intent"`
}
type State struct {
	Seq      int64              `json:"seq"`
	Space    Workspace          `json:"space"`
	Items    map[string]Item    `json:"items"`
	Places   map[string]Place   `json:"places"`
	Stocks   map[string]Stock   `json:"stocks"`
	Events   []Event            `json:"events"`
	Users    map[string]User    `json:"users"`
	Invites  map[string]Invite  `json:"invites"`
	Sessions map[string]Session `json:"sessions"`
	Seen     map[string]Seen    `json:"seen"`
}

// Every record is a small deterministic patch. Durable append + fsync precedes acknowledgment.
type Patch struct {
	Seq           int64      `json:"seq"`
	Key           string     `json:"key,omitempty"`
	Intent        string     `json:"intent,omitempty"`
	Space         *Workspace `json:"space,omitempty"`
	Item          *Item      `json:"item,omitempty"`
	Place         *Place     `json:"place,omitempty"`
	Stocks        []Stock    `json:"stocks,omitempty"`
	Event         *Event     `json:"event,omitempty"`
	User          *User      `json:"user,omitempty"`
	Invite        *Invite    `json:"invite,omitempty"`
	Session       *Session   `json:"session,omitempty"`
	DeleteSession string     `json:"deleteSession,omitempty"`
}
type Record struct {
	Data json.RawMessage `json:"data"`
	Hash string          `json:"hash"`
}
type Store struct {
	mu       sync.RWMutex
	S        State
	file     *os.File
	Path     string
	previous string
	failed   bool
}
type APIError struct {
	Code    int
	Message string
}

func (e *APIError) Error() string    { return e.Message }
func bad(code int, msg string) error { return &APIError{code, msg} }
func blankState() State {
	return State{Items: map[string]Item{}, Places: map[string]Place{}, Stocks: map[string]Stock{}, Events: []Event{}, Users: map[string]User{}, Invites: map[string]Invite{}, Sessions: map[string]Session{}, Seen: map[string]Seen{}}
}
func hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func random(n int) string {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func identifier() string            { return random(12) }
func key(item, place string) string { return item + "@" + place }
func apply(s *State, p Patch) {
	s.Seq = p.Seq
	if p.Space != nil {
		s.Space = *p.Space
	}
	if p.Item != nil {
		s.Items[p.Item.ID] = *p.Item
	}
	if p.Place != nil {
		s.Places[p.Place.ID] = *p.Place
	}
	for _, x := range p.Stocks {
		s.Stocks[key(x.Item, x.Place)] = x
	}
	if p.Event != nil {
		s.Events = append(s.Events, *p.Event)
	}
	if p.User != nil {
		s.Users[p.User.ID] = *p.User
	}
	if p.Invite != nil {
		s.Invites[p.Invite.Hash] = *p.Invite
	}
	if p.Session != nil {
		s.Sessions[p.Session.Hash] = *p.Session
	}
	if p.DeleteSession != "" {
		delete(s.Sessions, p.DeleteSession)
	}
	if p.Key != "" {
		s.Seen[p.Key] = Seen{p.Intent}
	}
}
func openStore(path string) (*Store, error) {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	st := &Store{S: blankState(), file: f, Path: path}
	r := bufio.NewReader(f)
	var offset int64
	for {
		line, err := r.ReadBytes('\n')
		if err == io.EOF {
			if len(line) > 0 {
				if e = f.Truncate(offset); e != nil {
					f.Close()
					return nil, e
				}
				if e = f.Sync(); e != nil {
					f.Close()
					return nil, e
				}
				log.Println("Recovered an unfinished final journal record; no acknowledged transaction removed.")
			}
			break
		}
		if err != nil {
			f.Close()
			return nil, err
		}
		var rec Record
		var p Patch
		if len(line) > 16<<20 || json.Unmarshal(line, &rec) != nil || rec.Hash != hash(st.previous+string(rec.Data)) || json.Unmarshal(rec.Data, &p) != nil || p.Seq != st.S.Seq+1 {
			f.Close()
			return nil, fmt.Errorf("journal integrity error at byte %d; preserve file and restore verified backup", offset)
		}
		apply(&st.S, p)
		st.previous = rec.Hash
		offset += int64(len(line))
	}
	_, e = f.Seek(0, io.SeekEnd)
	if e != nil {
		f.Close()
		return nil, e
	}
	// Add a stable QR namespace to legacy journals without rewriting stock or events.
	if len(st.S.Users) > 0 && st.S.Space.ID == "" {
		if _, err := st.backup(); err != nil {
			f.Close()
			return nil, fmt.Errorf("backup before namespace migration: %w", err)
		}
		space := st.S.Space
		space.ID = identifier()
		if err := st.commit(Patch{Space: &space}); err != nil {
			f.Close()
			return nil, err
		}
	}
	return st, nil
}

// Caller holds the write lock. A failure freezes writes until restart and inspection.
func (st *Store) commit(p Patch) error {
	if st.failed {
		return bad(503, "Ошибка записи на диск. Сервер остановил изменения. Проверьте диск и перезапустите сервер.")
	}
	p.Seq = st.S.Seq + 1
	if p.Event != nil {
		p.Event.Seq = p.Seq
	}
	data, e := json.Marshal(p)
	if e != nil {
		return e
	}
	rec := Record{data, hash(st.previous + string(data))}
	line, _ := json.Marshal(rec)
	line = append(line, '\n')
	n, e := st.file.Write(line)
	if e == nil && n != len(line) {
		e = io.ErrShortWrite
	}
	if e == nil {
		e = st.file.Sync()
	}
	if e != nil {
		st.failed = true
		log.Println("WRITE FAILED:", e)
		return bad(503, "Не удалось сохранить на диск. Операция не подтверждена; повторите после восстановления сервера.")
	}
	apply(&st.S, p)
	st.previous = rec.Hash
	return nil
}
func (st *Store) backup() (string, error) {
	st.mu.RLock()
	defer st.mu.RUnlock()
	dir := filepath.Join(filepath.Dir(st.Path), "backups")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	dest := filepath.Join(dir, "yarus-"+time.Now().Format("20060102-150405.000000000")+".journal")
	in, e := os.Open(st.Path)
	if e != nil {
		return "", e
	}
	defer in.Close()
	out, e := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return "", e
	}
	_, e = io.Copy(out, in)
	if e == nil {
		e = out.Sync()
	}
	ce := out.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return "", e
	}
	return dest, nil
}

// RFC 8018 PBKDF2-HMAC-SHA256; separate random salt per account.
func passwordHash(pass, salt string) string {
	mac := hmac.New(sha256.New, []byte(pass))
	mac.Write([]byte(salt))
	mac.Write([]byte{0, 0, 0, 1})
	u := mac.Sum(nil)
	out := append([]byte{}, u...)
	for i := 1; i < 310000; i++ {
		mac.Reset()
		mac.Write(u)
		u = mac.Sum(nil)
		for j := range out {
			out[j] ^= u[j]
		}
	}
	return hex.EncodeToString(out)
}
func newUser(login, name, pass, role string) User {
	u := User{ID: identifier(), Login: strings.ToLower(strings.TrimSpace(login)), Name: strings.TrimSpace(name), Role: role, Salt: random(16)}
	u.Hash = passwordHash(pass, u.Salt)
	return u
}
func (s *State) userFrom(token string) (User, error) {
	session, ok := s.Sessions[hash(token)]
	if !ok || session.Expires < time.Now().Unix() {
		return User{}, bad(401, "Войдите в общий склад заново.")
	}
	u, ok := s.Users[session.User]
	if !ok || u.Disabled {
		return User{}, bad(401, "Доступ отключён.")
	}
	return u, nil
}
func sessionFor(u User) (string, Session) {
	token := random(32)
	return token, Session{Hash: hash(token), User: u.ID, Expires: time.Now().Add(30 * 24 * time.Hour).Unix()}
}

var idRE = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,80}$`)
var loginRE = regexp.MustCompile(`^[a-zA-Z0-9_.@+-]{3,80}$`)

func validID(s string) bool {
	return idRE.MatchString(s) && s != "__proto__" && s != "constructor" && s != "prototype"
}
func text(s string, n int) bool { return len([]rune(s)) <= n && !strings.ContainsRune(s, 0) }
func credentials(login, name, pass string) error {
	if !loginRE.MatchString(login) || len(pass) < 10 || len(pass) > 256 || len(strings.TrimSpace(name)) < 1 || !text(name, 80) {
		return bad(400, "Логин: 3–80 латинских букв, цифр или . _ @ + -. Имя обязательно. Пароль: 10–256 символов.")
	}
	return nil
}
func allowedUnit(s string) bool {
	for _, u := range []string{"шт", "кг", "г", "л", "мл", "м", "м²", "м³", "упак", "компл", "т"} {
		if u == s {
			return true
		}
	}
	return false
}

type Command struct {
	ID       string     `json:"id"`
	Type     string     `json:"type"`
	Item     *Item      `json:"item,omitempty"`
	Place    *Place     `json:"place,omitempty"`
	ItemID   string     `json:"itemId,omitempty"`
	From     string     `json:"from,omitempty"`
	To       string     `json:"to,omitempty"`
	Qty      int64      `json:"qty"`
	Expected int        `json:"expected"`
	Note     string     `json:"note,omitempty"`
	Ref      string     `json:"ref,omitempty"`
	UserID   string     `json:"userId,omitempty"`
	Space    *Workspace `json:"space,omitempty"`
}

func prepare(s *State, u User, c Command) (Patch, error) {
	p := Patch{}
	if u.Role == "viewer" {
		return p, bad(403, "У вас доступ только для просмотра.")
	}
	if !validID(c.ID) {
		return p, bad(400, "Некорректный идентификатор операции.")
	}
	if !text(c.Note, 1000) || !text(c.Ref, 100) {
		return p, bad(400, "Слишком длинный комментарий или номер документа.")
	}
	switch c.Type {
	case "item":
		if c.Item == nil {
			return p, bad(400, "Нет карточки товара.")
		}
		x := *c.Item
		x.Name = strings.TrimSpace(x.Name)
		x.SKU = strings.TrimSpace(x.SKU)
		x.Barcode = strings.TrimSpace(x.Barcode)
		x.Category = strings.TrimSpace(x.Category)
		if !validID(x.ID) || x.Name == "" || !text(x.Name, 150) || !text(x.SKU, 80) || !text(x.Barcode, 80) || !text(x.Category, 80) || !text(x.Note, 2000) || !allowedUnit(x.Unit) || x.Min < 0 || x.Min > maxQty || x.Price < 0 || x.Price > 100_000_000 || len(x.Fields) > 12 {
			return p, bad(400, "Проверьте название, единицу, цену и минимум. До 12 дополнительных полей.")
		}
		for k, v := range x.Fields {
			if strings.TrimSpace(k) == "" || !text(k, 40) || !text(v, 160) {
				return p, bad(400, "Название доп. поля — до 40, значение — до 160 символов.")
			}
		}
		old, exists := s.Items[x.ID]
		if exists && x.Version != old.Version {
			return p, bad(409, "Карточку уже изменил другой участник. Обновите и повторите.")
		}
		if !exists && len(s.Items) >= 5000 {
			return p, bad(409, "Лимит этой версии — 5000 карточек на склад.")
		}
		if exists && x.Unit != old.Unit {
			for _, e := range s.Events {
				if e.Item == x.ID {
					return p, bad(409, "Единицу товара с историей менять нельзя. Создайте другую карточку.")
				}
			}
		}
		for id, y := range s.Items {
			if id != x.ID && ((x.SKU != "" && strings.EqualFold(y.SKU, x.SKU)) || (x.Barcode != "" && strings.EqualFold(y.Barcode, x.Barcode))) {
				return p, bad(409, "Такой артикул или штрихкод уже есть, в том числе в архиве.")
			}
		}
		if x.Archived {
			for _, v := range s.Stocks {
				if v.Item == x.ID && v.Qty != 0 {
					return p, bad(409, "Сначала обнулите остатки во всех местах, затем архивируйте товар.")
				}
			}
		}
		x.Version = old.Version + 1
		p.Item = &x
	case "place":
		if c.Place == nil {
			return p, bad(400, "Нет места хранения.")
		}
		x := *c.Place
		x.Name = strings.TrimSpace(x.Name)
		if !validID(x.ID) || x.Name == "" || !text(x.Name, 100) || !text(x.Note, 200) {
			return p, bad(400, "Укажите название места до 100 символов.")
		}
		old := s.Places[x.ID]
		if old.Version != x.Version {
			return p, bad(409, "Место уже изменено. Обновите данные.")
		}
		if old.Version == 0 && len(s.Places) >= 200 {
			return p, bad(409, "Лимит — 200 мест хранения.")
		}
		for id, x2 := range s.Places {
			if id != x.ID && strings.EqualFold(x.Name, x2.Name) {
				return p, bad(409, "Такое место уже существует.")
			}
		}
		x.Version++
		p.Place = &x
	case "space":
		if u.Role != "owner" {
			return p, bad(403, "Настройки склада меняет владелец.")
		}
		if c.Space == nil || strings.TrimSpace(c.Space.Name) == "" || !text(c.Space.Name, 80) {
			return p, bad(400, "Укажите название склада.")
		}
		x := *c.Space
		x.ID = s.Space.ID
		if x.Currency != "RUB" && x.Currency != "EUR" && x.Currency != "USD" {
			return p, bad(400, "Поддерживаются RUB, EUR и USD.")
		}
		p.Space = &x
	case "disable":
		if u.Role != "owner" || u.ID == c.UserID {
			return p, bad(403, "Нельзя отключить владельца или самого себя.")
		}
		x, ok := s.Users[c.UserID]
		if !ok || x.Role == "owner" {
			return p, bad(404, "Участник не найден.")
		}
		x.Disabled = true
		p.User = &x
	case "in", "out", "transfer", "count", "reverse":
		if c.Type == "reverse" && u.Role != "owner" {
			return p, bad(403, "Отмену проводит владелец.")
		}
		e := Event{ID: c.ID, Kind: c.Type, Item: c.ItemID, From: c.From, To: c.To, Qty: c.Qty, Note: c.Note, Ref: c.Ref, Actor: u.ID, ActorName: u.Name, At: time.Now().UTC().Format(time.RFC3339Nano)}
		if c.Type == "reverse" {
			var found *Event
			for i := range s.Events {
				old := &s.Events[i]
				if old.ID == c.Ref {
					found = old
				}
				if old.Kind == "reverse" && old.Ref == c.Ref {
					return p, bad(409, "Эта операция уже отменена.")
				}
			}
			if found == nil || found.Kind == "reverse" {
				return p, bad(400, "Можно отменить только существующее исходное движение.")
			}
			if strings.TrimSpace(c.Note) == "" {
				return p, bad(400, "Укажите причину отмены.")
			}
			e.Item = found.Item
			e.Qty = found.Qty
			e.From = found.To
			e.To = found.From
			for _, d := range found.Changes {
				e.Changes = append(e.Changes, Delta{d.Item, d.Place, -d.Qty})
			}
		} else {
			item, ok := s.Items[c.ItemID]
			if !ok || item.Archived {
				return p, bad(400, "Товар отсутствует или в архиве.")
			}
			if c.Qty < 0 || c.Qty > maxQty || (c.Type != "count" && c.Qty == 0) {
				return p, bad(400, "Количество должно быть больше нуля; точность — до 0,001.")
			}
			if c.Type == "in" {
				e.Changes = []Delta{{c.ItemID, c.To, c.Qty}}
			}
			if c.Type == "out" {
				e.Changes = []Delta{{c.ItemID, c.From, -c.Qty}}
			}
			if c.Type == "transfer" {
				if c.From == c.To {
					return p, bad(400, "Выберите разные места.")
				}
				e.Changes = []Delta{{c.ItemID, c.From, -c.Qty}, {c.ItemID, c.To, c.Qty}}
			}
			if c.Type == "count" {
				old := s.Stocks[key(c.ItemID, c.To)]
				if old.Version != c.Expected {
					return p, bad(409, "Во время пересчёта остаток изменился. Откройте пересчёт заново.")
				}
				if strings.TrimSpace(c.Note) == "" {
					return p, bad(400, "Укажите причину пересчёта.")
				}
				e.Changes = []Delta{{c.ItemID, c.To, c.Qty - old.Qty}}
			}
		}
		for _, d := range e.Changes {
			if _, ok := s.Places[d.Place]; !ok {
				return p, bad(400, "Место хранения не найдено.")
			}
			v := s.Stocks[key(d.Item, d.Place)]
			v.Item = d.Item
			v.Place = d.Place
			if d.Qty < 0 && v.Qty < -d.Qty {
				return p, bad(409, "Недостаточно остатка в выбранном месте. Обновите данные; операция не проведена.")
			}
			if d.Qty > 0 && v.Qty > maxQty-d.Qty {
				return p, bad(400, "Превышен максимальный остаток.")
			}
			v.Qty += d.Qty
			v.Version++
			p.Stocks = append(p.Stocks, v)
		}
		p.Event = &e
	default:
		return p, bad(400, "Неизвестный вид операции.")
	}
	return p, nil
}
func snapshot(s *State, u User, all bool) map[string]any {
	events := s.Events
	if !all && len(events) > 1000 {
		events = events[len(events)-1000:]
	}
	return map[string]any{"format": "yarus-data", "version": 1, "seq": s.Seq, "space": s.Space, "items": s.Items, "places": s.Places, "stocks": s.Stocks, "events": events, "eventCount": len(s.Events), "me": map[string]any{"id": u.ID, "name": u.Name, "login": u.Login, "role": u.Role}, "serverTime": time.Now().UTC().Format(time.RFC3339), "limits": map[string]int{"items": 5000, "places": 200}}
}

type Server struct {
	st       *Store
	setup    string
	listen   string
	limiter  sync.Mutex
	attempts map[string][]time.Time
}

func (a *Server) auth(r *http.Request) (User, error) {
	t := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return a.st.S.userFrom(t)
}
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
func fail(w http.ResponseWriter, e error) {
	code := 500
	msg := "Внутренняя ошибка сервера. Данные не подтверждены."
	var ae *APIError
	if errors.As(e, &ae) {
		code = ae.Code
		msg = ae.Message
	} else {
		log.Println(e)
	}
	writeJSON(w, code, map[string]string{"error": msg})
}
func readJSON(w http.ResponseWriter, r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return bad(415, "Нужен JSON.")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return bad(400, "Некорректный запрос или превышен размер 2 МБ.")
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return bad(400, "Лишние данные после JSON.")
	}
	return nil
}
func (a *Server) rate(r *http.Request) bool {
	a.limiter.Lock()
	defer a.limiter.Unlock()
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	now := time.Now()
	v := a.attempts[ip]
	j := 0
	for _, t := range v {
		if now.Sub(t) < 5*time.Minute {
			v[j] = t
			j++
		}
	}
	v = v[:j]
	if len(v) >= 15 {
		return false
	}
	a.attempts[ip] = append(v, now)
	return true
}
func (a *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	origin := r.Header.Get("Origin")
	same := origin == "http://"+r.Host || origin == "https://"+r.Host
	if origin != "" && (same || origin == "https://appassets.androidplatform.net" || origin == "null") {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
	}
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if r.URL.Path == "/api/info" && r.Method == "GET" {
		a.st.mu.RLock()
		ready := len(a.st.S.Users) > 0
		a.st.mu.RUnlock()
		writeJSON(w, 200, map[string]any{"version": appVersion, "ready": ready, "name": "ЯРУС"})
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(405)
			return
		}
		sub, _ := fs.Sub(assets, "web")
		w.Header().Set("Cache-Control", "no-cache")
		http.FileServer(http.FS(sub)).ServeHTTP(w, r)
		return
	}
	if r.Method == "POST" && (r.URL.Path == "/api/setup" || r.URL.Path == "/api/login" || r.URL.Path == "/api/join") {
		a.account(w, r)
		return
	}
	if r.URL.Path == "/api/command" && r.Method == "POST" {
		var c Command
		if e := readJSON(w, r, &c); e != nil {
			fail(w, e)
			return
		}
		a.st.mu.Lock()
		defer a.st.mu.Unlock()
		u, e := a.auth(r)
		if e != nil {
			fail(w, e)
			return
		}
		intent, _ := json.Marshal(c)
		k := u.ID + ":" + c.ID
		ih := hash(string(intent))
		if old, ok := a.st.S.Seen[k]; ok {
			if old.Intent != ih {
				fail(w, bad(409, "Идентификатор уже использован для другой операции."))
				return
			}
			writeJSON(w, 200, map[string]any{"duplicate": true, "state": snapshot(&a.st.S, u, false)})
			return
		}
		p, e := prepare(&a.st.S, u, c)
		if e != nil {
			fail(w, e)
			return
		}
		p.Key = k
		p.Intent = ih
		if e = a.st.commit(p); e != nil {
			fail(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"state": snapshot(&a.st.S, u, false)})
		return
	}
	if r.URL.Path == "/api/invite" && r.Method == "POST" {
		var input struct {
			Role string `json:"role"`
		}
		if e := readJSON(w, r, &input); e != nil {
			fail(w, e)
			return
		}
		a.st.mu.Lock()
		defer a.st.mu.Unlock()
		u, e := a.auth(r)
		if e != nil {
			fail(w, e)
			return
		}
		if u.Role != "owner" {
			fail(w, bad(403, "Приглашает владелец."))
			return
		}
		if input.Role != "editor" && input.Role != "viewer" {
			fail(w, bad(400, "Недопустимая роль."))
			return
		}
		code := random(18)
		inv := Invite{Hash: hash(code), Role: input.Role, Expires: time.Now().Add(24 * time.Hour).Unix()}
		if e = a.st.commit(Patch{Invite: &inv}); e != nil {
			fail(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"code": code, "expires": inv.Expires})
		return
	}
	if r.URL.Path == "/api/logout" && r.Method == "POST" {
		a.st.mu.Lock()
		defer a.st.mu.Unlock()
		_, e := a.auth(r)
		if e != nil {
			fail(w, e)
			return
		}
		e = a.st.commit(Patch{DeleteSession: hash(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))})
		if e != nil {
			fail(w, e)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.URL.Path == "/api/backup" && r.Method == "POST" {
		a.st.mu.RLock()
		u, e := a.auth(r)
		a.st.mu.RUnlock()
		if e != nil {
			fail(w, e)
			return
		}
		if u.Role != "owner" {
			fail(w, bad(403, "Резервную копию создаёт владелец."))
			return
		}
		path, e := a.st.backup()
		if e != nil {
			fail(w, e)
			return
		}
		writeJSON(w, 200, map[string]string{"filename": filepath.Base(path)})
		return
	}
	if r.Method == "GET" {
		a.st.mu.RLock()
		defer a.st.mu.RUnlock()
		u, e := a.auth(r)
		if e != nil {
			fail(w, e)
			return
		}
		switch r.URL.Path {
		case "/api/state":
			writeJSON(w, 200, snapshot(&a.st.S, u, false))
			return
		case "/api/export":
			if u.Role != "owner" {
				fail(w, bad(403, "Полную копию экспортирует владелец."))
				return
			}
			writeJSON(w, 200, snapshot(&a.st.S, u, true))
			return
		case "/api/team":
			if u.Role != "owner" {
				fail(w, bad(403, "Управление участниками доступно владельцу."))
				return
			}
			list := []map[string]any{}
			for _, x := range a.st.S.Users {
				list = append(list, map[string]any{"id": x.ID, "name": x.Name, "login": x.Login, "role": x.Role, "disabled": x.Disabled})
			}
			sort.Slice(list, func(i, j int) bool { return list[i]["name"].(string) < list[j]["name"].(string) })
			writeJSON(w, 200, map[string]any{"users": list, "addresses": addresses(a.listen)})
			return
		}
	}
	fail(w, bad(404, "Маршрут не найден."))
}
func (a *Server) account(w http.ResponseWriter, r *http.Request) {
	if !a.rate(r) {
		fail(w, bad(429, "Слишком много попыток. Повторите через 5 минут."))
		return
	}
	var input struct {
		Login    string `json:"login"`
		Name     string `json:"name"`
		Password string `json:"password"`
		Code     string `json:"code"`
		Setup    string `json:"setup"`
		Space    string `json:"space"`
	}
	if e := readJSON(w, r, &input); e != nil {
		fail(w, e)
		return
	}
	input.Login = strings.ToLower(strings.TrimSpace(input.Login))
	a.st.mu.Lock()
	defer a.st.mu.Unlock()
	var u User
	var p Patch
	switch r.URL.Path {
	case "/api/login":
		for _, x := range a.st.S.Users {
			if x.Login == input.Login {
				u = x
				break
			}
		}
		salt := u.Salt
		if salt == "" {
			salt = "dummy-salt-for-equal-work"
		}
		got := passwordHash(input.Password, salt)
		if u.ID == "" || u.Disabled || subtle.ConstantTimeCompare([]byte(got), []byte(u.Hash)) != 1 {
			fail(w, bad(401, "Неверный логин или пароль."))
			return
		}
	case "/api/setup", "/api/join":
		if e := credentials(input.Login, input.Name, input.Password); e != nil {
			fail(w, e)
			return
		}
		for _, x := range a.st.S.Users {
			if x.Login == input.Login {
				fail(w, bad(409, "Такой логин уже занят."))
				return
			}
		}
		role := "editor"
		if r.URL.Path == "/api/setup" {
			if len(a.st.S.Users) > 0 {
				fail(w, bad(409, "Склад уже создан. Войдите или используйте приглашение."))
				return
			}
			if a.setup == "" || subtle.ConstantTimeCompare([]byte(input.Setup), []byte(a.setup)) != 1 {
				fail(w, bad(403, "Для первого запуска откройте ссылку из окна сервера на компьютере."))
				return
			}
			if strings.TrimSpace(input.Space) == "" || !text(input.Space, 80) {
				fail(w, bad(400, "Укажите название склада до 80 символов."))
				return
			}
			role = "owner"
			p.Space = &Workspace{Name: strings.TrimSpace(input.Space), Currency: "RUB", ID: identifier()}
			p.Place = &Place{ID: "main-place", Name: "Основной склад", Version: 1}
		} else {
			inv, ok := a.st.S.Invites[hash(strings.TrimSpace(input.Code))]
			if !ok || inv.Used || inv.Expires < time.Now().Unix() {
				fail(w, bad(403, "Приглашение недействительно, использовано или истекло."))
				return
			}
			inv.Used = true
			p.Invite = &inv
			role = inv.Role
		}
		u = newUser(input.Login, input.Name, input.Password, role)
		p.User = &u
	}
	token, session := sessionFor(u)
	p.Session = &session
	if e := a.st.commit(p); e != nil {
		fail(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"token": token, "state": snapshot(&a.st.S, u, false)})
}
func addresses(listen string) []string {
	_, port, e := net.SplitHostPort(listen)
	if e != nil {
		port = "8787"
	}
	out := []string{}
	as, _ := net.InterfaceAddrs()
	for _, a := range as {
		ip, _, _ := net.ParseCIDR(a.String())
		if ip != nil && !ip.IsLoopback() && ip.To4() != nil {
			out = append(out, "http://"+ip.String()+":"+port)
		}
	}
	return out
}
func openURL(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// Prefer an application-style window; no browser download and no PowerShell is used.
		for _, root := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")} {
			p := filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe")
			if _, e := os.Stat(p); e == nil {
				cmd = exec.Command(p, "--app="+url)
				_ = cmd.Start()
				return
			}
		}
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
func main() {
	defaultDir := ""
	if runtime.GOOS == "windows" {
		defaultDir = filepath.Join(os.Getenv("LOCALAPPDATA"), "Yarus", "data")
	} else {
		home, _ := os.UserHomeDir()
		defaultDir = filepath.Join(home, ".local", "share", "yarus")
	}
	dir := flag.String("data", defaultDir, "Data directory")
	listen := flag.String("listen", "0.0.0.0:8787", "Listen address; use 127.0.0.1 behind a local HTTPS proxy")
	headless := flag.Bool("headless", false, "Do not open interface")
	flag.Parse()
	_ = os.MkdirAll(*dir, 0700)
	// A TCP listener is acquired BEFORE the journal is opened: prevents two standard instances.
	ln, e := net.Listen("tcp", *listen)
	if e != nil {
		log.Println("Port unavailable: ", e)
		if !*headless {
			nativeAlert("Порт ЯРУС уже занят. Закройте старый ЯРУС / сервер перед обновлением. Данные не изменены.")
		}
		return
	}
	// Exclusive lock prevents simultaneous writers even when a second process uses a different port.
	unlock, e := acquireDirLock(filepath.Join(*dir, "server.lock"))
	if e != nil {
		ln.Close()
		log.Println("Data folder is in use by another server:", e)
		if !*headless {
			nativeAlert("Папка данных уже открыта другим сервером ЯРУС или недоступна. Закройте старый сервер. Данные не удалены.")
		}
		return
	}
	defer unlock()
	st, e := openStore(filepath.Join(*dir, "yarus.journal"))
	if e != nil {
		log.Println(e)
		if !*headless {
			nativeAlert("Не удалось открыть журнал склада. Не удаляйте папку данных. Ошибка: " + e.Error())
		}
		return
	}
	defer st.file.Close()
	setup := random(24)
	server := &Server{st: st, setup: setup, listen: *listen, attempts: map[string][]time.Time{}}
	_, port, _ := net.SplitHostPort(*listen)
	base := "http://127.0.0.1:" + port
	url := base
	if len(st.S.Users) == 0 {
		url += "/#setup=" + setup
	}
	log.Printf("YARUS %s | %s\nData: %s\n", appVersion, url, *dir)
	for _, u := range addresses(*listen) {
		log.Println("LAN:", u)
	}
	// Only this private local file contains the one-time bootstrap link. It is replaced each launch.
	_ = os.WriteFile(filepath.Join(*dir, "OPEN-YARUS.url"), []byte("[InternetShortcut]\r\nURL="+url+"\r\n"), 0600)
	srv := &http.Server{Handler: server, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	if !*headless && runtime.GOOS != "windows" {
		go func() { time.Sleep(200 * time.Millisecond); openURL(url) }()
	}
	go func() {
		for range time.NewTicker(24 * time.Hour).C {
			if _, e := st.backup(); e != nil {
				log.Println("BACKUP:", e)
			}
		}
	}()
	if st.S.Seq > 0 {
		if _, e = st.backup(); e != nil {
			log.Println("BACKUP:", e)
		}
	}
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	go func() { <-quit; ln.Close() }()
	if !*headless && runtime.GOOS == "windows" {
		go func() {
			if e := srv.Serve(ln); e != nil && !errors.Is(e, http.ErrServerClosed) && !errors.Is(e, net.ErrClosed) {
				log.Println(e)
			}
		}()
		if e := desktopUI(url); e != nil {
			log.Println(e)
			nativeAlert(e.Error())
		}
		_ = srv.Close()
		return
	}
	e = srv.Serve(ln)
	if e != nil && !errors.Is(e, net.ErrClosed) && e != http.ErrServerClosed {
		log.Println(e)
	}
}

// Compile-time guards for standard-library-only build and API precision.
var _ = bytes.Equal
var _ = strconv.Itoa

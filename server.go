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

const appVersion = "1.1.2"
const maxQty int64 = 1_000_000_000_000
const backupRetention = 30

const (
	permCatalog      = "catalog"
	permPlaces       = "places"
	permStock        = "stock"
	permReverse      = "reverse"
	permSettings     = "settings"
	permTeam         = "team"
	permFullExport   = "full_export"
	permBackup       = "backup"
	permViewPrices   = "view_prices"
	permHostTransfer = "host_transfer"
)

var allPermissions = []string{
	permCatalog, permPlaces, permStock, permReverse, permSettings,
	permTeam, permFullExport, permBackup, permViewPrices, permHostTransfer,
}

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
	ID          string          `json:"id"`
	Login       string          `json:"login"`
	Name        string          `json:"name"`
	Role        string          `json:"role"`
	Permissions map[string]bool `json:"permissions,omitempty"`
	Salt        string          `json:"salt"`
	Hash        string          `json:"hash"`
	Disabled    bool            `json:"disabled"`
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
type HostInfo struct {
	Epoch      int64  `json:"epoch"`
	Status     string `json:"status"`
	Device     string `json:"device"`
	TransferID string `json:"transferId,omitempty"`
	UpdatedAt  string `json:"updatedAt"`
}
type State struct {
	Seq      int64              `json:"seq"`
	Space    Workspace          `json:"space"`
	Host     HostInfo           `json:"host"`
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
	Host          *HostInfo  `json:"host,omitempty"`
	Full          *State     `json:"full,omitempty"`
}
type Record struct {
	Version    int             `json:"v,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
	Nonce      string          `json:"nonce,omitempty"`
	Ciphertext string          `json:"ciphertext,omitempty"`
	Hash       string          `json:"hash"`
}
type Store struct {
	mu       sync.RWMutex
	S        State
	file     *os.File
	Path     string
	key      []byte
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
func activeHost(device string, epoch int64) HostInfo {
	if epoch < 1 {
		epoch = 1
	}
	return HostInfo{Epoch: epoch, Status: "active", Device: device, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
}
func apply(s *State, p Patch) {
	if p.Full != nil {
		*s = *p.Full
		s.Seq = p.Seq
		return
	}
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
	if p.Host != nil {
		s.Host = *p.Host
	}
	if p.Key != "" {
		s.Seen[p.Key] = Seen{p.Intent}
	}
}
func openStore(path string) (*Store, error) {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	journalKey, e := loadOrCreateJournalKey(path)
	if e != nil {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	st := &Store{S: blankState(), file: f, Path: path, key: journalKey}
	r := bufio.NewReader(f)
	var offset int64
	legacy := false
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
		if len(line) > 64<<20 || json.Unmarshal(line, &rec) != nil {
			f.Close()
			return nil, fmt.Errorf("journal integrity error at byte %d; preserve file and restore verified backup", offset)
		}
		var plain []byte
		if rec.Version == 1 {
			if len(rec.Data) != 0 || rec.Hash != encryptedRecordHash(st.previous, rec.Nonce, rec.Ciphertext) {
				f.Close()
				return nil, fmt.Errorf("journal integrity error at byte %d; preserve file and restore verified backup", offset)
			}
			plain, e = openJournalRecord(st.key, rec.Nonce, rec.Ciphertext, st.previous)
		} else {
			legacy = true
			if rec.Hash != hash(st.previous+string(rec.Data)) {
				e = errors.New("legacy journal hash mismatch")
			} else {
				plain = rec.Data
			}
		}
		if e != nil || json.Unmarshal(plain, &p) != nil || p.Seq != st.S.Seq+1 {
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
	if legacy {
		if _, err := st.backup(); err != nil {
			f.Close()
			return nil, fmt.Errorf("backup before encrypted journal migration: %w", err)
		}
		if err := st.rewriteEncrypted(); err != nil {
			return nil, fmt.Errorf("encrypted journal migration: %w", err)
		}
	}
	// Add stable QR and host metadata to legacy journals in one backed-up record.
	if len(st.S.Users) > 0 && (st.S.Space.ID == "" || st.S.Host.Epoch == 0) {
		if _, err := st.backup(); err != nil {
			st.file.Close()
			return nil, fmt.Errorf("backup before metadata migration: %w", err)
		}
		patch := Patch{}
		if st.S.Space.ID == "" {
			space := st.S.Space
			space.ID = identifier()
			patch.Space = &space
		}
		if st.S.Host.Epoch == 0 {
			host := activeHost("Windows-компьютер", 1)
			patch.Host = &host
		}
		if err := st.commit(patch); err != nil {
			st.file.Close()
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
	nonce, ciphertext, e := sealJournalRecord(st.key, data, st.previous)
	if e != nil {
		return e
	}
	rec := Record{Version: 1, Nonce: nonce, Ciphertext: ciphertext}
	rec.Hash = encryptedRecordHash(st.previous, rec.Nonce, rec.Ciphertext)
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
	if e := copyJournalKey(st.Path, dir); e != nil {
		return "", fmt.Errorf("backup encryption key: %w", e)
	}
	dest := filepath.Join(dir, "yarus-"+time.Now().Format("20060102-150405.000000000")+"-"+random(4)+".journal")
	in, e := os.Open(st.Path)
	if e != nil {
		return "", e
	}
	defer in.Close()
	out, e := os.CreateTemp(dir, ".yarus-backup-*.tmp")
	if e != nil {
		return "", e
	}
	temp := out.Name()
	ok := false
	defer func() {
		_ = out.Close()
		if !ok {
			_ = os.Remove(temp)
		}
	}()
	var copied int64
	copied, e = io.Copy(out, in)
	if e == nil {
		e = out.Sync()
	}
	ce := out.Close()
	if e == nil {
		e = ce
	}
	if e == nil {
		var info os.FileInfo
		info, e = in.Stat()
		if e == nil && info.Size() != copied {
			e = io.ErrShortWrite
		}
	}
	if e == nil {
		e = os.Rename(temp, dest)
	}
	if e != nil {
		return "", e
	}
	ok = true
	if e = pruneBackups(dir, backupRetention); e != nil {
		log.Println("BACKUP PRUNE:", e)
	}
	return dest, nil
}

func pruneBackups(dir string, keep int) error {
	entries, e := os.ReadDir(dir)
	if e != nil {
		return e
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "yarus-") && strings.HasSuffix(entry.Name(), ".journal") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)
	for len(files) > keep {
		if e = os.Remove(filepath.Join(dir, files[0])); e != nil {
			return e
		}
		files = files[1:]
	}
	return nil
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
	Role     string     `json:"role,omitempty"`
	Space    *Workspace `json:"space,omitempty"`
}

func roleDefaults(role string) map[string]bool {
	permissions := map[string]bool{}
	add := func(values ...string) {
		for _, value := range values {
			permissions[value] = true
		}
	}
	switch role {
	case "owner":
		add(allPermissions...)
	case "admin":
		add(permCatalog, permPlaces, permStock, permReverse, permSettings, permTeam, permFullExport, permBackup, permViewPrices)
	case "manager", "editor": // editor is kept for compatibility with 1.0.0 journals.
		add(permCatalog, permPlaces, permStock, permViewPrices)
	case "operator":
		add(permStock)
	case "viewer":
	}
	return permissions
}

func effectivePermissions(u User) map[string]bool {
	permissions := roleDefaults(u.Role)
	if u.Role == "owner" {
		return permissions
	}
	for key, value := range u.Permissions {
		known := false
		for _, permission := range allPermissions {
			if key == permission && key != permHostTransfer {
				known = true
				break
			}
		}
		if known {
			permissions[key] = value
		}
	}
	return permissions
}

func allowed(u User, permission string) bool { return effectivePermissions(u)[permission] }

func allowedRole(role string) bool {
	return role == "admin" || role == "manager" || role == "operator" || role == "viewer" || role == "editor"
}

func requirePermission(u User, permission, message string) error {
	if !allowed(u, permission) {
		return bad(403, message)
	}
	return nil
}

func prepare(s *State, u User, c Command) (Patch, error) {
	p := Patch{}
	if s.Host.Status == "retired" {
		return p, bad(423, "Это устройство больше не является главным. Подключитесь к новому главному устройству.")
	}
	if !validID(c.ID) {
		return p, bad(400, "Некорректный идентификатор операции.")
	}
	if !text(c.Note, 1000) || !text(c.Ref, 100) {
		return p, bad(400, "Слишком длинный комментарий или номер документа.")
	}
	switch c.Type {
	case "item":
		if e := requirePermission(u, permCatalog, "У вас нет права изменять каталог."); e != nil {
			return p, e
		}
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
		if e := requirePermission(u, permPlaces, "У вас нет права изменять места хранения."); e != nil {
			return p, e
		}
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
		if e := requirePermission(u, permSettings, "У вас нет права менять настройки склада."); e != nil {
			return p, e
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
		if e := requirePermission(u, permTeam, "У вас нет права управлять участниками."); e != nil {
			return p, e
		}
		if u.ID == c.UserID {
			return p, bad(403, "Нельзя отключить самого себя.")
		}
		x, ok := s.Users[c.UserID]
		if !ok || x.Role == "owner" {
			return p, bad(404, "Участник не найден.")
		}
		x.Disabled = true
		p.User = &x
	case "user-role":
		if e := requirePermission(u, permTeam, "У вас нет права управлять участниками."); e != nil {
			return p, e
		}
		if !allowedRole(c.Role) {
			return p, bad(400, "Недопустимый уровень доступа.")
		}
		x, ok := s.Users[c.UserID]
		if !ok || x.Role == "owner" {
			return p, bad(404, "Владельца нельзя заменить или понизить.")
		}
		x.Role = c.Role
		x.Permissions = nil
		p.User = &x
	case "in", "out", "transfer", "count", "reverse":
		permission := permStock
		message := "У вас нет права проводить складские движения."
		if c.Type == "reverse" {
			permission = permReverse
			message = "У вас нет права отменять движения."
		}
		if e := requirePermission(u, permission, message); e != nil {
			return p, e
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
	items := s.Items
	if !allowed(u, permViewPrices) {
		items = make(map[string]Item, len(s.Items))
		for id, item := range s.Items {
			item.Price = 0
			items[id] = item
		}
	}
	return map[string]any{
		"format": "yarus-data", "version": 1, "seq": s.Seq, "space": s.Space,
		"host": s.Host, "items": items, "places": s.Places, "stocks": s.Stocks,
		"events": events, "eventCount": len(s.Events),
		"me":         map[string]any{"id": u.ID, "name": u.Name, "login": u.Login, "role": u.Role, "permissions": effectivePermissions(u)},
		"serverTime": time.Now().UTC().Format(time.RFC3339),
		"limits":     map[string]int{"items": 5000, "places": 200},
	}
}

type HostTransfer struct {
	Format     string `json:"format"`
	Version    int    `json:"version"`
	AppVersion string `json:"appVersion"`
	CreatedAt  string `json:"createdAt"`
	State      State  `json:"state"`
}

func copyState(source State) (State, error) {
	data, err := json.Marshal(source)
	if err != nil {
		return State{}, err
	}
	var result State
	if err = json.Unmarshal(data, &result); err != nil {
		return State{}, err
	}
	return result, nil
}

func knownPermission(permission string) bool {
	for _, value := range allPermissions {
		if permission == value {
			return true
		}
	}
	return false
}

func validateImportedState(s State) error {
	if !validID(s.Space.ID) || strings.TrimSpace(s.Space.Name) == "" || !text(s.Space.Name, 80) || (s.Space.Currency != "RUB" && s.Space.Currency != "EUR" && s.Space.Currency != "USD") {
		return bad(400, "В пакете повреждены настройки склада.")
	}
	if s.Host.Epoch < 1 || s.Host.Status != "active" || !text(s.Host.Device, 80) || strings.TrimSpace(s.Host.Device) == "" {
		return bad(400, "В пакете повреждены сведения о главном устройстве.")
	}
	if s.Items == nil || s.Places == nil || s.Stocks == nil || s.Users == nil || s.Seen == nil || len(s.Items) > 5000 || len(s.Places) < 1 || len(s.Places) > 200 || len(s.Events) > 200000 || len(s.Users) < 1 || len(s.Users) > 500 || len(s.Seen) > 250000 {
		return bad(400, "Пакет превышает ограничения этой версии или содержит неполные данные.")
	}
	for id, place := range s.Places {
		if id != place.ID || !validID(id) || strings.TrimSpace(place.Name) == "" || !text(place.Name, 100) || !text(place.Note, 200) || place.Version < 1 {
			return bad(400, "В пакете повреждено место хранения.")
		}
	}
	for id, item := range s.Items {
		if id != item.ID || !validID(id) || strings.TrimSpace(item.Name) == "" || !text(item.Name, 150) || !text(item.SKU, 80) || !text(item.Barcode, 80) || !text(item.Category, 80) || !text(item.Note, 2000) || !allowedUnit(item.Unit) || item.Min < 0 || item.Min > maxQty || item.Price < 0 || item.Price > 100_000_000 || item.Version < 1 || len(item.Fields) > 12 {
			return bad(400, "В пакете повреждена карточка товара.")
		}
		for key, value := range item.Fields {
			if strings.TrimSpace(key) == "" || !text(key, 40) || !text(value, 160) {
				return bad(400, "В пакете повреждено дополнительное поле товара.")
			}
		}
	}
	ownerCount := 0
	logins := map[string]bool{}
	for id, user := range s.Users {
		if id != user.ID || !validID(id) || !loginRE.MatchString(user.Login) || strings.TrimSpace(user.Name) == "" || !text(user.Name, 80) || logins[strings.ToLower(user.Login)] {
			return bad(400, "В пакете повреждён участник или повторяется логин.")
		}
		logins[strings.ToLower(user.Login)] = true
		if user.Role == "owner" {
			ownerCount++
		} else if !allowedRole(user.Role) {
			return bad(400, "В пакете указана неизвестная роль участника.")
		}
		if len(user.Salt) < 16 || len(user.Salt) > 80 || len(user.Hash) != 64 {
			return bad(400, "В пакете повреждены данные входа участника.")
		}
		for permission := range user.Permissions {
			if !knownPermission(permission) || permission == permHostTransfer {
				return bad(400, "В пакете указано неизвестное право участника.")
			}
		}
	}
	if ownerCount != 1 {
		return bad(400, "В пакете должен быть ровно один владелец.")
	}
	computed := map[string]int64{}
	eventIDs := map[string]bool{}
	for _, event := range s.Events {
		if !validID(event.ID) || eventIDs[event.ID] || !validID(event.Item) || s.Items[event.Item].ID == "" || len(event.Changes) < 1 || len(event.Changes) > 2 || !text(event.Note, 1000) || !text(event.ActorName, 80) {
			return bad(400, "В пакете повреждена история движений.")
		}
		eventIDs[event.ID] = true
		for _, delta := range event.Changes {
			if delta.Item != event.Item || !validID(delta.Place) || s.Places[delta.Place].ID == "" || delta.Qty < -maxQty || delta.Qty > maxQty {
				return bad(400, "В пакете повреждено изменение остатка.")
			}
			stockKey := key(delta.Item, delta.Place)
			computed[stockKey] += delta.Qty
			if computed[stockKey] < 0 || computed[stockKey] > maxQty {
				return bad(400, "История пакета приводит к недопустимому остатку.")
			}
		}
	}
	for stockKey, stock := range s.Stocks {
		if stockKey != key(stock.Item, stock.Place) || !validID(stock.Item) || !validID(stock.Place) || s.Items[stock.Item].ID == "" || s.Places[stock.Place].ID == "" || stock.Qty != computed[stockKey] || stock.Version < 1 {
			return bad(400, "Остатки пакета не сходятся с историей.")
		}
		delete(computed, stockKey)
	}
	for _, qty := range computed {
		if qty != 0 {
			return bad(400, "Пакет содержит неполные остатки.")
		}
	}
	return nil
}

func requestIsLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func ownerOf(s State) (User, bool) {
	for _, user := range s.Users {
		if user.Role == "owner" && !user.Disabled {
			return user, true
		}
	}
	return User{}, false
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
	return readJSONLimit(w, r, v, 2<<20)
}
func readJSONLimit(w http.ResponseWriter, r *http.Request, v any, limit int64) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return bad(415, "Нужен JSON.")
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return bad(400, "Некорректный запрос или превышен допустимый размер файла.")
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
		host := a.st.S.Host
		a.st.mu.RUnlock()
		writeJSON(w, 200, map[string]any{"version": appVersion, "ready": ready, "name": "ЯРУС", "host": host})
		return
	}
	if r.URL.Path == "/api/transfer/import" && r.Method == "POST" {
		if !requestIsLoopback(r) {
			fail(w, bad(403, "Перенос на новый компьютер запускается только на самом новом компьютере."))
			return
		}
		var input struct {
			Setup    string       `json:"setup"`
			Transfer HostTransfer `json:"transfer"`
		}
		if e := readJSONLimit(w, r, &input, 32<<20); e != nil {
			fail(w, e)
			return
		}
		a.st.mu.Lock()
		defer a.st.mu.Unlock()
		if len(a.st.S.Users) > 0 || a.st.S.Seq > 0 {
			fail(w, bad(409, "Этот компьютер уже содержит склад. Для безопасного переноса используйте новую пустую папку ЯРУС."))
			return
		}
		if a.setup == "" || subtle.ConstantTimeCompare([]byte(input.Setup), []byte(a.setup)) != 1 {
			fail(w, bad(403, "Откройте перенос из окна ЯРУС на новом компьютере."))
			return
		}
		if input.Transfer.Format != "yarus-host-state" || input.Transfer.Version != 1 {
			fail(w, bad(400, "Это не пакет главного устройства ЯРУС."))
			return
		}
		imported := input.Transfer.State
		imported.Invites = map[string]Invite{}
		imported.Sessions = map[string]Session{}
		if imported.Seen == nil {
			imported.Seen = map[string]Seen{}
		}
		if e := validateImportedState(imported); e != nil {
			fail(w, e)
			return
		}
		owner, ok := ownerOf(imported)
		if !ok {
			fail(w, bad(400, "В пакете не найден владелец."))
			return
		}
		token, session := sessionFor(owner)
		imported.Sessions[session.Hash] = session
		imported.Seq = 0
		if e := a.st.commit(Patch{Full: &imported}); e != nil {
			fail(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"token": token, "state": snapshot(&a.st.S, owner, false)})
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
	if r.URL.Path == "/api/transfer/prepare" && r.Method == "POST" {
		if !requestIsLoopback(r) {
			fail(w, bad(403, "Пакет главного устройства создаётся только на самом главном компьютере."))
			return
		}
		var input struct {
			Device string `json:"device"`
		}
		if e := readJSON(w, r, &input); e != nil {
			fail(w, e)
			return
		}
		input.Device = strings.TrimSpace(input.Device)
		if input.Device == "" || !text(input.Device, 80) {
			fail(w, bad(400, "Укажите понятное название нового главного устройства."))
			return
		}
		a.st.mu.Lock()
		defer a.st.mu.Unlock()
		u, e := a.auth(r)
		if e != nil {
			fail(w, e)
			return
		}
		if !allowed(u, permHostTransfer) {
			fail(w, bad(403, "Только владелец передаёт роль главного устройства."))
			return
		}
		if a.st.S.Host.Status != "retired" {
			host := a.st.S.Host
			if host.Epoch < 1 {
				host = activeHost("Windows-компьютер", 1)
			}
			host.Status = "retired"
			host.TransferID = identifier()
			host.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			if e = a.st.commit(Patch{Host: &host}); e != nil {
				fail(w, e)
				return
			}
		}
		moved, e := copyState(a.st.S)
		if e != nil {
			fail(w, e)
			return
		}
		moved.Host = activeHost(input.Device, a.st.S.Host.Epoch+1)
		moved.Host.TransferID = a.st.S.Host.TransferID
		moved.Invites = map[string]Invite{}
		moved.Sessions = map[string]Session{}
		transfer := HostTransfer{Format: "yarus-host-state", Version: 1, AppVersion: appVersion, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), State: moved}
		writeJSON(w, 200, map[string]any{"transfer": transfer})
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
		if !allowed(u, permTeam) {
			fail(w, bad(403, "У вас нет права приглашать участников."))
			return
		}
		if !allowedRole(input.Role) {
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
		if !allowed(u, permBackup) {
			fail(w, bad(403, "У вас нет права создавать серверную копию."))
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
			if !allowed(u, permFullExport) {
				fail(w, bad(403, "У вас нет права экспортировать полную копию."))
				return
			}
			writeJSON(w, 200, snapshot(&a.st.S, u, true))
			return
		case "/api/team":
			if !allowed(u, permTeam) {
				fail(w, bad(403, "У вас нет права управлять участниками."))
				return
			}
			list := []map[string]any{}
			for _, x := range a.st.S.Users {
				list = append(list, map[string]any{"id": x.ID, "name": x.Name, "login": x.Login, "role": x.Role, "permissions": effectivePermissions(x), "disabled": x.Disabled})
			}
			sort.Slice(list, func(i, j int) bool { return list[i]["name"].(string) < list[j]["name"].(string) })
			writeJSON(w, 200, map[string]any{"users": list, "addresses": addresses(a.listen), "host": a.st.S.Host})
			return
		case "/api/storage":
			journalBytes := int64(0)
			if info, statErr := os.Stat(a.st.Path); statErr == nil {
				journalBytes = info.Size()
			}
			serialized, _ := json.Marshal(a.st.S)
			writeJSON(w, 200, map[string]any{
				"journalBytes": journalBytes, "packageBytes": len(serialized),
				"items": len(a.st.S.Items), "places": len(a.st.S.Places), "events": len(a.st.S.Events),
				"host": a.st.S.Host,
			})
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
		role := "manager"
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
			host := activeHost("Windows-компьютер", 1)
			p.Host = &host
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
	type candidate struct {
		url   string
		score int
	}
	seen := map[string]bool{}
	all := []candidate{}
	interfaces, _ := net.Interfaces()
	for _, network := range interfaces {
		if network.Flags&net.FlagUp == 0 || network.Flags&net.FlagLoopback != 0 {
			continue
		}
		as, _ := network.Addrs()
		for _, a := range as {
			ip, _, _ := net.ParseCIDR(a.String())
			if ip == nil || ip.To4() == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
				continue
			}
			address := "http://" + ip.String() + ":" + port
			if !seen[address] {
				seen[address] = true
				all = append(all, candidate{url: address, score: networkAddressScore(network.Name, ip)})
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].score != all[j].score {
			return all[i].score > all[j].score
		}
		return all[i].url < all[j].url
	})
	out := make([]string, 0, len(all))
	for _, item := range all {
		out = append(out, item.url)
	}
	return out
}

// networkAddressScore keeps real Wi-Fi/Ethernet addresses ahead of VPN, WSL,
// Hyper-V and other virtual adapters. All addresses remain available in the
// selector, but the first one is encoded into a newly-created QR invitation.
func networkAddressScore(name string, ip net.IP) int {
	n := strings.ToLower(strings.TrimSpace(name))
	score := 0
	if ip.IsPrivate() {
		score += 20
	}
	v4 := ip.To4()
	if v4 != nil && v4[0] == 192 && v4[1] == 168 {
		score += 20
	}
	if strings.Contains(n, "wi-fi") || strings.Contains(n, "wifi") || strings.Contains(n, "wlan") || strings.Contains(n, "wireless") || strings.Contains(n, "беспровод") {
		score += 70
	}
	if strings.HasPrefix(n, "ethernet") || strings.HasPrefix(n, "eth") || strings.HasPrefix(n, "en") {
		score += 50
	}
	for _, virtual := range []string{"vethernet", "wsl", "hyper-v", "virtual", "docker", "vmware", "virtualbox", "tunnel", " tun", "tun", " tap", "tap", "vpn", "tailscale", "zerotier", "hamachi", "happ"} {
		if strings.Contains(n, virtual) {
			score -= 200
			break
		}
	}
	return score
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
	portable := flag.Bool("portable", false, "Store data next to the executable")
	flag.Parse()
	dataWasSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "data" {
			dataWasSet = true
		}
	})
	if runtime.GOOS == "windows" && !dataWasSet {
		if executable, err := os.Executable(); err == nil {
			root := filepath.Dir(executable)
			_, markerErr := os.Stat(filepath.Join(root, "YARUS-PORTABLE.flag"))
			if *portable || markerErr == nil {
				*dir = filepath.Join(root, "data")
			}
		}
	}
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

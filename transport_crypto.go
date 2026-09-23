package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

const transportFormat = "yarus-secure-transport"

type transportEnvelope struct {
	Format     string `json:"format"`
	Version    int    `json:"version"`
	ID         string `json:"id"`
	IV         string `json:"iv"`
	Ciphertext string `json:"ciphertext"`
}

type transportRequest struct {
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Token  string          `json:"token"`
	Body   json.RawMessage `json:"body"`
	At     int64           `json:"at"`
}

type transportResponse struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

func validTransportKey(value string) bool {
	key, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(key) == 32
}

func transportAAD(id, direction string) []byte {
	return []byte("YARUS-TRANSPORT|1|" + id + "|" + direction)
}

func openTransport(keyText string, envelope transportEnvelope, direction string) ([]byte, error) {
	if envelope.Format != transportFormat || envelope.Version != 1 || !validID(envelope.ID) {
		return nil, errors.New("invalid transport envelope")
	}
	key, err := base64.RawURLEncoding.DecodeString(keyText)
	if err != nil || len(key) != 32 {
		return nil, errors.New("invalid transport key")
	}
	iv, err := base64.RawURLEncoding.DecodeString(envelope.IV)
	if err != nil || len(iv) != 12 {
		return nil, errors.New("invalid transport nonce")
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(envelope.Ciphertext)
	if err != nil || len(ciphertext) < 17 {
		return nil, errors.New("invalid transport ciphertext")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, iv, ciphertext, transportAAD(envelope.ID, direction))
}

func sealTransport(keyText, id, direction string, plain []byte) (transportEnvelope, error) {
	key, err := base64.RawURLEncoding.DecodeString(keyText)
	if err != nil || len(key) != 32 {
		return transportEnvelope{}, errors.New("invalid transport key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return transportEnvelope{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return transportEnvelope{}, err
	}
	iv := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(iv); err != nil {
		return transportEnvelope{}, err
	}
	ciphertext := gcm.Seal(nil, iv, plain, transportAAD(id, direction))
	return transportEnvelope{Format: transportFormat, Version: 1, ID: id, IV: base64.RawURLEncoding.EncodeToString(iv), Ciphertext: base64.RawURLEncoding.EncodeToString(ciphertext)}, nil
}

func (a *Server) secureRequest(w http.ResponseWriter, r *http.Request) {
	var envelope transportEnvelope
	if err := readJSONLimit(w, r, &envelope, 4<<20); err != nil {
		fail(w, err)
		return
	}
	a.st.mu.RLock()
	key := a.st.S.TransportKey
	a.st.mu.RUnlock()
	plain, err := openTransport(key, envelope, "request")
	if err != nil {
		fail(w, bad(403, "Защищённый запрос не прошёл проверку. Создайте новое приглашение."))
		return
	}
	var input transportRequest
	if err = json.Unmarshal(plain, &input); err != nil || (input.Method != "GET" && input.Method != "POST") || !strings.HasPrefix(input.Path, "/api/") || input.Path == "/api/secure" || len(input.Path) > 160 || time.Since(time.UnixMilli(input.At)) > 5*time.Minute || time.Until(time.UnixMilli(input.At)) > 5*time.Minute {
		fail(w, bad(400, "Защищённый запрос повреждён или устарел."))
		return
	}
	if !a.acceptSecureID(envelope.ID) {
		fail(w, bad(409, "Защищённый запрос уже был обработан. Повторите действие из приложения."))
		return
	}
	var body []byte
	if input.Method == "POST" {
		body = input.Body
		if len(body) == 0 {
			body = []byte("{}")
		}
	}
	inner := httptest.NewRequest(input.Method, input.Path, bytes.NewReader(body))
	inner.RemoteAddr = r.RemoteAddr
	inner.Header.Set("X-Yarus-Secure", "1")
	if input.Token != "" {
		inner.Header.Set("Authorization", "Bearer "+input.Token)
	}
	if input.Method == "POST" {
		inner.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	a.ServeHTTP(recorder, inner)
	responseBody := bytes.TrimSpace(recorder.Body.Bytes())
	if len(responseBody) == 0 || !json.Valid(responseBody) {
		responseBody = []byte(`{"error":"Сервер не вернул корректный ответ."}`)
	}
	responsePlain, _ := json.Marshal(transportResponse{Status: recorder.Code, Body: json.RawMessage(responseBody)})
	secured, err := sealTransport(key, envelope.ID, "response", responsePlain)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, secured)
}

package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const journalKeyMagic = "YARUSKEY1\n"

func journalKeyPath(journalPath string) string {
	return filepath.Join(filepath.Dir(journalPath), "yarus.key")
}

func loadOrCreateJournalKey(journalPath string) ([]byte, error) {
	path := journalKeyPath(journalPath)
	if data, err := os.ReadFile(path); err == nil {
		return decodeJournalKey(data)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	protected, err := protectJournalKey(key)
	if err != nil {
		return nil, fmt.Errorf("protect journal key: %w", err)
	}
	payload := []byte(journalKeyMagic + base64.StdEncoding.EncodeToString(protected) + "\n")
	temp, err := os.CreateTemp(filepath.Dir(path), ".yarus-key-*.tmp")
	if err != nil {
		return nil, err
	}
	tempPath := temp.Name()
	ok := false
	defer func() {
		_ = temp.Close()
		if !ok {
			_ = os.Remove(tempPath)
		}
	}()
	if err = temp.Chmod(0600); err == nil {
		_, err = temp.Write(payload)
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tempPath, path)
	}
	if err != nil {
		if existing, readErr := os.ReadFile(path); readErr == nil {
			return decodeJournalKey(existing)
		}
		return nil, err
	}
	ok = true
	return key, nil
}

func decodeJournalKey(data []byte) ([]byte, error) {
	text := string(data)
	if !strings.HasPrefix(text, journalKeyMagic) {
		return nil, errors.New("unknown YARUS key format")
	}
	protected, err := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.TrimPrefix(text, journalKeyMagic)))
	if err != nil || len(protected) == 0 {
		return nil, errors.New("damaged YARUS key")
	}
	key, err := unprotectJournalKey(protected)
	if err != nil {
		return nil, fmt.Errorf("this Windows account cannot unlock the warehouse key; use a protected .yarus transfer: %w", err)
	}
	if len(key) != 32 {
		return nil, errors.New("invalid YARUS key length")
	}
	return key, nil
}

func sealJournalRecord(key, plain []byte, previous string) (string, string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", "", err
	}
	aad := []byte("YARUS-JOURNAL-1|" + previous)
	ciphertext := gcm.Seal(nil, nonce, plain, aad)
	return base64.RawStdEncoding.EncodeToString(nonce), base64.RawStdEncoding.EncodeToString(ciphertext), nil
}

func openJournalRecord(key []byte, nonceText, ciphertextText, previous string) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce, err := base64.RawStdEncoding.DecodeString(nonceText)
	if err != nil || len(nonce) != gcm.NonceSize() {
		return nil, errors.New("invalid journal nonce")
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(ciphertextText)
	if err != nil || len(ciphertext) < gcm.Overhead() {
		return nil, errors.New("invalid journal ciphertext")
	}
	return gcm.Open(nil, nonce, ciphertext, []byte("YARUS-JOURNAL-1|"+previous))
}

func encryptedRecordHash(previous, nonce, ciphertext string) string {
	return hash("YARUS-JOURNAL-1|" + previous + "|" + nonce + "|" + ciphertext)
}

func copyJournalKey(sourceJournal, destinationDir string) error {
	source := journalKeyPath(sourceJournal)
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	destination := filepath.Join(destinationDir, "yarus.key")
	if old, readErr := os.ReadFile(destination); readErr == nil {
		if string(old) != string(data) {
			return errors.New("backup directory contains a different YARUS key")
		}
		return nil
	}
	return os.WriteFile(destination, data, 0600)
}

// rewriteEncrypted is only used for the one-time migration of a verified legacy journal.
// The exact legacy file is backed up before this function is called.
func (st *Store) rewriteEncrypted() error {
	snapshot, err := copyState(st.S)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(st.Path), ".yarus-encrypted-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	installed := false
	defer func() {
		_ = temp.Close()
		if !installed {
			_ = os.Remove(tempPath)
		}
	}()
	replacement := &Store{S: blankState(), file: temp, Path: st.Path, key: st.key}
	if err = replacement.commit(Patch{Full: &snapshot}); err != nil {
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	if err = st.file.Close(); err != nil {
		return err
	}
	if err = replaceFileAtomic(tempPath, st.Path); err != nil {
		return err
	}
	installed = true
	file, err := os.OpenFile(st.Path, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Seek(0, os.SEEK_END); err != nil {
		file.Close()
		return err
	}
	st.file = file
	st.S = replacement.S
	st.previous = replacement.previous
	st.failed = false
	return nil
}

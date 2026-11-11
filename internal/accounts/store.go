// Package accounts owns the persisted operator credential. Bootstrap credentials
// are used only for a new database; existing accounts are always authoritative.
package accounts

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	_ "modernc.org/sqlite"
)

var (
	ErrCredentials  = errors.New("Current username or password is incorrect")
	ErrPassword     = errors.New("Use a password of 12–256 characters, without control characters")
	ErrUnavailable  = errors.New("Operator account storage is unavailable")
	ErrUnconfigured = errors.New("Operator account is not configured")
	ErrBusy         = errors.New("Too many sign-in attempts; try again shortly")
)
var hashSlots = make(chan struct{}, 2)

const memoryKiB = 19 * 1024

type Store struct {
	path                   string
	key                    [32]byte
	mu                     sync.Mutex
	cachedUser, cachedHash string
	cachedProof            [32]byte
}

func Open(path, user, password string) (*Store, error) {
	s := &Store{path: path}
	if _, err := rand.Read(s.key[:]); err != nil {
		return nil, ErrUnavailable
	}
	info, err := os.Lstat(path)
	fresh := errors.Is(err, os.ErrNotExist)
	if fresh {
		if user == "" || password == "" {
			return nil, ErrUnconfigured
		}
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, ErrUnavailable
		}
		f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if e != nil {
			return nil, ErrUnavailable
		}
		f.Close()
	} else if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, ErrUnavailable
	}
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY CHECK(id=1), username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, password_changed_at INTEGER NOT NULL)`); err != nil {
		return nil, ErrUnavailable
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM users`).Scan(&count); err != nil {
		return nil, ErrUnavailable
	}
	if count == 0 {
		// Only a newly created file may bootstrap. An empty existing database must
		// never resurrect old environment credentials after corruption or deletion.
		if !fresh || user == "" || password == "" {
			return nil, ErrUnavailable
		}
		encoded, err := hashPassword(password)
		if err != nil {
			return nil, err
		}
		if _, err = db.Exec(`INSERT INTO users(id,username,password_hash,password_changed_at) VALUES(1,?,?,?)`, user, encoded, time.Now().Unix()); err != nil {
			return nil, ErrUnavailable
		}
	}
	if count > 1 {
		return nil, ErrUnavailable
	}
	return s, nil
}
func (s *Store) database() (*sql.DB, error) {
	info, err := os.Lstat(s.path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, ErrUnavailable
	}
	absolute, err := filepath.Abs(s.path)
	if err != nil {
		return nil, ErrUnavailable
	}
	dsn := (&url.URL{Scheme: "file", Path: absolute, RawQuery: "mode=rw"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, ErrUnavailable
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		db.Close()
		return nil, ErrUnavailable
	}
	return db, nil
}
func (s *Store) credential(user string) (string, error) {
	db, err := s.database()
	if err != nil {
		return "", err
	}
	defer db.Close()
	var hash string
	err = db.QueryRow(`SELECT password_hash FROM users WHERE id=1 AND username=?`, user).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrCredentials
	}
	if err != nil {
		return "", ErrUnavailable
	}
	return hash, nil
}
func (s *Store) proof(user, password string) [32]byte {
	mac := hmac.New(sha256.New, s.key[:])
	mac.Write([]byte(user))
	mac.Write([]byte{0})
	mac.Write([]byte(password))
	var sum [32]byte
	copy(sum[:], mac.Sum(nil))
	return sum
}
func (s *Store) Verify(user, password string) error {
	encoded, err := s.credential(user)
	if err != nil {
		return err
	}
	proof := s.proof(user, password)
	s.mu.Lock()
	cached := s.cachedUser == user && s.cachedHash == encoded && subtle.ConstantTimeCompare(s.cachedProof[:], proof[:]) == 1
	s.mu.Unlock()
	if cached {
		return nil
	}
	if err = verifyPassword(encoded, password); err != nil {
		return err
	}
	s.mu.Lock()
	s.cachedUser, s.cachedHash, s.cachedProof = user, encoded, proof
	s.mu.Unlock()
	return nil
}
func validPassword(password string) bool {
	n := utf8.RuneCountInString(password)
	if n < 12 || n > 256 || !utf8.ValidString(password) {
		return false
	}
	for _, r := range password {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func (s *Store) Change(user, current, replacement string) error {
	if !validPassword(replacement) {
		return ErrPassword
	}
	encoded, err := s.credential(user)
	if err != nil {
		return err
	}
	if err = verifyPassword(encoded, current); err != nil {
		return err
	}
	return s.replace(user, encoded, replacement)
}

// Reset requires filesystem access to the private database, never an HTTP call.
// The existing username is required so a typo cannot create a second account.
func (s *Store) Reset(user, replacement string) error {
	if !validPassword(replacement) {
		return ErrPassword
	}
	encoded, err := s.credential(user)
	if err != nil {
		return err
	}
	return s.replace(user, encoded, replacement)
}
func (s *Store) replace(user, previous, replacement string) error {
	encoded, err := hashPassword(replacement)
	if err != nil {
		return err
	}
	db, err := s.database()
	if err != nil {
		return err
	}
	defer db.Close()
	result, err := db.Exec(`UPDATE users SET password_hash=?,password_changed_at=? WHERE id=1 AND username=? AND password_hash=?`, encoded, time.Now().Unix(), user, previous)
	if err != nil {
		return ErrUnavailable
	}
	n, err := result.RowsAffected()
	if err != nil {
		return ErrUnavailable
	}
	if n != 1 {
		return ErrCredentials
	}
	return nil
}
func hashPassword(password string) (string, error) {
	select {
	case hashSlots <- struct{}{}:
		defer func() { <-hashSlots }()
	default:
		return "", ErrBusy
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", ErrUnavailable
	}
	hash := argon2.IDKey([]byte(password), salt, 2, memoryKiB, 1, 32)
	return fmt.Sprintf("$argon2id$v=19$m=19456,t=2,p=1$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}
func verifyPassword(encoded, password string) error {
	if len(password) > 4096 {
		return ErrCredentials
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=19456,t=2,p=1" {
		return ErrUnavailable
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != 16 {
		return ErrUnavailable
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) != 32 {
		return ErrUnavailable
	}
	select {
	case hashSlots <- struct{}{}:
		defer func() { <-hashSlots }()
	default:
		return ErrBusy
	}
	actual := argon2.IDKey([]byte(password), salt, 2, memoryKiB, 1, 32)
	if subtle.ConstantTimeCompare(actual, expected) != 1 {
		return ErrCredentials
	}
	return nil
}

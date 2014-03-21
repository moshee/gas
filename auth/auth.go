package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
	"github.com/moshee/gas"
	"github.com/moshee/gas/db"

	"code.google.com/p/go.crypto/scrypt"
	"code.google.com/p/go.crypto/sha3"
)

var (
	errNoUser        = errors.New("gas: no user has been provided")
	errBadPassword   = errors.New("invalid username or password")
	errCookieExpired = errors.New("session cookie expired")
	errBadMac        = errors.New("HMAC digests don't match")
	hmacKeys         [][]byte
	store            SessionStore
)

// keccak256
const macLength = 32

// Env contains the environment variables specific to user authentication.
var Env struct {
	// Maximum age of a cookie before it goes stale. Syntax specified as in
	// time.ParseDuration (maximum unit is hours 'h')
	MaxCookieAge time.Duration `default:"186h"`

	// The key used in HMAC signing of cookies. If it's blank, no signing will
	// be used. Multiple os.PathListSeparator-separated keys can be used to
	// allow for key rotation; the keys will be tried in order from left to
	// right.
	CookieAuthKey []byte

	// The name of the database table in which sessions will be stored
	SessTable string `default:"gas_sessions"`

	// The length of the session ID in bytes
	SessidLen int `default:"64"`

	// HASH_COST is the cost passed into the scrypt hash function. It is
	// represented as the power of 2 (aka HASH_COST=9 means 2<<9 iterations).
	// It should be set as desired in the main() function of the importing
	// client. A value of 13 (the default) is a good number to start with, and
	// should be increased as hardware gets faster (see
	// http://www.tarsnap.com/scrypt.html for more info)
	HashCost uint `default:"13"`
}

func init() {
	if err := gas.EnvConf(&Env, gas.EnvPrefix); err != nil {
		log.Fatalf("auth (init): %v", err)
	}

	if len(Env.CookieAuthKey) > 0 {
		hmacKeys = bytes.Split(Env.CookieAuthKey, []byte{byte(os.PathListSeparator)})
	}

	_, err := db.DB.Exec("CREATE TABLE IF NOT EXISTS " + Env.SessTable +
		" ( id bytea, expires timestamptz, username text )")
	if err != nil {
		log.Fatalf("db (init): %v", err)
	}

}

// A User is a generic representation of a user with some common traits
type User interface {
	Username() string
	Secrets() (passHash, salt []byte, err error)
}

// Session is a secure session to be stored temporarily or long-term.
type Session struct {
	Id       []byte
	Expires  time.Time
	Username string
}

// UseSessionStore instructs the package to use the given store to store
// sessions. Must be called if one wishes to use sessions. Must be called
// during app init, not during runtime.
func UseSessionStore(s SessionStore) {
	store = s
}

// SessionStore is the interface that is satisfied by backing stores for user
// sessions. It must be safe for concurrent access.
type SessionStore interface {
	Create(id []byte, expires time.Time, username string) error
	Read(id []byte) (*Session, error)
	Update(id []byte) error
	Delete(id []byte) error
}

// DBStore is a session store that stores sessions in a database table.
type DBStore struct {
	// The name of the table.
	Table string
}

func (s *DBStore) Create(id []byte, expires time.Time, username string) error {
	_, err := db.DB.Exec("INSERT INTO "+s.Table+" VALUES ( $1, $2, $3 )",
		id, expires, username)

	return err
}

func (s *DBStore) Read(id []byte) (*Session, error) {
	sess := new(Session)
	err := db.Query(sess, "SELECT * FROM "+s.Table+" WHERE id = $1", id)
	return sess, err
}

func (s *DBStore) Update(id []byte) error {
	exp := time.Now().Add(Env.MaxCookieAge)
	_, err := db.DB.Exec("UPDATE "+s.Table+" SET expires = $1", exp)
	return err
}

func (s *DBStore) Delete(id []byte) error {
	_, err := db.DB.Exec("DELETE FROM "+s.Table+" WHERE id = $1", id)
	return err
}

func NewFileStore() (*FileStore, error) {
	tmp, err := ioutil.TempDir("", "gas-sessions")
	if err != nil {
		return nil, err
	}
	return &FileStore{Root: tmp}, nil
}

type FileStore struct {
	Root string
	sync.RWMutex
}

func (s *FileStore) Path(id []byte) string {
	return filepath.Join(s.Root, base64.URLEncoding.EncodeToString(id))
}

func (s *FileStore) Destroy() {
	os.RemoveAll(s.Root)
}

func (s *FileStore) Create(id []byte, expires time.Time, username string) error {
	s.Lock()
	defer s.Unlock()
	err := os.MkdirAll(s.Root, os.FileMode(0700))
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.Path(id), os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(0600))
	if err != nil {
		return err
	}
	sess := &Session{id, expires, username}
	err = json.NewEncoder(f).Encode(sess)
	if err != nil {
		return err
	}
	return f.Close()
}

func (s *FileStore) Read(id []byte) (*Session, error) {
	s.RLock()
	defer s.RUnlock()
	f, err := os.Open(s.Path(id))
	if err != nil {
		return nil, err
	}

	sess := new(Session)
	err = json.NewDecoder(f).Decode(sess)
	if err != nil {
		return nil, err
	}

	if time.Now().After(sess.Expires) {
		return nil, errCookieExpired
	}
	return sess, err
}

func (s *FileStore) Update(id []byte) error {
	s.Lock()
	defer s.Unlock()

	now := time.Now()
	sess := new(Session)
	path := s.Path(id)
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	err = json.NewDecoder(f).Decode(sess)
	f.Close()
	if err != nil {
		return err
	}
	f, err = os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(0600))
	if err != nil {
		return err
	}
	sess.Expires = now.Add(Env.MaxCookieAge)
	err = json.NewEncoder(f).Encode(sess)
	return err
}

func (s *FileStore) Delete(id []byte) error {
	s.Lock()
	defer s.Unlock()
	return os.Remove(s.Path(id))
}

// GetSession figures out the session from the session cookie in the request, or
// just return the session if that's been done already.
func GetSession(g *gas.Gas) (*Session, error) {
	const sessKey = "_gas_session"
	if sessBox := g.Data(sessKey); sessBox != nil {
		if sess, ok := sessBox.(*Session); ok {
			return sess, nil
		}
	}

	cookie, err := g.Cookie("s")
	if err != nil {
		if err == http.ErrNoCookie {
			return nil, nil
		}
		SignOut(g)
		return nil, err
	}

	if err = VerifyCookie(cookie); err != nil {
		return nil, err
	}

	id, err := base64.StdEncoding.DecodeString(cookie.Value)
	if err != nil {
		// this means invalid session
		SignOut(g)
		return nil, err
	}
	//id := []byte(cookie.Value)

	sess, err := store.Read(id)

	if err != nil {
		if err == sql.ErrNoRows {
			SignOut(g)
			return nil, nil
		}
		return nil, err
	}

	if time.Now().After(sess.Expires) {
		SignOut(g)
		return nil, errCookieExpired
	}

	g.SetData(sessKey, sess)

	return sess, nil
}

// SignIn signs the user in by creating a new session and setting a cookie on
// the client.
func SignIn(g *gas.Gas, u User) error {
	// already signed in?
	sess, _ := GetSession(g)
	if sess != nil {
		cookie, err := g.Cookie("s")
		if err != nil && err != http.ErrNoCookie {
			return err
		}

		if err = VerifyCookie(cookie); err != nil {
			return err
		}

		id, err := base64.StdEncoding.DecodeString(cookie.Value)
		if err != nil {
			return err
		}
		//id := []byte(cookie.Value)

		if err := store.Update(id); err != nil {
			return err
		}

		return nil
	}

	pass, salt, err := u.Secrets()
	if err != nil {
		return err
	}
	if !VerifyHash([]byte(g.FormValue("pass")), pass, salt) {
		return errBadPassword
	}

	username := u.Username()
	sessid := make([]byte, Env.SessidLen)
	rand.Read(sessid)
	err = store.Create(sessid, time.Now().Add(Env.MaxCookieAge), username)
	if err != nil {
		return err
	}

	// TODO: determine if setting the path to / is always what we want. If it's
	// set to anything other than /, then only requests to that path will
	// include the cookie in the header (the browser restricts this).
	cookie := &http.Cookie{
		Name:     "s",
		Path:     "/",
		Value:    base64.StdEncoding.EncodeToString(sessid),
		MaxAge:   int(Env.MaxCookieAge / time.Second),
		HttpOnly: true,
	}

	SignCookie(cookie)

	g.SetCookie(cookie)

	return nil
}

// SignOut signs the user out, destroying the associated session and cookie.
func SignOut(g *gas.Gas) error {
	cookie, err := g.Cookie("s")
	if err != nil {
		return err
	}
	if err := VerifyCookie(cookie); err != nil {
		return err
	}

	id, err := base64.StdEncoding.DecodeString(cookie.Value)
	if err != nil {
		return err
	}
	//id := []byte(cookie.Value)

	if err := store.Delete(id); err != nil && err != sql.ErrNoRows {
		return err
	}

	cookie = &http.Cookie{
		Name:     "s",
		Path:     "/",
		Value:    "",
		Expires:  time.Time{},
		MaxAge:   -1,
		HttpOnly: true,
	}

	SignCookie(cookie)
	g.SetCookie(cookie)

	return nil
}

// SignCookie signs a cookie's value with the configured HMAC key, if it exists
func SignCookie(cookie *http.Cookie) {
	if hmacKeys != nil && len(hmacKeys) > 0 {
		// so what's going on here is that stuff is getting base64 encoded two
		// times. First the value, and then the hmac is appended and it's all
		// encoded again.
		b := []byte(cookie.Value)
		sum := hmacSum(b, hmacKeys[0], b)
		cookie.Value = base64.StdEncoding.EncodeToString(sum)
	}
}

// VerifyCookie checks and un-signs the cookie's contents against all of the
// configured HMAC keys.
func VerifyCookie(cookie *http.Cookie) error {
	decodedLen := base64.StdEncoding.DecodedLen(len(cookie.Value))
	if hmacKeys == nil || len(hmacKeys) == 0 || decodedLen < macLength {
		return nil
	}

	p, err := base64.StdEncoding.DecodeString(cookie.Value)
	if err != nil {
		return err
	}

	var (
		pos = len(p) - macLength
		val = p[:pos]
		sum = p[pos:]
	)

	for _, key := range hmacKeys {
		s := hmacSum(val, key, nil)
		if hmac.Equal(s, sum) {
			// So when we reset the value of the cookie to the un-signed value,
			// we're not decoding or encoding it again.
			// I guess this is how WTFs happen.
			cookie.Value = string(val)
			return nil
		}
	}

	return errBadMac
}

func hmacSum(plaintext, key, b []byte) []byte {
	mac := hmac.New(sha3.NewKeccak256, key)
	mac.Write(plaintext)
	return mac.Sum(b)
}

// VerifyHash checks if the supplied passphrase matches the expected hash using
// the salt.
func VerifyHash(supplied, expected, salt []byte) bool {
	hashed := Hash(supplied, salt)
	return hmac.Equal(expected, hashed)
}

// Hash the given passphrase using the salt provided.
func Hash(pass []byte, salt []byte) []byte {
	hash, _ := scrypt.Key(pass, salt, 2<<Env.HashCost, 8, 1, 32)
	return hash
}

// NewHash creates a new hash and random salt from the supplied password.
func NewHash(pass []byte) (hash, salt []byte) {
	salt = make([]byte, 16)
	rand.Read(salt)
	hash = Hash([]byte(pass), salt)
	return
}

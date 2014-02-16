package gas

import (
	"crypto/hmac"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"code.google.com/p/go.crypto/scrypt"
)

var (
	errNoUser        = errors.New("gas: no user has been provided")
	errBadPassword   = errors.New("Invalid username or password.")
	errCookieExpired = errors.New("Your session has expired. Please log in again.")
	errBadMac        = errors.New("HMAC digests don't match")
	hmacKeys         [][]byte
	store            SessionStore
)

// keccak256
const macLength = 32

type User interface {
	Username() string
	Secrets() (passHash, salt []byte, err error)
}

// Describes a secure session to be stored temporarily or long-term.
type Session struct {
	Id       []byte
	Expires  time.Time
	Username string
}

// Create a new random session ID, base64 encoded
func NewSession(username string) []byte {
	sessid := make([]byte, Env.SessidLen)
	rand.Read(sessid)
	return sessid
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
	_, err := DB.Exec("INSERT INTO "+s.Table+" VALUES ( $1, $2, $3 )",
		id, expires, username)

	return err
}

func (s *DBStore) Read(id []byte) (*Session, error) {
	sess := new(Session)
	err := Query(sess, "SELECT * FROM "+s.Table+" WHERE id = $1", id)
	return sess, err
}

func (s *DBStore) Update(id []byte) error {
	_, err := DB.Exec("UPDATE " + s.Table + " SET expires = now() + '7d'")
	return err
}

func (s *DBStore) Delete(id []byte) error {
	_, err := DB.Exec("DELETE FROM "+s.Table+" WHERE id = $1", id)
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
	return sess, err
}

func (s *FileStore) Update(id []byte) error {
	s.Lock()
	defer s.Unlock()
	now := time.Now()
	return os.Chtimes(s.Path(id), now, now)
}

func (s *FileStore) Delete(id []byte) error {
	s.Lock()
	defer s.Unlock()
	return os.Remove(s.Path(id))
}

// Figure out the session from the session cookie in the request, or just
// return the session if that's been done already.
func (g *Gas) Session() (*Session, error) {
	if g.session != nil {
		return g.session, nil
	}

	cookie, err := g.Cookie("s")
	if err != nil {
		if err == http.ErrNoCookie {
			return nil, nil
		} else {
			g.SignOut()
			return nil, err
		}
	}

	id, err := base64.StdEncoding.DecodeString(cookie.Value)
	if err != nil {
		// this means invalid session
		g.SignOut()
		return nil, err
	}
	//id := []byte(cookie.Value)

	g.session, err = store.Read(id)

	if err != nil {
		if err == sql.ErrNoRows {
			g.SignOut()
			return nil, nil
		} else {
			return nil, err
		}
	}

	if g.session != nil && time.Now().After(g.session.Expires) {
		g.SignOut()
		return nil, errCookieExpired
	}

	return g.session, nil
}

// Signs the user in by creating a new session and setting a cookie on the
// client.
func (g *Gas) SignIn(u User) error {
	// already signed in?
	sess, _ := g.Session()
	if sess != nil {
		cookie, err := g.Cookie("s")
		if err != nil && err != http.ErrNoCookie {
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
	sessid := NewSession(username)
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
		MaxAge:   int(Env.MaxCookieAge / time.Second),
		HttpOnly: true,
	}

	g.SetCookie(cookie, sessid)

	return nil
}

// Signs the user out, destroying the associated session and cookie.
func (g *Gas) SignOut() error {
	cookie, err := g.Cookie("s")
	if err != nil {
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

	g.SetCookie(&http.Cookie{
		Name:     "s",
		Path:     "/",
		Value:    "",
		Expires:  time.Time{},
		MaxAge:   -1,
		HttpOnly: true,
	}, nil)

	return nil
}

// Check if the supplied passphrase matches the expected hash using the salt.
func VerifyHash(supplied, expected, salt []byte) bool {
	hashed := Hash(supplied, salt)
	return hmac.Equal(expected, hashed)
}

// Hash the given passphrase using the salt provided.
func Hash(pass []byte, salt []byte) []byte {
	hash, _ := scrypt.Key(pass, salt, 2<<Env.HashCost, 8, 1, 32)
	return hash
}

// Create a new hash and random salt from the supplied password.
func NewHash(pass []byte) (hash, salt []byte) {
	salt = make([]byte, 16)
	rand.Read(salt)
	hash = Hash([]byte(pass), salt)
	return
}

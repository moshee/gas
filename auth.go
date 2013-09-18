package gas

import (
	"code.google.com/p/go.crypto/scrypt"
	"crypto/md5"
	"crypto/rand"
	"crypto/subtle"
	//	"database/sql"
	"encoding/base64"
	"fmt"
	"net/http"
	//	"os"
	"time"
)

var (
	max_age = 7 * 24 * time.Hour
	// HashCost is the cost passed into the scrypt hash function. It is
	// represented as the power of 2 (aka HashCost=9 means 2<<9 iterations).
	// It should be set as desired in the main() function of the importing
	// client. A value of 13 (the default) is a good number to start with,
	// and should be increased as hardware gets faster (see
	// http://www.tarsnap.com/scrypt.html for more info)
	HashCost uint = 13
)

const sessid_len = 64

// Describes a secure session to be stored temporarily or long-term.
type Session struct {
	Name    string
	Sessid  []byte
	Salt    []byte
	Expires time.Time
	Who     string
}

func parse_sessid(sessid string) ([]byte, []byte, error) {
	p, err := base64.StdEncoding.DecodeString(sessid)
	if err != nil {
		return nil, nil, err
	}
	return p[:sessid_len], p[sessid_len:], nil
}

// A Store is the generalized interface used to store sessions. Any type that
// implements this interface can be used to store sessions.
// TODO: implement a file-based store
type Store interface {
	Get(string) (*Session, error)
	Add([]byte, []byte, time.Time, string) error
	Touch(string) error
	Drop(string) error
}

// A session store that stores sessions in the database.
type DBStore struct {
	table string
}

func NewDBStore(table string) (Store, error) {
	_, err := DB.Exec("CREATE TABLE IF NOT EXISTS " + table +
		" ( name bytea UNIQUE NOT NULL, sessid bytea UNIQUE NOT NULL, salt bytea NOT NULL, expires timestamp with time zone, who integer references " + UsersTable + "(id) )")
	if err != nil {
		return nil, err
	}
	return &DBStore{table}, nil
}

func (self *DBStore) Get(sessid string) (*Session, error) {
	id, name, err := parse_sessid(sessid)
	if err != nil {
		return nil, err
	}
	session := new(Session)
	if err := SelectRow(session, "SELECT s.name, s.sessid, s.salt, s.expires, u.name FROM "+self.table+" s, users u WHERE s.name=$1 AND s.who = u.id", name); err != nil {
		return nil, err
	}

	if !VerifyHash(id, session.Sessid, session.Salt) {
		return nil, fmt.Errorf("(*DBStore).Get: invalid session id")
	}

	return session, nil
}

func (self *DBStore) Add(name, sessid []byte, expires time.Time, username string) error {
	hash, salt, err := NewHash(sessid)
	if err != nil {
		return err
	}
	_, err = DB.Exec("INSERT INTO "+self.table+" VALUES ( $1, $2, $3, $4, (SELECT id FROM "+UsersTable+" WHERE name = $5) )", name, hash, salt, expires, username)
	return err
}

// TODO: fix this
func (self *DBStore) Touch(sessid string) error {
	session, err := self.Get(sessid)
	if err != nil {
		return err
	}
	_, err = DB.Exec("UPDATE "+self.table+" SET expires=$1 WHERE sessid=$2", time.Now().Add(max_age), session.Sessid)
	return err
}

func (self *DBStore) Drop(sessid string) error {
	_, name, err := parse_sessid(sessid)
	if err != nil {
		return err
	}
	_, err = DB.Exec("DELETE FROM "+self.table+" WHERE name=$1", name)
	return err
}

/*
type FileStore struct {
	dir *os.File
}

func NewFileStore(path string) (Store, error) {
	dir, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	fi, err := dir.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("NewFileStore: '%s' is not a directory", path)
	}
	return &FileStore{dir}, nil
}

func (self *FileStore) Verify(sessid string) error {

}

func (self *FileStore) Add(sessid string) error {

}

func (self *FileStore) Drop(sessid string) error {

}
*/

func NewSession(store Store, who string) (id64 string, err error) {
	now := time.Now()
	session_name := md5.New()
	session_name.Write([]byte(now.String()))

	session_salt := make([]byte, 4)
	rand.Read(session_salt)
	session_name.Write(session_salt)

	name := session_name.Sum(nil)

	sessid := make([]byte, sessid_len)
	_, err = rand.Read(sessid)
	if err != nil {
		return "", err
	}

	err = store.Add(name, sessid, now.Add(max_age), who)
	id64 = base64.StdEncoding.EncodeToString(append(sessid, name...))
	return
}

// Provides facilities to store and use sessions.
type CookieAuth struct {
	path   string
	domain string
	store  Store
}

func NewCookieAuth(path, domain string, store Store) *CookieAuth {
	return &CookieAuth{path, domain, store}
}

func (self *CookieAuth) new_session(who string) (*http.Cookie, error) {
	sessid, err := NewSession(self.store, who)
	if err != nil {
		return nil, err
	}

	age := 7 * 24 * time.Hour

	cookie := &http.Cookie{
		Name:     "s",
		Value:    sessid,
		Path:     self.path,
		Domain:   self.domain,
		MaxAge:   int(age / time.Second),
		HttpOnly: true,
	}

	return cookie, nil
}

// Returns true if the user (identified by the request context) is logged in,
// false otherwise.
func (self *CookieAuth) Session(g *Gas) *Session {
	cookie, err := g.Cookie("s")
	if err != nil {
		return nil
	}

	session, err := self.store.Get(cookie.Value)

	if err != nil || time.Now().After(session.Expires) {
		Log(Warning, "(*CookieAuth).Session: %v", err)
		self.SignOut(g)
		return nil
	}
	return session
}

// Signs the user in by creating a new session and setting a cookie on the
// client.
func (self *CookieAuth) SignIn(g *Gas) error {
	if self.Session(g) != nil {
		return nil
	}
	user := g.FormValue("user")
	if len(user) == 0 {
		return fmt.Errorf("No username supplied")
	}
	if err := VerifyPass(user, g.FormValue("pass")); err != nil {
		return err
	}

	cookie, err := self.new_session(user)
	if err != nil {
		return err
	}
	g.SetCookie(cookie)
	return nil
}

// Signs the user out, destroying the associated session.
func (self *CookieAuth) SignOut(g *Gas) {
	cookie, err := g.Cookie("s")
	if err != nil {
		return
	}
	self.store.Drop(cookie.Value)
	g.SetCookie(&http.Cookie{Name: "s", Value: "deleted", MaxAge: -1})
}

// The name of the table used to store users.
// TODO: this is a really lame way to do this. Needs a more civilized API.
var UsersTable = "users"

// Add a user to the database.
func NewUser(user, pass string) error {
	hash, salt, err := NewHash([]byte(pass))
	if err != nil {
		return err
	}
	_, err = DB.Exec("INSERT INTO "+UsersTable+" ( name, pass, salt ) VALUES ( $1, $2, $3 )", user, hash, salt)

	return err
}

// Check if the supplied passphrase matches the expected hash using the salt.
func VerifyHash(supplied, expected, salt []byte) bool {
	//Log(Notice, "verifying '%s' : '%s'", user, pass)
	hashed := Hash(supplied, salt)
	return subtle.ConstantTimeCompare(expected, hashed) == 1
	//	return fmt.Errorf("Invalid username or password: got hash %v, expected %v (salt %v)", hashed, stored_pass, stored_salt)
}

// Hash the given passphrase using the salt provided.
func Hash(pass []byte, salt []byte) []byte {
	// TODO: magic numbers
	hash, _ := scrypt.Key(pass, salt, 2<<HashCost, 8, 1, 32)
	return hash
}

// Create a new hash and random salt from the supplied password.
func NewHash(pass []byte) (hash, salt []byte, err error) {
	salt = make([]byte, 16)
	rand.Read(salt)
	hash = Hash([]byte(pass), salt)
	return
}

func VerifyPass(user, pass string) error {
	var (
		stored_pass []byte
		stored_salt []byte
	)
	row := DB.QueryRow("SELECT pass, salt FROM "+UsersTable+" WHERE name = $1", user)
	if err := row.Scan(&stored_pass, &stored_salt); err != nil {
		return err
	}
	if !VerifyHash([]byte(pass), stored_pass, stored_salt) {
		return fmt.Errorf("Invalid username or password.")
	}
	return nil
}

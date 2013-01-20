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
	Add([]byte, []byte, time.Time) error
	Touch(string) error
	Drop(string) error
}

// A session store that stores sessions in the database.
type DBStore struct {
	table string
}

func NewDBStore(table string) (Store, error) {
	_, err := DB.Exec("CREATE TABLE IF NOT EXISTS " + table + " ( name bytea UNIQUE NOT NULL, sessid bytea UNIQUE NOT NULL, salt bytea NOT NULL, expires timestamp with time zone )")
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
	if err := SelectRow(session, "SELECT * FROM "+self.table+" WHERE name=$1", name); err != nil {
		return nil, err
	}

	if !VerifyHash(id, session.Sessid, session.Salt) {
		return nil, fmt.Errorf("(*DBStore).Get: invalid session id")
	}

	return session, nil
}

func (self *DBStore) Add(name, sessid []byte, expires time.Time) error {
	hash, salt, err := NewHash(sessid)
	if err != nil {
		return err
	}
	_, err = DB.Exec("INSERT INTO "+self.table+" VALUES ( $1, $2, $3, $4 )", name, hash, salt, expires)
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

// Provides facilities to store and use sessions.
type CookieAuth struct {
	path   string
	domain string
	store  Store
}

func NewCookieAuth(path, domain string, store Store) *CookieAuth {
	return &CookieAuth{path, domain, store}
}

func (self *CookieAuth) new_session(name []byte) (*http.Cookie, error) {
	sessid := make([]byte, sessid_len)
	_, err := rand.Read(sessid)
	if err != nil {
		return nil, err
	}

	age := 7 * 24 * time.Hour

	cookie := &http.Cookie{
		Name: "s",
		Value:    base64.StdEncoding.EncodeToString(append(sessid, name...)),
		Path:     self.path,
		Domain:   self.domain,
		MaxAge:   int(age / time.Second),
		HttpOnly: true,
	}

	self.store.Add(name, sessid, time.Now().Add(age))

	return cookie, nil
}

// Returns true if the user (identified by the request context) is logged in,
// false otherwise.
func (self *CookieAuth) SignedIn(g *Gas) bool {
	cookie, err := g.Cookie("s")
	if err != nil {
		return false
	}

	session, err := self.store.Get(cookie.Value)

	if err != nil || time.Now().After(session.Expires) {
		self.SignOut(g)
		return false
	}
	return true
}

// Signs the user in by creating a new session and setting a cookie on the
// client.
func (self *CookieAuth) SignIn(g *Gas) error {
	if self.SignedIn(g) {
		return nil
	}
	user := g.FormValue("user")
	if len(user) == 0 {
		return fmt.Errorf("No username supplied")
	}
	pass := []byte(g.FormValue("pass"))
	var (
		stored_pass []byte
		stored_salt []byte
	)
	row := DB.QueryRow("SELECT pass, salt FROM "+UsersTable+" WHERE name = $1", user)
	if err := row.Scan(&stored_pass, &stored_salt); err != nil {
		return err
	}
	if !VerifyHash(pass, stored_pass, stored_salt) {
		return fmt.Errorf("Invalid username or password")
	}

	session_name := md5.New()
	session_name.Write([]byte(time.Now().String()))
	cookie, err := self.new_session(session_name.Sum(nil))
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

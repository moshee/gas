package gas

import (
	"code.google.com/p/go.crypto/scrypt"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	//	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	//	"os"
	"strings"
	"time"
)

var (
	// MaxCookieAge is the maximum age sent in the  Set-Cookie header when a
	// user logs in.
	MaxCookieAge = 7 * 24 * time.Hour

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

func parseSessid(sessid string) ([]byte, []byte, error) {
	p, err := base64.StdEncoding.DecodeString(sessid)
	if err != nil {
		return nil, nil, err
	}
	return p[:sessid_len], p[sessid_len:], nil
}

// A Authenticator is the generalized interface used to auth sessions. Any type
// that implements this interface can be used to auth sessions.
type Authenticator interface {
	// Methods for CRUDing sessions.
	CreateSession(name, id []byte, expires time.Time, username string) error
	ReadSession(name, id []byte) (*Session, error)
	UpdateSession(name, id []byte) error
	DeleteSession(name, id []byte) error

	// Return the user's password hash and salt for password checking.
	UserAuthData(username string) (pass, salt []byte, err error)

	// Return an object that implements the User interface for authorization
	// checking.
	User(name string) (User, error)
}

// All of the generic things a user should be able to do.
type User interface {
	Allowed(privileges interface{}) bool
}

type dbStore struct {
	table string
}

// A session auth that stores sessions in the database.
func NewDBStore(table string) (Authenticator, error) {
	_, err := DB.Exec("CREATE TABLE IF NOT EXISTS " + table +
		" ( name bytea UNIQUE NOT NULL, sessid bytea UNIQUE NOT NULL, salt bytea NOT NULL, expires timestamp with time zone, who integer references " + UsersTable + "(id) )")
	if err != nil {
		return nil, err
	}
	return &dbStore{table}, nil
}

// Create a new session.
func (self *dbStore) CreateSession(name, sessid []byte, expires time.Time, username string) error {
	hash, salt, err := NewHash(sessid)
	if err != nil {
		return err
	}
	_, err = DB.Exec("INSERT INTO "+self.table+" VALUES ( $1, $2, $3, $4, (SELECT id FROM "+UsersTable+" WHERE name = $5) )", name, hash, salt, expires, username)
	return err
}

// Return a stored session. If no session was found—indicating not logged in—ReadSession should return a nil session, not an error.
func (self *dbStore) ReadSession(id, name []byte) (*Session, error) {
	session := new(Session)
	if err := QueryRow(session, "SELECT s.name, s.sessid, s.salt, s.expires, u.name FROM "+self.table+" s, users u WHERE s.name=$1 AND s.who = u.id", name); err != nil {
		return nil, err
	}

	if !VerifyHash(id, session.Sessid, session.Salt) {
		return nil, fmt.Errorf("(*dbStore).ReadSession: invalid session id")
	}

	return session, nil
}

// TODO: fix this
func (self *dbStore) UpdateSession(id, name []byte) error {
	session, err := self.ReadSession(id, name)
	if err != nil {
		return err
	}
	_, err = DB.Exec("UPDATE "+self.table+" SET expires=$1 WHERE sessid=$2", time.Now().Add(MaxCookieAge), session.Sessid)
	return err
}

func (self *dbStore) DeleteSession(id, name []byte) error {
	_, err := DB.Exec("DELETE FROM "+self.table+" WHERE name=$1", name)
	return err
}

func (self *dbStore) UserAuthData(username string) (pass, salt []byte, err error) {
	row := DB.QueryRow("SELECT pass, salt FROM "+UsersTable+" WHERE name = $1", username)
	err = row.Scan(&pass, &salt)
	return
}

// TODO: this (or just get rid of this whole thing)
func (self *dbStore) User(name string) (User, error) {
	return nil, nil
}

func NewSession(auth Authenticator, who string) (id64 string, err error) {
	now := time.Now()
	session_name := sha256.New()
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

	err = auth.CreateSession(name, sessid, now.Add(MaxCookieAge), who)
	id64 = base64.StdEncoding.EncodeToString(append(sessid, name...))
	return
}

// Provides facilities to auth and use sessions.
type cookieAuth struct {
	auth Authenticator
}

var (
	cookies              *cookieAuth
	errCookiesNotEnabled = errors.New("gas: cookies: cookies have not been enabled")
	errBadPassword       = errors.New("Invalid username or password.")
)

func UseCookies(auth Authenticator) {
	cookies = &cookieAuth{auth}
}

// Returns true if the user (identified by the request context) is logged in,
// false otherwise.
func (g *Gas) Session() (*Session, error) {
	if cookies == nil {
		return nil, errCookiesNotEnabled
	}

	cookie, err := g.Cookie("s")
	if err != nil {
		return nil, err
	}

	id, name, err := parseSessid(cookie.Value)
	if err != nil {
		return nil, err
	}

	session, err := cookies.auth.ReadSession(id, name)

	if err != nil || time.Now().After(session.Expires) {
		Log(Warning, "(*cookieAuth).Session: %v", err)
		g.SignOut()
		return nil, err
	}
	return session, nil
}

// Signs the user in by creating a new session and setting a cookie on the
// client.
func (g *Gas) SignIn() error {
	if cookies == nil {
		return errCookiesNotEnabled
	}

	// already signed in?
	sess, err := g.Session()
	if err != nil {
		return err
	}
	if sess != nil {
		cookie, err := g.Cookie("s")
		if err != nil {
			return err
		}

		id, name, err := parseSessid(cookie.Value)
		if err != nil {
			return err
		}

		if err := cookies.auth.UpdateSession(id, name); err != nil {
			return err
		}

		g.User, err = cookies.auth.User(sess.Who)
		if err != nil {
			return err
		}

		return nil
	}

	user := g.FormValue("user")
	if len(user) == 0 {
		return fmt.Errorf("No username supplied")
	}

	good, err := VerifyPass(user, g.FormValue("pass"))
	if err != nil {
		return err
	}

	if !good {
		return errBadPassword
	}

	sessid, err := NewSession(cookies.auth, user)
	if err != nil {
		return err
	}

	cookie := &http.Cookie{
		Name:     "s",
		Value:    sessid,
		Path:     g.URL.Path,
		Domain:   strings.SplitN(g.Host, ":", 1)[0],
		MaxAge:   int(MaxCookieAge / time.Second),
		HttpOnly: true,
	}

	g.SetCookie(cookie)

	g.User, err = cookies.auth.User(user)
	if err != nil {
		return err
	}

	return nil
}

// Signs the user out, destroying the associated session and cookie.
func (g *Gas) SignOut() error {
	cookie, err := g.Cookie("s")
	if err != nil {
		return err
	}

	id, name, err := parseSessid(cookie.Value)
	if err != nil {
		return err
	}

	if err := cookies.auth.DeleteSession(id, name); err != nil {
		return err
	}

	g.SetCookie(&http.Cookie{Name: "s", Value: "deleted", MaxAge: -1})
	return nil
}

// The name of the table used to auth users.
// TODO: this is a really lame way to do this. Needs a more civilized API.
var UsersTable = "users"

// Add a user to the database.
// TODO: deprecate this
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
	hashed := Hash(supplied, salt)
	return subtle.ConstantTimeCompare(expected, hashed) == 1
}

// Hash the given passphrase using the salt provided.
func Hash(pass []byte, salt []byte) []byte {
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

func VerifyPass(user, pass string) (bool, error) {
	storedPass, storedSalt, err := cookies.auth.UserAuthData(user)
	if err != nil {
		return false, err
	}

	if !VerifyHash([]byte(pass), storedPass, storedSalt) {
		return false, nil
	}
	return true, nil
}

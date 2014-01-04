package gas

// TODO: rework the login logic from the ground up

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"code.google.com/p/go.crypto/scrypt"
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

const (
	sessidLen    = 64
	sessionTable = "gas_sessions"
)

// Describes a secure session to be stored temporarily or long-term.
type Session struct {
	Id       []byte
	Expires  time.Time
	Username string
}

func parseSessid(sessid string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(sessid)
}

type SessionCreator interface {
	CreateSession(id []byte, expires time.Time, username string) error
}

type SessionReader interface {
	ReadSession(id []byte) (*Session, error)
}

type SessionUpdater interface {
	UpdateSession(id []byte) error
}

type SessionDeleter interface {
	DeleteSession(id []byte) error
}

// A Authenticator is the generalized interface used to auth sessions. Any type
// that implements this interface can be used to auth sessions.
type User interface {
	// Return the user's password hash and salt for password checking.
	Secrets() (pass, salt []byte, err error)
	Name() string
	Allowed(privileges interface{}) bool
}

type user struct {
	User
}

func (u user) CreateSession(id []byte, expires time.Time, username string) error {
	if s, ok := u.User.(SessionCreator); ok {
		return s.CreateSession(id, expires, username)
	}

	_, err := DB.Exec("INSERT INTO "+sessionTable+" VALUES ( $1, $2, $3 )",
		id, expires, username)

	return err
}

func (u user) ReadSession(id []byte) (*Session, error) {
	if s, ok := u.User.(SessionReader); ok {
		return s.ReadSession(id)
	}

	sess := new(Session)
	err := Query(sess, "SELECT * FROM "+sessionTable+" WHERE id = $1", id)
	return sess, err
}

func (u user) UpdateSession(id []byte) error {
	if s, ok := u.User.(SessionUpdater); ok {
		return s.UpdateSession(id)
	}

	_, err := DB.Exec("UPDATE " + sessionTable + " SET expires = now() + '7d'")
	return err
}

func (u user) DeleteSession(id []byte) error {
	if s, ok := u.User.(SessionDeleter); ok {
		return s.DeleteSession(id)
	}

	_, err := DB.Exec("DELETE FROM "+sessionTable+" WHERE id = $1", id)
	return err
}

func NewSession(u User) (id64 string, err error) {
	sessid := make([]byte, sessidLen)
	_, err = rand.Read(sessid)
	if err != nil {
		return "", err
	}

	err = user.CreateSession(sessid, time.Now().Add(MaxCookieAge), user.Name())
	id64 = base64.StdEncoding.EncodeToString(sessid)
	return
}

var (
	errCookiesNotEnabled = errors.New("gas: cookies: cookies have not been enabled")
	errNoUser            = errors.New("gas: no user has been provided")
	errBadPassword       = errors.New("Invalid username or password.")
	errCookieExpired     = errors.New("Your session has expired. Please log in again.")
)

func (g *Gas) SetUser(u User) {
	g.user = user{u}
}

func (g *Gas) User() User {
	return g.user.User
}

// Returns true if the user (identified by the request context) is logged in,
// false otherwise.
func (g *Gas) Session() (*Session, error) {
	if g.user.User == nil {
		return errNoUser
	}

	// error here would be cookie not present (this is not an error)
	cookie, err := g.Cookie("s")
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		} else {
			return nil, err
		}
	}

	id, err := parseSessid(cookie.Value)
	if err != nil {
		// this means invalid session
		g.SignOut()
		return nil, nil
	}

	session, err := g.user.ReadSession(id)

	if err != nil {
		if err == sql.ErrNoRows {
			g.SignOut()
			return nil, nil
		} else {
			return nil, err
		}
	}

	if session != nil && time.Now().After(session.Expires) {
		g.SignOut()
		return nil, errCookieExpired
	}

	return session, nil
}

// Signs the user in by creating a new session and setting a cookie on the
// client.
func (g *Gas) SignIn() error {
	if g.user.User == nil {
		return errNoUser
	}

	// already signed in?
	sess, _ := g.Session()
	if sess != nil {
		cookie, err := g.Cookie("s")
		if err != nil {
			return err
		}

		id, err := parseSessid(cookie.Value)
		if err != nil {
			return err
		}

		if err := g.user.UpdateSession(id); err != nil {
			return err
		}

		return nil
	}

	good, err := VerifyPass(user, g.FormValue("pass"))

	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if !good {
		return errBadPassword
	}

	sessid, err := NewSession(cookies.auth, user)
	if err != nil {
		return err
	}

	// TODO: determine if setting the path to / is always what we want. If it's
	// set to anything other than /, then only requests to that path will
	// include the cookie in the header (the browser restricts this).
	cookie := &http.Cookie{
		Name:     "s",
		Value:    sessid,
		Path:     "/",
		MaxAge:   int(MaxCookieAge / time.Second),
		HttpOnly: true,
	}

	g.SetCookie(cookie)

	return nil
}

// Signs the user out, destroying the associated session and cookie.
func (g *Gas) SignOut() error {
	cookie, err := g.Cookie("s")
	if err != nil {
		return err
	}

	id, err := parseSessid(cookie.Value)
	if err != nil {
		return err
	}

	if g.user.User == nil {
		return errNoUser
	}
	g.user.DeleteSession(id)

	g.SetCookie(&http.Cookie{
		Name:     "s",
		Path:     "/",
		Value:    "",
		Expires:  time.Time{},
		HttpOnly: true,
	})
	return nil
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

func VerifyPass(u User, pass string) (bool, error) {
	storedPass, storedSalt, err := u.Secrets()
	if err != nil {
		return false, err
	}

	return VerifyHash([]byte(pass), storedPass, storedSalt), nil
}

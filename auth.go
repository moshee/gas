package gas

import (
	"crypto/hmac"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"code.google.com/p/go.crypto/scrypt"
)

var (
	errNoUser        = errors.New("gas: no user has been provided")
	errBadPassword   = errors.New("Invalid username or password.")
	errCookieExpired = errors.New("Your session has expired. Please log in again.")
	errBadMac        = errors.New("HMAC digests don't match")
	hmacKeys         [][]byte
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

func createSession(id []byte, expires time.Time, username string) error {
	_, err := DB.Exec("INSERT INTO "+Env.SessTable+" VALUES ( $1, $2, $3 )",
		id, expires, username)

	return err
}

func readSession(id []byte) (*Session, error) {
	sess := new(Session)
	err := Query(sess, "SELECT * FROM "+Env.SessTable+" WHERE id = $1", id)
	return sess, err
}

func updateSession(id []byte) error {
	_, err := DB.Exec("UPDATE " + Env.SessTable + " SET expires = now() + '7d'")
	return err
}

func deleteSession(id []byte) error {
	_, err := DB.Exec("DELETE FROM "+Env.SessTable+" WHERE id = $1", id)
	return err
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

	g.session, err = readSession(id)

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

		if err := updateSession(id); err != nil {
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
	err = createSession(sessid, time.Now().Add(Env.MaxCookieAge), username)
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

	if err := deleteSession(id); err != nil && err != sql.ErrNoRows {
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

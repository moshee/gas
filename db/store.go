package db

import (
	"time"

	"github.com/moshee/gas/auth"
)

func NewStore(table string) (*Store, error) {
	_, err := DB.Exec("CREATE TABLE IF NOT EXISTS " + table +
		" ( id bytea, expires timestamptz, username text )")
	if err != nil {
		return nil, err
	}
	return &Store{table}, nil
}

// Store is a session store that stores sessions in a database table.
type Store struct {
	// The name of the table.
	table string
}

func (s *Store) Create(id []byte, expires time.Time, username string) error {
	_, err := DB.Exec("INSERT INTO "+s.table+" VALUES ( $1, $2, $3 )",
		id, expires, username)

	return err
}

func (s *Store) Read(id []byte) (*auth.Session, error) {
	sess := new(auth.Session)
	err := Query(sess, "SELECT * FROM "+s.table+" WHERE id = $1", id)
	return sess, err
}

func (s *Store) Update(id []byte) error {
	exp := time.Now().Add(auth.Env.MaxCookieAge)
	_, err := DB.Exec("UPDATE "+s.table+" SET expires = $1", exp)
	return err
}

func (s *Store) Delete(id []byte) error {
	_, err := DB.Exec("DELETE FROM "+s.table+" WHERE id = $1", id)
	return err
}

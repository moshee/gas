package gas

import "errors"

type User struct {
	Name string
	Pass []byte
	Salt []byte
}

func (u *User) Name() string {
	return u.Name
}

func (u *User) Secrets() ([]byte, []byte, error) {
	if u == nil {
		return nil, nil, errors.New("nil user")
	}
	return u.Pass, u.Salt, nil
}

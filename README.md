Dumb web toolkit made for experimentation and learning that I end up using for all of my websites.

No reason anyone should use this.

#### Features and unfeatures

##### `package gas`: router and utilities

- Path matcher with named capture groups (no regex)*
- Post form unmarshaling*
- Defines handler and middleware structure
- Environment variable configuration*
- Signal capturing
- User-Agent and Accept header helpers
- TLS

##### `package gas/auth`: session logic

- Pluggable User interface for transparent login/logout
- Session handling with pluggable store
- Secure cookies
- Password KDF (via scrypt[1]) and verification
- All the crypto stuff is totally unverified™ and probably broken

[1]: http://www.tarsnap.com/scrypt.html

##### `package gas/db`: SQL database wrapper

- Support postgres only (for now?)
- Use raw SQL commands
- Unmarshal row into struct, recursively handling embedded types

##### `package gas/out`: output generators

- HTML templates*
	- Arbitrarily nested layouts
	- Extra utility template funcs for Markdown, etc.
	- Partial renders for pjax-like behavior
	- gzip
	- Error page redirection
	- Established directory structure
- JSON marshaling
- Page redirection and rerouting via flash message cookies

\* = needs to be moved to a new package

#### In the works

- Remote process control and monitoring
	- Start, stop
	- Automatic start
	- Send and receive messages/signals
	- Resource usage monitoring
	- Analytics

```
moshee@deimos(~/downloads) ⚡ gas status
  NAME                PID    PORT   UPTIME
× airlift (disabled)  -      -      -
✓ index               21247  60607  7 days, 23:05:21.75
✓ manga               21244  60608  7 days, 23:05:21.78
✓ dl                  21271  60609  7 days, 23:05:21.69
✓ pls                 21246  60610  7 days, 23:05:21.76
× landing (disabled)  -      -      -
moshee@deimos(~/downloads) ⚡
```

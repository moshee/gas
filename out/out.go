package out

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"ktkr.us/pkg/gas"
)

var (
	// ErrNoReroute is returned from Recover() if there was no reroute info
	// found in the cookie.
	ErrNoReroute = errors.New("reroute: no cookie found")
)

type jsonOutputter struct {
	data interface{}
}

func (o jsonOutputter) Output(code int, g *gas.Gas) {
	h := g.Header()
	if _, foundType := h["Content-Type"]; !foundType {
		h.Set("Content-Type", "application/json; charset=utf-8")
	}
	g.WriteHeader(code)
	json.NewEncoder(g).Encode(o.data)
}

// JSON returns an outputter that returns the json encoding of the argument.
func JSON(data interface{}) gas.Outputter {
	return jsonOutputter{data}
}

type redirectOutputter string

func (o redirectOutputter) Output(code int, g *gas.Gas) {
	http.Redirect(g, g.Request, string(o), code)
}

// Redirect returns an outputter that redirects the client to the given path.
func Redirect(path string) gas.Outputter {
	return redirectOutputter(path)
}

// CheckReroute is a middleware handler that will check for and deal with
// reroute cookies
func CheckReroute(g *gas.Gas) (int, gas.Outputter) {
	reroute, err := g.Cookie("_reroute")
	if reroute != nil {
		if err == nil {
			blob, err := base64.StdEncoding.DecodeString(reroute.Value)

			if err == nil {
				g.SetData("_reroute", blob)
			} else {
				log.Println("gas: dispatch reroute:", err)
			}
		} else {
			log.Println("gas: reroute cookie:", err)
		}

		// Empty the cookie out and toss it back
		reroute.Value = ""
		reroute.MaxAge = -1

		g.SetCookie(reroute)
	}

	return g.Continue()
}

// Recover will try to recover the reroute info stored in the cookie and decode
// it into dest. If there is no reroute cookie, an error is returned.
func Recover(g *gas.Gas, dest interface{}) error {
	blob := g.Data("_reroute")
	if blob == nil {
		return ErrNoReroute
	}
	dec := gob.NewDecoder(bytes.NewReader(blob.([]byte)))
	return dec.Decode(dest)
}

type rerouteOutputter struct {
	path string
	data interface{}
}

func (o *rerouteOutputter) Output(code int, g *gas.Gas) {
	var cookieVal string

	if o.data != nil {
		buf := new(bytes.Buffer)
		enc := gob.NewEncoder(buf)
		err := enc.Encode(o.data)

		// TODO: do we want to ignore an encode error here?
		if err != nil {
			Error(g, err).Output(code, g)
			return
		}

		cookieVal = base64.StdEncoding.EncodeToString(buf.Bytes())
	}

	g.SetCookie(&http.Cookie{
		Path:     "/",
		Name:     "_reroute",
		Value:    cookieVal,
		Expires:  time.Now().Add(60 * time.Second),
		HttpOnly: true,
	})

	redirectOutputter(o.path).Output(code, g)
}

// Reroute will perform a redirect, but first place a cookie on the client
// containing an encoding/gob blob encoded from the data passed in. The
// recieving handler should then check for the RerouteInfo on the request, and
// handle the special case if necessary.
func Reroute(path string, data interface{}) gas.Outputter {
	return &rerouteOutputter{path, data}
}

// Error returns an Outputter that will serve up an error page from
// templates/errors. Templates in that directory should be defined under the
// HTTP status code they correspond to, e.g.
//
//     {{ define "404" }} ... {{ end }}
//
// will provide the template for a 404 error. The template will be rendered
// with a *ErrorInfo as the data binding.
func Error(g *gas.Gas, err error) gas.Outputter {
	e := ""
	if err != nil {
		e = err.Error()
	}
	return &ErrorInfo{
		Err:  e,
		Path: g.URL.Path,
		Host: g.Host,
	}
}

// ErrorInfo represents an error that occurred in a particular request handler.
type ErrorInfo struct {
	Err  string
	Path string
	Host string
}

// Output satisfies the gas.Outputter interface.
func (o *ErrorInfo) Output(code int, g *gas.Gas) {
	s := strconv.Itoa(code)
	(&templateOutputter{templatePath{"errors", s}, o}).Output(code, g)
}

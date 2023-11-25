package main

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// AccessLog helps log request and response related data.
func AccessLog(path string, h http.Handler) http.Handler {
	ch := make(chan *strings.Builder, 1000)

	go func() {
		fp, err := os.Create(path)
		if err != nil {
			panic(err)
		}
		defer fp.Close()

		buf := bufio.NewWriter(fp)
		defer buf.Flush()

		for {
			p := <-ch

			buf.WriteString(p.String())
		}
	}()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := time.Now()

		wr := newWriter(w, t)

		h.ServeHTTP(wr, r)

		referrer := r.Referer()
		if referrer == "" {
			referrer = "-"
		}

		builder := strings.Builder{}

		builder.WriteString("time:")
		builder.WriteString(t.Format("02/Jan/2006:15:04:05 -0700"))

		builder.WriteString("\thost:")
		builder.WriteString(r.Host)

		builder.WriteString("\tforwardedfor:")
		builder.WriteString(r.Header.Get("X-Forwarded-For"))

		builder.WriteString("\treq:")
		builder.WriteString(r.Method + " " + r.RequestURI + " " + r.Proto)

		builder.WriteString("\tmethod:")
		builder.WriteString(r.Method)

		builder.WriteString("\turi:")
		builder.WriteString(r.RequestURI)

		builder.WriteString("\tstatus:")
		builder.WriteString(strconv.Itoa(wr.resStatus))

		builder.WriteString("\tsize:")
		builder.WriteString(strconv.Itoa(wr.resSize))

		builder.WriteString("\treferer:")
		builder.WriteString(referrer)

		builder.WriteString("\tua:")
		builder.WriteString(r.UserAgent())

		builder.WriteString("\treqtime:0.000")

		builder.WriteString("\truntime:-")

		// in seconds like 0.123
		builder.WriteString("\tapptime:")
		builder.WriteString(strconv.FormatFloat(time.Since(t).Seconds(), 'f', 3, 64))

		builder.WriteString("\tcache:-")

		builder.WriteString("\tvhost:")
		builder.WriteString(r.Host)

		builder.WriteString("\n")

		ch <- &builder
	})
}

// Acts as an adapter for http.ResponseWriter type to store request and
// response data.
type writer struct {
	http.ResponseWriter

	resStatus int
	resSize   int // bytes
}

func newWriter(w http.ResponseWriter, t time.Time) *writer {
	return &writer{
		ResponseWriter: w,
	}
}

// Overrides http.ResponseWriter type.
func (w *writer) WriteHeader(status int) {
	if w.resStatus == 0 {
		w.resStatus = status
		w.ResponseWriter.WriteHeader(status)
	}
}

// Overrides http.ResponseWriter type.
func (w *writer) Write(body []byte) (int, error) {
	if w.resStatus == 0 {
		w.WriteHeader(http.StatusOK)
	}

	var err error
	w.resSize, err = w.ResponseWriter.Write(body)

	return w.resSize, err
}

// Overrides http.Flusher type.
func (w *writer) Flush() {
	if fl, ok := w.ResponseWriter.(http.Flusher); ok {
		if w.resStatus == 0 {
			w.WriteHeader(http.StatusOK)
		}

		fl.Flush()
	}
}

// Overrides http.Hijacker type.
func (w *writer) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("the hijacker interface is not supported")
	}

	return hj.Hijack()
}

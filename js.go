package retracer

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"h12.me/errors"
)

type JSTracer struct {
	Timeout time.Duration
	Certs   CertPool
}

func (t *JSTracer) Trace(uri string, body []byte) (string, error) {
	redirectChan := make(chan string)
	errChan := make(chan error)

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == "GET" {
			if req.RequestURI == uri {
				w.Write(body)
			} else {
				switch strings.ToLower(path.Ext(req.RequestURI)) {
				case ".js", ".css", ".png", ".gif", ".jpg", ".jpeg":
					return
				}
				redirectChan <- req.RequestURI
			}
		} else if req.Method == "CONNECT" {
			host := req.URL.Host
			cli, err := hijack(w)
			if err != nil {
				errChan <- err
				return
			}
			defer cli.Close()
			if err := OK200(cli); err != nil {
				errChan <- err
				return
			}
			conn, err := fakeSecureConn(cli, trimPort(host), t.Certs)
			if err != nil {
				errChan <- err
				return
			}
			defer conn.Close()

			tlsReq, err := http.ReadRequest(bufio.NewReader(conn))
			if err != nil {
				errChan <- err
				return
			}
			redirectChan <- "https://" + req.RequestURI + tlsReq.RequestURI
		}
	}))
	defer proxy.Close()

	browser := exec.Command(
		"surf",
		"-bdfgikmnp",
		"-t", os.DevNull,
		uri)
	browser.Env = []string{
		"DISPLAY=" + os.Getenv("DISPLAY"),
		"http_proxy=" + proxy.URL,
	}
	defer func() {
		if browser.Process != nil {
			browser.Process.Kill()
		}
	}()

	go func() {
		if err := browser.Run(); err != nil {
			if _, ok := err.(*exec.ExitError); !ok {
				errChan <- errors.Wrap(err)
			}
		}
	}()
	select {
	case redirectURL := <-redirectChan:
		return redirectURL, nil
	case <-time.After(t.Timeout):
	case err := <-errChan:
		return "", err
	}
	return "", nil
}

func trimPort(hostPort string) string {
	host, _, _ := net.SplitHostPort(hostPort)
	return host
}

func isEOF(err error) bool {
	return err == io.EOF || err.Error() == "EOF" || err.Error() == "unexpected EOF"
}

func OK200(w io.Writer) error {
	_, err := w.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))
	return errors.Wrap(err)
}

func hijack(w http.ResponseWriter) (net.Conn, error) {
	hij, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("cannot hijack the ResponseWriter")
	}
	conn, _, err := hij.Hijack()
	if err != nil {
		return nil, errors.Wrap(err)
	}
	return conn, nil
}

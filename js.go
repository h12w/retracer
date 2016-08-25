package retracer

import (
	"bufio"
	"bytes"
	"io"
	"io/ioutil"
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

func isResource(uri string) bool {
	switch strings.ToLower(path.Ext(uri)) {
	case ".js", ".css", ".png", ".gif", ".jpg", ".jpeg":
		return true
	}
	return false
}

type fakeProxy struct {
	uri          string
	body         []byte
	certs        CertPool
	redirectChan chan string
	errChan      chan error
	proxy        *httptest.Server
}

func newProxy(uri string, body []byte, certs CertPool) *fakeProxy {
	fp := &fakeProxy{
		uri:          uri,
		body:         body,
		certs:        certs,
		redirectChan: make(chan string),
		errChan:      make(chan error),
	}

	fp.proxy = httptest.NewServer(http.HandlerFunc(fp.serve))
	return fp
}

func (p *fakeProxy) serveHTTP(w http.ResponseWriter, req *http.Request) {
	if req.RequestURI == p.uri {
		w.Write(p.body)
	} else {
		if !isResource(req.RequestURI) {
			p.redirectChan <- req.RequestURI
		}
	}
}

func (p *fakeProxy) serve(w http.ResponseWriter, req *http.Request) {
	if req.Method == "GET" {
		p.serveHTTP(w, req)
	} else if req.Method == "CONNECT" {
		host := req.URL.Host
		cli, err := hijack(w)
		if err != nil {
			p.errChan <- err
			return
		}
		defer cli.Close()
		if err := OK200(cli); err != nil {
			p.errChan <- err
			return
		}
		conn, err := fakeSecureConn(cli, trimPort(host), p.certs)
		if err != nil {
			p.errChan <- err
			return
		}
		defer conn.Close()

		tlsReq, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			p.errChan <- err
			return
		}
		requestURI := "https://" + trimPort(req.RequestURI) + tlsReq.RequestURI
		if requestURI == p.uri {
			resp := http.Response{
				Status:     http.StatusText(http.StatusFound),
				StatusCode: http.StatusFound,
				Proto:      "HTTP/1.1",
				ProtoMajor: 1,
				ProtoMinor: 1,
				Body:       ioutil.NopCloser(bytes.NewReader(p.body)),
				Close:      true,
			}
			if err := resp.Write(conn); err != nil {
				p.errChan <- err
			}
		} else {
			if !isResource(requestURI) {
				p.redirectChan <- requestURI
			}
		}
	}
}

func (t *JSTracer) Trace(uri string, body []byte) (string, error) {
	proxy := newProxy(uri, body, t.Certs)
	redirectChan := proxy.redirectChan
	errChan := proxy.errChan
	defer proxy.proxy.Close()

	browser := exec.Command(
		"surf",
		"-bdfgikmnp",
		"-t", os.DevNull,
		uri)
	browser.Env = []string{
		"DISPLAY=" + os.Getenv("DISPLAY"),
		"http_proxy=" + proxy.proxy.URL,
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

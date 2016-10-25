package retracer

import (
	"bufio"
	"bytes"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync/atomic"
	"time"

	"h12.me/errors"
)

type JSTracer struct {
	Timeout time.Duration
	Certs   CertPool
}

func (t *JSTracer) Trace(uri string, header http.Header, body []byte) (string, error) {
	if t.Timeout == 0 {
		t.Timeout = 10 * time.Second
	}
	proxy := newProxy(uri, header, body, t.Timeout, t.Certs)
	defer proxy.Close()

	browser, err := startBrowser(uri, proxy.URL())
	if err != nil {
		return "", nil
	}
	defer browser.Close()

	select {
	case <-time.After(t.Timeout):
	case redirectURL := <-proxy.RedirectURLChan():
		return redirectURL, nil
	case err := <-proxy.ErrChan():
		return "", err
	case err := <-errChan(browser.Wait):
		return "", err
	}
	return "", nil
}

type fakeProxy struct {
	uri          string
	header       http.Header
	body         []byte
	certs        CertPool
	timeout      time.Duration
	proxy        *httptest.Server
	redirectChan chan string
	errChan      chan error
	respondCount int32
}

func newProxy(uri string, header http.Header, body []byte, timeout time.Duration, certs CertPool) *fakeProxy {
	fp := &fakeProxy{
		uri:          uri,
		header:       header,
		body:         body,
		certs:        certs,
		timeout:      timeout,
		redirectChan: make(chan string),
		errChan:      make(chan error),
	}

	fp.proxy = httptest.NewServer(http.HandlerFunc(fp.serve))
	return fp
}

func (p *fakeProxy) URL() string {
	return p.proxy.URL
}

func (p *fakeProxy) RedirectURLChan() <-chan string {
	return p.redirectChan
}

func (p *fakeProxy) ErrChan() <-chan error {
	return p.errChan
}

func (p *fakeProxy) setError(err error) {
	select {
	case p.errChan <- err:
	default:
	}
}

func (p *fakeProxy) setRedirectURL(uri string) {
	select {
	case p.redirectChan <- uri:
	default:
	}
}

func (p *fakeProxy) Close() error {
	p.proxy.Close() // make should all serve goroutines have exited
	return nil
}

func (p *fakeProxy) serve(w http.ResponseWriter, req *http.Request) {
	if req.Method == "GET" {
		p.serveHTTP(w, req)
	} else if req.Method == "CONNECT" {
		p.serveHTTPS(w, req)
	}
}

func (p *fakeProxy) serveHTTP(w http.ResponseWriter, req *http.Request) {
	if atomic.AddInt32(&p.respondCount, 1) == 1 {
		for k, v := range p.header {
			w.Header()[k] = v
		}
		w.Write(p.body)
	} else {
		if !isResource(req.RequestURI, req.Header) {
			p.setRedirectURL(req.RequestURI)
		}
	}
}

func (p *fakeProxy) serveHTTPS(w http.ResponseWriter, req *http.Request) {
	cli, err := hijack(w)
	if err != nil {
		p.setError(err)
		return
	}
	defer cli.Close()
	if err := OK200(cli); err != nil {
		p.setError(err)
		return
	}
	conn, err := fakeSecureConn(cli, trimPort(req.URL.Host), p.certs)
	if err != nil {
		p.setError(err)
		return
	}
	defer conn.Close()

	tlsReq, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		p.setError(err)
		return
	}
	if atomic.AddInt32(&p.respondCount, 1) == 1 {
		resp := http.Response{
			Status:     http.StatusText(http.StatusFound),
			StatusCode: http.StatusFound,
			Proto:      "HTTP/1.1",
			ProtoMajor: 1,
			ProtoMinor: 1,
			Header:     p.header,
			Body:       ioutil.NopCloser(bytes.NewReader(p.body)),
			Close:      true,
		}
		if err := resp.Write(conn); err != nil {
			p.setError(err)
		}
	} else {
		requestURI := "https://" + req.RequestURI + tlsReq.RequestURI
		if !isResource(requestURI, tlsReq.Header) {
			p.setRedirectURL(requestURI)
		}
	}
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

func isResource(uri string, header http.Header) bool {
	switch strings.ToLower(path.Ext(uri)) {
	case ".js", ".css", ".png", ".gif", ".jpg", ".jpeg":
		return true
	}
	accept := header.Get("Accept")
	if !strings.Contains(accept, "text/html") {
		return true
	}
	return false
}

type browser struct {
	cmd *exec.Cmd
}

func startBrowser(uri, proxy string) (*browser, error) {
	cmd := exec.Command(
		"surf",
		"-bdfgikmnp",
		"-t", os.DevNull,
		uri)
	cmd.Env = []string{
		"DISPLAY=" + os.Getenv("DISPLAY"),
		"http_proxy=" + proxy,
	}
	return &browser{cmd: cmd}, cmd.Start()
}

func (b *browser) pid() int {
	if b.cmd.Process != nil {
		return b.cmd.Process.Pid
	}
	return 0
}

func (b *browser) Wait() error {
	err := b.cmd.Wait()
	if _, ok := err.(*exec.ExitError); !ok {
		return errors.Wrap(err)
	}
	return nil
}

func (b *browser) Close() error {
	if b.cmd.Process != nil {
		pid := b.pid()
		// try killing twice
		for i := 0; i < 2; i++ {
			if err := b.cmd.Process.Kill(); err != nil {
				if !strings.Contains(err.Error(), "already finished") {
					log.Printf("fail to kill surf %d: %s", pid, err.Error())
					return err
				}
			}
		}
	}
	return nil
}

func strChan(f func() string) chan string {
	ch := make(chan string)
	go func() {
		ch <- f()
	}()
	return ch
}

func errChan(f func() error) chan error {
	ch := make(chan error)
	go func() {
		ch <- f()
	}()
	return ch
}

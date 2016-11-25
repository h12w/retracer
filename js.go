package retracer

import (
	"bytes"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"h12.me/errors"
	"h12.me/mitm"
	"h12.me/uuid"
)

type JSTracer struct {
	Timeout time.Duration
	Certs   *mitm.CertPool
}

func (t *JSTracer) Trace(uri string, header http.Header, body []byte) (string, error) {
	body = neutralizeIFrame(body)
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
		return "", ErrJSRedirectionTimeout
	case redirectURL := <-proxy.RedirectURLChan():
		return redirectURL, nil
	case err := <-proxy.ErrChan():
		return "", err
	case err := <-errChan(browser.Wait):
		return "", err
	}
}
func neutralizeIFrame(body []byte) []byte {
	body = bytes.Replace(body, []byte("<iframe"), []byte("<div"), -1)
	body = bytes.Replace(body, []byte("/iframe>"), []byte("</div"), -1)
	return body
}

type fakeProxy struct {
	uri          string
	header       http.Header
	body         []byte
	certs        *mitm.CertPool
	timeout      time.Duration
	proxy        *httptest.Server
	redirectChan chan string
	errChan      chan error
	respondCount int32
}

func newProxy(uri string, header http.Header, body []byte, timeout time.Duration, certs *mitm.CertPool) *fakeProxy {
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
		err := p.certs.ServeHTTPS(w, req, p.serveHTTP)
		if err != nil {
			p.setError(errors.Wrap(err))
		}
	}
}

func (p *fakeProxy) serveHTTP(w http.ResponseWriter, req *http.Request) {
	if atomic.AddInt32(&p.respondCount, 1) == 1 {
		for k, v := range p.header {
			w.Header()[k] = v
		}
		w.Write(p.body)
	} else {
		if isJS(req.RequestURI) {
			jsResp, err := http.Get(req.RequestURI)
			if err != nil {
				p.setError(errors.Wrap(err))
				return
			}

			for k, v := range jsResp.Header {
				w.Header()[k] = v
			}
			w.WriteHeader(jsResp.StatusCode)
			if _, err := io.Copy(w, jsResp.Body); err != nil {
				p.setError(errors.Wrap(err))
			}
			jsResp.Body.Close()
			return
		}

		if isResource(req.RequestURI, req.Header) {
			return
		}

		p.setRedirectURL(req.RequestURI)
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

func isJS(uri string) bool {
	return strings.ToLower(path.Ext(uri)) == ".js"
}

func isResource(uri string, header http.Header) bool {
	switch strings.ToLower(path.Ext(uri)) {
	case ".css", ".png", ".gif", ".jpg", ".jpeg":
		return true
	}
	accept := header.Get("Accept")
	if !strings.Contains(accept, "text/html") {
		return true
	}
	return false
}

type browser struct {
	id  string
	cmd *exec.Cmd
}

func startBrowser(uri, proxy string) (*browser, error) {
	// id is for debugging only
	id, _ := uuid.NewTime(time.Now())
	cmd := exec.Command(
		"surf",
		"-bdfgikmnp",
		"-t", os.DevNull,
		uri,
		id.String(),
	)
	// set pgid so all child processes can be killed together
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = []string{
		"DISPLAY=" + os.Getenv("DISPLAY"),
		"http_proxy=" + proxy,
	}
	return &browser{id: id.String(), cmd: cmd}, cmd.Start()
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
	if b.cmd.Process == nil {
		log.Printf("cannot kill surf %s because it is not started", b.id)
		return nil
	}

	// kill -pgid (-pid)
	// https://medium.com/@felixge/killing-a-child-process-and-all-of-its-children-in-go-54079af94773#.g2krdc3ir
	if err := syscall.Kill(-b.cmd.Process.Pid, syscall.SIGKILL); err != nil {
		log.Printf("fail to kill surf %s (%d)", b.id, b.pid())
		return err
	}
	return nil
}

func forceKill(p *os.Process) error {
	if err := p.Kill(); err != nil {
		return err
	}
	for i := 0; processExists(p.Pid); i++ {
		if err := p.Kill(); err != nil {
			return err
		}
		time.Sleep(time.Second)
		if i > 10 {
			log.Printf("try to kill surf %d for the %d times", p.Pid, i)
		}
	}
	return nil
}

func processExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		// non-unix system
		return false
	}
	return nil == process.Signal(syscall.Signal(0))
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

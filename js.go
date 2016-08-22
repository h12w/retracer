package retracer

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"time"
)

type JSTracer struct {
	Timeout time.Duration
}

func (t JSTracer) Trace(uri string, body []byte) (string, error) {
	redirectChan := make(chan string)
	errChan := make(chan error)

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.RequestURI == uri {
			w.Write(body)
		} else {
			redirectChan <- req.RequestURI
			w.WriteHeader(http.StatusNotFound)
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
				errChan <- err
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

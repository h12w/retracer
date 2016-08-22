package retracer

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type Tracer struct {
	RoundTripper http.RoundTripper
	Header       http.Header
	Timeout      time.Duration
}

func (t *Tracer) Trace(uri string, callback func(string, *http.Response) error) error {
	for {
		req, err := http.NewRequest("GET", uri, nil)
		if err != nil {
			return err
		}
		if t.Header != nil {
			req.Header = t.Header
		}
		resp, err := t.RoundTripper.RoundTrip(req)
		if err != nil {
			callback(uri, &http.Response{
				Header: make(http.Header),
				Body:   ioutil.NopCloser(strings.NewReader("")),
			})
			return err
		}
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			resp.Body.Close()
			return err
		}
		resp.Body.Close()
		resp.Body = ioutil.NopCloser(bytes.NewReader(body))
		if err := callback(uri, resp); err != nil {
			return err
		}

		if shouldRedirect(resp.StatusCode) {
			if uri = resp.Header.Get("Location"); uri != "" {
				continue
			}
		} else if resp.StatusCode == http.StatusOK && couldJSRedirect(body) {
			location, err := JSTracer{Timeout: t.Timeout}.Trace(uri, body)
			if err != nil {
				return err
			}
			if location != "" {
				uri = location
				continue
			}
		}

		break
	}
	return nil
}

func shouldRedirect(statusCode int) bool {
	switch statusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect:
		return true
	}
	return false
}

var rxRedirect = regexp.MustCompile("(<script|http-equiv)")

func couldJSRedirect(body []byte) bool {
	return rxRedirect.Find(body) != nil
}

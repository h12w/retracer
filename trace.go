package retracer

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"regexp"
	"strings"
	"time"

	"h12.me/errors"
)

type Tracer struct {
	RoundTripper http.RoundTripper
	Header       http.Header
	Timeout      time.Duration
	Certs        CertPool
}

func (t *Tracer) Trace(uri string, callback func(string, *http.Response) error) error {
	for {
		req, err := http.NewRequest("GET", uri, nil)
		if err != nil {
			return errors.Wrap(err)
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
			return errors.Wrap(err)
		}
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			resp.Body.Close()
			return errors.Wrap(err)
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
			location, err := (&JSTracer{Timeout: t.Timeout, Certs: t.Certs}).Trace(uri, body)
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

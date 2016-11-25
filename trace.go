package retracer

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"net/http/cookiejar"

	"golang.org/x/net/publicsuffix"
	"h12.me/errors"
	"h12.me/mitm"
)

type Tracer struct {
	RoundTripper http.RoundTripper
	Header       http.Header
	Timeout      time.Duration
	Certs        *mitm.CertPool
}

func (t *Tracer) Trace(uri string, callback func(string, *http.Response) error) error {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return err
	}
	var lastReq *http.Request
	for {
		req, err := http.NewRequest("GET", uri, nil)
		if err != nil {
			return errors.Wrap(err)
		}

		// fill headers
		if t.Header != nil {
			for k, v := range t.Header {
				req.Header[k] = v
			}
		}
		for _, cookie := range jar.Cookies(req.URL) {
			req.AddCookie(cookie)
		}
		if lastReq != nil {
			if ref := refererForURL(lastReq.URL, req.URL); ref != "" {
				req.Header.Set("Referer", ref)
			}
		}
		lastReq = req

		// do the request
		resp, err := t.RoundTripper.RoundTrip(req)
		if err != nil {
			callback(uri, &http.Response{
				Header: make(http.Header),
				Body:   ioutil.NopCloser(strings.NewReader("")),
			})
			return errors.Wrap(err)
		}

		// handle response
		if rc := resp.Cookies(); len(rc) > 0 {
			jar.SetCookies(req.URL, rc)
		}
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			resp.Body.Close()
			return errors.Wrap(err)
		}
		resp.Body.Close()
		resp.Body = ioutil.NopCloser(bytes.NewReader(body))

		// callback
		if err := callback(uri, resp); err != nil {
			return err
		}

		// redirect
		if shouldRedirect(resp.StatusCode) {
			loc, err := resp.Location() // relative path will be expanded
			if err != nil {
				return err
			}
			uri = loc.String()
			continue
		} else if resp.StatusCode == http.StatusOK && couldJSRedirect(resp.Header, body) {
			location, err := (&JSTracer{Timeout: 5 * time.Second, Certs: t.Certs}).Trace(uri, resp.Header, body)
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

func couldJSRedirect(header http.Header, body []byte) bool {
	return header.Get("Refresh") != "" ||
		rxRedirect.Find(body) != nil
}

// refererForURL returns a referer without any authentication info or
// an empty string if lastReq scheme is https and newReq scheme is http.
func refererForURL(lastReq, newReq *url.URL) string {
	// https://tools.ietf.org/html/rfc7231#section-5.5.2
	//   "Clients SHOULD NOT include a Referer header field in a
	//    (non-secure) HTTP request if the referring page was
	//    transferred with a secure protocol."
	if lastReq.Scheme == "https" && newReq.Scheme == "http" {
		return ""
	}
	referer := lastReq.String()
	if lastReq.User != nil {
		// This is not very efficient, but is the best we can
		// do without:
		// - introducing a new method on URL
		// - creating a race condition
		// - copying the URL struct manually, which would cause
		//   maintenance problems down the line
		auth := lastReq.User.String() + "@"
		referer = strings.Replace(referer, auth, "", 1)
	}
	return referer
}

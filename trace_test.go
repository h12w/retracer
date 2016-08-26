package retracer

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestTrace(t *testing.T) {
	var tlsURL string
	var httpURL string
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.RequestURI {
		case "/2":
			w.Write([]byte(jsRedirectPage(tlsURL + "/3")))
		case "/3":
			http.Redirect(w, req, httpURL+"/4", http.StatusFound)
		}
	}))
	defer tlsServer.Close()
	tlsURL = tlsServer.URL
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.RequestURI {
		case "/0":
			w.Write([]byte(jsRedirectPage(httpURL + "/1")))
		case "/1":
			http.Redirect(w, req, tlsURL+"/2", http.StatusFound)
		case "/4":
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer httpServer.Close()
	httpURL = httpServer.URL
	certs, err := NewCertPool("cert")
	if err != nil {
		t.Fatal(err)
	}
	tracer := &Tracer{
		RoundTripper: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: time.Second,
		Certs:   certs,
	}
	var via []string
	err = tracer.Trace(httpServer.URL+"/0", func(uri string, r *http.Response) error {
		via = append(via, uri)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	expectedVia := []string{
		httpURL + "/0",
		httpURL + "/1",
		tlsURL + "/2",
		tlsURL + "/3",
		httpURL + "/4",
	}
	if !reflect.DeepEqual(via, expectedVia) {
		t.Fatalf("expect \n%v\n got \n%v", expectedVia, via)
	}
}

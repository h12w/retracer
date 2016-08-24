package retracer

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTrace(t *testing.T) {
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {}))
	sURL := ""
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.RequestURI {
		case "/0":
			http.Redirect(w, req, sURL+"/1", http.StatusFound)
		case "/1":
			w.Write([]byte(jsRedirectPage(sURL + "/2")))
		case "/2":
			w.Write([]byte(jsRedirectPage(tlsServer.URL + "/3")))
		}
	}))
	defer httpServer.Close()
	sURL = httpServer.URL
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
	for i := range via {
		via[i] = strings.TrimPrefix(via[i], httpServer.URL+"/")
	}
	for i := range via {
		via[i] = strings.TrimPrefix(via[i], tlsServer.URL+"/")
	}
	if !reflect.DeepEqual(via, []string{"0", "1", "2", "3"}) {
		t.Fatalf("expect [0 1 2 3] got %v", via)
	}
}

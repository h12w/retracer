package retracer

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTrace(t *testing.T) {
	sURL := ""
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.RequestURI {
		case "/0":
			http.Redirect(w, req, sURL+"/1", http.StatusFound)
		case "/1":
			w.Write([]byte(jsRedirectPage(sURL + "/2")))
		}
	}))
	defer s.Close()
	sURL = s.URL
	tracer := &Tracer{
		RoundTripper: &http.Transport{},
		Timeout:      time.Second,
	}
	var via []string
	err := tracer.Trace(s.URL+"/0", func(uri string, r *http.Response) error {
		via = append(via, uri)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := range via {
		via[i] = strings.TrimPrefix(via[i], s.URL+"/")
	}
	if !reflect.DeepEqual(via, []string{"0", "1", "2"}) {
		t.Fatalf("expect [0 1 2] got %v", via)
	}
}

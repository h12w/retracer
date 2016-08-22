package retracer

import (
	"testing"
	"time"
)

func TestJSSetLocation(t *testing.T) {
	const (
		pageURI     = "http://test/"
		redirectURI = "http://redirect/"
	)
	pageBody := jsRedirectPage(redirectURI)
	gotRedirectURI, err := (&JSTracer{Timeout: time.Second}).Trace(pageURI, []byte(pageBody))
	if err != nil {
		t.Fatal(err)
	}
	if gotRedirectURI != redirectURI {
		t.Fatalf("expect %s got %s", redirectURI, gotRedirectURI)
	}
}

func jsRedirectPage(redirectURI string) string {
	return `
		<html>
		    <body>
		        <script type="text/javascript">
					window.location.href = "` + redirectURI + `";
				</script>
		    </body>
		</html>
	`
}

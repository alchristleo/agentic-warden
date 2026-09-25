// Command fakeidp runs the oidctest provider for the console's end-to-end
// suite. It is a test fixture: never deploy it.
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/acme/agent-wrapper/internal/console/oidctest"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9400", "listen address")
	clientID := flag.String("client-id", "console", "OIDC client id")
	secret := flag.String("client-secret", "secret", "OIDC client secret")
	flag.Parse()
	p, err := oidctest.New("http://"+*addr, *clientID, *secret)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("fake IdP at http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, p.Handler()))
}

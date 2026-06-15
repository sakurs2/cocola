// admin-mint is the Go-side, command-line token minter — the symmetric twin of
// the gateway's Python `issue-token` CLI. It exists so ops (and the M5 cross-
// language e2e) can mint a cocola identity token without standing up the HTTP
// server, and so we can prove the byte-for-byte interop claim: a token minted
// here verifies in the Python gateway with the SAME shared secret.
//
//	COCOLA_AUTH_SECRET=... admin-mint -user emp-42 -tenant team-a -ttl 3600
//
// Prints just the compact JWS to stdout (nothing else), so it composes in pipes.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/cocola-project/cocola/packages/go-common/config"
	"github.com/cocola-project/cocola/packages/go-common/token"
)

func main() {
	user := flag.String("user", "", "subject (user id) — required")
	tenant := flag.String("tenant", "", "tenant id (optional)")
	ttl := flag.Int64("ttl", 0, "lifetime in seconds; 0=issuer default, <0=non-expiring")
	issuer := flag.String("issuer", "cocola", "iss claim")
	flag.Parse()

	secret := config.SecretFromEnv("COCOLA_AUTH_SECRET")
	if secret == "" {
		fmt.Fprintln(os.Stderr, "error: COCOLA_AUTH_SECRET must be set")
		os.Exit(2)
	}
	if *user == "" {
		fmt.Fprintln(os.Stderr, "error: -user is required")
		os.Exit(2)
	}

	iss := token.NewIssuer(secret, *issuer, 30*24*time.Hour)
	d := time.Duration(*ttl) * time.Second
	tok, _, err := iss.Issue(*user, *tenant, d, time.Now().Unix())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(tok)
}

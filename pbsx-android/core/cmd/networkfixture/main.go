// A loopback-only HTTPS fixture for Android socket-binding instrumentation.
// It accepts dummy credentials only and never connects to a real PBS server.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	output := flag.String("config", "", "destination for dummy emulator connection JSON")
	flag.Parse()
	if *output == "" {
		panic("-config is required")
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/admin/datastore/backup/snapshots" || r.Header.Get("Authorization") != "PBSAPIToken=fixture@pbs!android:instrumentation-fixture-secret" {
			http.Error(w, "unexpected fixture request", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[]}`)
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	_, port, err := net.SplitHostPort(endpoint.Host)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(server.Certificate().Raw)
	config, _ := json.Marshal(map[string]string{"server": "https://10.0.2.2:" + port, "host_id": "android-test", "fingerprint": hex.EncodeToString(sum[:]), "auth_id": "fixture@pbs!android", "secret": "instrumentation-fixture-secret"})
	if err := os.WriteFile(*output, config, 0600); err != nil {
		panic(err)
	}
	fmt.Println("Loopback HTTPS fixture ready")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
}

// Command agy-stub-gateway is a throwaway https server for scripts/probe-agy-admin.sh.
// It makes a self-signed CA, writes the CA certificate to -ca-out, serves a leaf for
// gateway.example.com over HTTP/2, logs every request as one JSON line, and answers
// gRPC calls with UNIMPLEMENTED and everything else with 404.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"flag"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	addr := flag.String("addr", ":443", "listen address")
	caOut := flag.String("ca-out", "", "write the CA certificate (PEM) here")
	flag.Parse()
	if *caOut == "" {
		log.Fatal("-ca-out is required")
	}

	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "agy probe CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		log.Fatal(err)
	}
	caCert, _ := x509.ParseCertificate(caDER)
	if err := os.WriteFile(*caOut, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o644); err != nil {
		log.Fatal(err)
	}

	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "gateway.example.com"},
		DNSNames:     []string{"gateway.example.com", "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		log.Fatal(err)
	}

	srv := &http.Server{
		Addr: *addr,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{{Certificate: [][]byte{leafDER}, PrivateKey: leafKey}},
			NextProtos:   []string{"h2", "http/1.1"},
		},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hdr := map[string]string{}
			for k, v := range r.Header {
				hdr[k] = strings.Join(v, ",")
			}
			line, _ := json.Marshal(map[string]any{"method": r.Method, "path": r.URL.Path, "proto": r.Proto, "headers": hdr})
			os.Stdout.Write(append(line, '\n'))
			if strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
				w.Header().Set("Content-Type", "application/grpc")
				w.Header().Set("Grpc-Status", "12")
				w.Header().Set("Grpc-Message", "stub gateway")
				return
			}
			http.NotFound(w, r)
		}),
	}
	log.Fatal(srv.ListenAndServeTLS("", ""))
}

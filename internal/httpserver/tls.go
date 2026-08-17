package httpserver

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// EnsureSelfSignedCert generates a per-device self-signed certificate on first
// boot if certFile/keyFile don't already exist. The operator can later replace
// these with their own cert (bring-your-own-cert). Returns nil if a cert is
// already present.
//
// extraSANs are additional Subject Alternative Names (IP addresses or DNS
// names) the cert should be valid for — e.g. the device's LAN IP, which a
// bridged container can't auto-discover. IPs are detected by net.ParseIP;
// everything else is treated as a DNS name. Locally-visible unicast IPs are
// also included automatically (useful for host-network / native deployments).
func EnsureSelfSignedCert(certFile, keyFile, hostname string, extraSANs []string) error {
	if fileExists(certFile) && fileExists(keyFile) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(certFile), 0o750); err != nil {
		return err
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}

	dnsNames := []string{hostname, hostname + ".local", "localhost"}
	ips := append([]net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}, localUnicastIPs()...)
	for _, s := range extraSANs {
		if ip := net.ParseIP(s); ip != nil {
			ips = append(ips, ip)
		} else if s != "" {
			dnsNames = append(dnsNames, s)
		}
	}

	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: hostname, Organization: []string{"Torizon Gateway"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0), // 10 years; device-local trust
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              dnsNames,
		IPAddresses:           ips,
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	if err := writePEM(certFile, "CERTIFICATE", der, 0o644); err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return err
	}
	return writePEM(keyFile, "EC PRIVATE KEY", keyDER, 0o600)
}

func writePEM(path, blockType string, der []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: blockType, Bytes: der})
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// localUnicastIPs returns this host's non-loopback, non-link-local unicast IPs.
// In a bridged container these are the container's addresses (not the host's);
// with host networking or a native deployment they are the real device IPs.
func localUnicastIPs() []net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []net.IP
	for _, a := range addrs {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		if ip.IsGlobalUnicast() {
			out = append(out, ip)
		}
	}
	return out
}

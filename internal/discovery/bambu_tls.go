package discovery

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// Bambu printers can't always be found over SSDP. Their beacons arrive on UDP
// :2021, and Bambu Studio / Orca Slicer bind that port *without* SO_REUSEPORT —
// on macOS and the BSDs every binder must set it, so while a slicer is running
// our listener cannot start at all. Since most Bambu owners run a slicer, SSDP
// alone leaves onboarding blind.
//
// The TLS sweep is the reliable path. Every Bambu printer answers MQTT over TLS
// on :8883 with a certificate issued by Bambu's own CA whose subject CommonName
// IS the printer serial — so a handshake alone identifies the device, with no
// access code and no cooperation from the slicer. Checking the issuer matters:
// unrelated devices (alarm panels, brokers) also listen on :8883, and only the
// BBL-issued chain marks a real printer.
const (
	bambuMQTTPort = 8883
	// bambuIssuerMark appears in the issuer of every Bambu device certificate
	// ("O=BBL Technologies Co., Ltd, CN=BBL CA").
	bambuIssuerMark = "BBL"
)

// ScanBambuTLS sweeps every local /24 for Bambu printers by TLS fingerprint. It
// complements ScanBambu (SSDP) and works even when a slicer holds the SSDP port.
func ScanBambuTLS(ctx context.Context) []BambuPrinter {
	subnets := localSubnets()
	targets := make([]string, 0, len(subnets)*254)
	for _, base := range subnets {
		for i := 1; i <= 254; i++ {
			targets = append(targets, fmt.Sprintf("%s.%d", base, i))
		}
	}

	var (
		mu    sync.Mutex
		wg    sync.WaitGroup
		found []BambuPrinter
		sem   = make(chan struct{}, 64) // matches Scan: gentle on small home LANs
	)

	for _, ip := range targets {
		select {
		case <-ctx.Done():
			wg.Wait()
			return found
		default:
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }()
			// Cheap reachability check first, so dead hosts don't each burn a
			// full TLS handshake timeout.
			if !tcpOpen(ip, bambuMQTTPort, 800*time.Millisecond) {
				return
			}
			if p, ok := probeBambuTLS(ctx, ip); ok {
				mu.Lock()
				found = append(found, p)
				mu.Unlock()
			}
		}(ip)
	}
	wg.Wait()
	return found
}

// probeBambuTLS handshakes with ip:8883 and reads the printer serial out of the
// certificate subject. Verification is skipped (the cert is signed by Bambu's
// private CA, which we don't ship) — this identifies a device, it does not
// authenticate it; the access code does that later.
func probeBambuTLS(ctx context.Context, ip string) (BambuPrinter, bool) {
	d := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 3 * time.Second},
		Config:    &tls.Config{InsecureSkipVerify: true}, // #nosec G402 — identification only; see above
	}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, fmt.Sprint(bambuMQTTPort)))
	if err != nil {
		return BambuPrinter{}, false
	}
	defer func() { _ = conn.Close() }()

	tc, ok := conn.(*tls.Conn)
	if !ok {
		return BambuPrinter{}, false
	}
	certs := tc.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return BambuPrinter{}, false
	}
	serial, ok := bambuSerialFromCert(certs[0])
	if !ok {
		return BambuPrinter{}, false
	}
	return BambuPrinter{
		Host:   ip,
		Serial: serial,
		Name:   "Bambu Lab printer", // model is unknown until we authenticate
		Source: SourceTLS,
	}, true
}

// bambuSerialFromCert extracts the serial from a Bambu device certificate,
// rejecting certificates from anything that merely happens to listen on :8883.
func bambuSerialFromCert(cert *x509.Certificate) (string, bool) {
	issuer := cert.Issuer.CommonName + " " + strings.Join(cert.Issuer.Organization, " ")
	if !strings.Contains(strings.ToUpper(issuer), bambuIssuerMark) {
		return "", false
	}
	cn := strings.TrimSpace(cert.Subject.CommonName)
	// Serials are bare alphanumeric identifiers; a dotted CN is a hostname,
	// which means this isn't a printer certificate.
	if cn == "" || strings.Contains(cn, ".") {
		return "", false
	}
	return cn, true
}

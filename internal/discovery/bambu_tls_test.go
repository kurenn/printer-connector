package discovery

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"testing"
)

func TestBambuSerialFromCert(t *testing.T) {
	bblIssuer := pkix.Name{
		CommonName:   "BBL CA",
		Organization: []string{"BBL Technologies Co., Ltd"},
	}

	tests := []struct {
		name       string
		issuer     pkix.Name
		subjectCN  string
		wantSerial string
		wantOK     bool
	}{
		{
			// Real A1 mini certificate, verified against hardware.
			name:       "bambu device cert",
			issuer:     bblIssuer,
			subjectCN:  "0300CA612001784",
			wantSerial: "0300CA612001784",
			wantOK:     true,
		},
		{
			// An alarm panel on the same LAN also listens on :8883; the issuer
			// check is what keeps it out of the printer list.
			name:      "non-bambu device on 8883",
			issuer:    pkix.Name{CommonName: "Qolsys Inc Pvt Ltd.", Organization: []string{"Qolsys Inc."}},
			subjectCN: "www.qolsys.com",
			wantOK:    false,
		},
		{
			name:      "bambu issuer but hostname subject",
			issuer:    bblIssuer,
			subjectCN: "printer.local",
			wantOK:    false,
		},
		{
			name:      "bambu issuer but empty subject",
			issuer:    bblIssuer,
			subjectCN: "",
			wantOK:    false,
		},
		{
			name:      "issuer org only",
			issuer:    pkix.Name{Organization: []string{"BBL Technologies Co., Ltd"}},
			subjectCN: "0300CA612001784",
			// Serial is recoverable from the org alone.
			wantSerial: "0300CA612001784",
			wantOK:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cert := &x509.Certificate{
				Issuer:  tt.issuer,
				Subject: pkix.Name{CommonName: tt.subjectCN},
			}
			serial, ok := bambuSerialFromCert(cert)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if serial != tt.wantSerial {
				t.Errorf("serial = %q, want %q", serial, tt.wantSerial)
			}
		})
	}
}

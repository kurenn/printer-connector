package bambu

import (
	"bytes"
	"crypto/tls"
	"net"
	"path"
	"time"

	"github.com/jlaffaye/ftp"
)

// Bambu printers expose their storage over implicit FTPS on port 990, same
// credentials as MQTT (user "bblp", password = access code), self-signed cert.
// Files printed via project_file are uploaded to the FTP root.
//
// NOTE: Bambu's FTPS is strict about reusing the control channel's TLS session
// on the data channel. jlaffaye/ftp negotiates a fresh data-channel session,
// which most firmware accepts but some reject. These functions are written
// against the documented protocol and need validation against real hardware;
// they are isolated here (and injected into Client) so the implementation can
// be swapped without touching the driver logic.

const (
	ftpPort    = "990"
	ftpUser    = "bblp"
	ftpTimeout = 15 * time.Second
)

func ftpsDial(host, accessCode string) (*ftp.ServerConn, error) {
	conn, err := ftp.Dial(
		net.JoinHostPort(host, ftpPort),
		ftp.DialWithTLS(&tls.Config{InsecureSkipVerify: true}), // #nosec G402 — LAN, self-signed; access code authenticates
		ftp.DialWithTimeout(ftpTimeout),
		ftp.DialWithDisabledEPSV(true), // Bambu firmware uses PASV, not EPSV
	)
	if err != nil {
		return nil, err
	}
	if err := conn.Login(ftpUser, accessCode); err != nil {
		_ = conn.Quit()
		return nil, err
	}
	return conn, nil
}

func ftpsUpload(host, accessCode, filename string, content []byte) error {
	conn, err := ftpsDial(host, accessCode)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Quit() }()
	return conn.Stor(path.Base(filename), bytes.NewReader(content))
}

func ftpsDelete(host, accessCode, filename string) error {
	conn, err := ftpsDial(host, accessCode)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Quit() }()
	return conn.Delete(path.Base(filename))
}

func ftpsList(host, accessCode string) ([]map[string]any, error) {
	conn, err := ftpsDial(host, accessCode)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Quit() }()

	entries, err := conn.List("/")
	if err != nil {
		return nil, err
	}
	files := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		if e.Type != ftp.EntryTypeFile {
			continue
		}
		files = append(files, map[string]any{
			"path":     e.Name,
			"filename": e.Name,
			"size":     e.Size,
			"modified": e.Time.UTC().Format(time.RFC3339),
		})
	}
	return files, nil
}

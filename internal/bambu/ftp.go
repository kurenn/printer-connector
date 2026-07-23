package bambu

import (
	"bytes"
	"crypto/md5" // #nosec G501 — Bambu's protocol specifies md5 for file verification, not security
	"crypto/tls"
	"encoding/hex"
	"io"
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

// ftpsMD5 streams a file back from the printer and returns its md5 hex digest.
// StartPrint sends this in the project_file command; the firmware verifies it
// before printing. Streaming (io.Copy into the hash) avoids holding a large 3MF
// in memory just to fingerprint it.
func ftpsMD5(host, accessCode, filename string) (string, error) {
	conn, err := ftpsDial(host, accessCode)
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Quit() }()

	r, err := conn.Retr(printerPath(filename))
	if err != nil {
		return "", err
	}
	defer func() { _ = r.Close() }()

	h := md5.New() // #nosec G401 — file-integrity digest required by Bambu's protocol, not a security hash
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
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
	// Accepts a directory-qualified path from ListFiles as well as a bare name.
	return conn.Delete(printerPath(filename))
}

// listDirs are the directories that actually hold printable files. The FTP root
// contains *only* subdirectories on real firmware, so listing it alone always
// returned an empty set: prints pushed from the cloud and slicer uploads land in
// /cache, and the factory sample models live in /model. Root is still listed
// because ftpsUpload stores there.
var listDirs = []string{"/", "/cache", "/model"}

func ftpsList(host, accessCode string) ([]map[string]any, error) {
	conn, err := ftpsDial(host, accessCode)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Quit() }()

	files := make([]map[string]any, 0, 16)
	var firstErr error
	for _, dir := range listDirs {
		entries, err := conn.List(dir)
		if err != nil {
			// Directory sets vary by model and firmware, so a missing one is
			// normal — remember the error but keep walking the rest.
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, e := range entries {
			if e.Type != ftp.EntryTypeFile {
				continue
			}
			files = append(files, map[string]any{
				// Directory-qualified so StartPrint can address the file; the
				// bare name stays available for display.
				"path":     path.Join(dir, e.Name),
				"filename": e.Name,
				"size":     e.Size,
				"modified": e.Time.UTC().Format(time.RFC3339),
			})
		}
	}
	// Only fail if every directory failed — a partial walk is still useful.
	if len(files) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return files, nil
}

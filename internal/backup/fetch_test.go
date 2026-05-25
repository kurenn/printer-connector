package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type fakeFetcher struct {
	files map[string][]RemoteFile      // root -> files
	data  map[string]map[string][]byte // root -> path -> content
}

func (f fakeFetcher) ListFiles(_ context.Context, root string) ([]RemoteFile, error) {
	return f.files[root], nil
}

func (f fakeFetcher) DownloadFile(_ context.Context, root, path string, w io.Writer) error {
	_, err := w.Write(f.data[root][path])
	return err
}

func tarEntries(t *testing.T, path string) map[string]bool {
	t.Helper()
	fh, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	gz, err := gzip.NewReader(fh)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	names := map[string]bool{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names[h.Name] = true
	}
	return names
}

func TestFetchAndCreate(t *testing.T) {
	f := fakeFetcher{
		files: map[string][]RemoteFile{
			"config": {{Path: "printer.cfg", Size: 5}, {Path: "sub/extra.cfg", Size: 3}},
			"logs":   {{Path: "klippy.log", Size: 4}},
		},
		data: map[string]map[string][]byte{
			"config": {"printer.cfg": []byte("hello"), "sub/extra.cfg": []byte("abc")},
			"logs":   {"klippy.log": []byte("logs")},
		},
	}
	out := filepath.Join(t.TempDir(), "b.tar.gz")

	res, err := FetchAndCreate(context.Background(), f, Options{
		IncludeConfig: true,
		IncludeLogs:   true,
		OutputPath:    out,
		MaxSizeBytes:  1 << 20,
	})
	if err != nil {
		t.Fatalf("FetchAndCreate: %v", err)
	}
	if res.SizeBytes <= 0 || res.SHA256 == "" {
		t.Fatalf("bad result %+v", res)
	}

	names := tarEntries(t, out)
	for _, want := range []string{"config/printer.cfg", "config/sub/extra.cfg", "logs/klippy.log"} {
		if !names[want] {
			t.Errorf("archive missing %q; got %v", want, names)
		}
	}
}

func TestFetchAndCreateNoFetchableRoots(t *testing.T) {
	// Only the database is requested — not a Moonraker file root, so nothing to fetch.
	_, err := FetchAndCreate(context.Background(), fakeFetcher{}, Options{
		IncludeDatabase: true,
		OutputPath:      filepath.Join(t.TempDir(), "b.tar.gz"),
	})
	if err == nil {
		t.Error("expected an error when no fetchable roots are selected")
	}
}

func TestFetchAndCreateRejectsPathTraversal(t *testing.T) {
	f := fakeFetcher{
		files: map[string][]RemoteFile{"config": {{Path: "../../etc/evil", Size: 1}}},
		data:  map[string]map[string][]byte{"config": {"../../etc/evil": []byte("x")}},
	}
	_, err := FetchAndCreate(context.Background(), f, Options{
		IncludeConfig: true,
		OutputPath:    filepath.Join(t.TempDir(), "b.tar.gz"),
		MaxSizeBytes:  1 << 20,
	})
	if err == nil {
		t.Error("expected an error for a path-traversal entry")
	}
}

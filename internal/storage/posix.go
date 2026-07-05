package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// posixStore is the local/shared filesystem backend (topology A). When root is
// empty, keys are treated as filesystem paths directly — reproducing the base
// spec's behaviour exactly; when root is set, keys are joined under it.
type posixStore struct {
	root string
}

func newPOSIX(root string) *posixStore { return &posixStore{root: root} }

func (p *posixStore) Backend() string { return BackendPOSIX }

func (p *posixStore) path(key string) string {
	key = filepath.FromSlash(key)
	if p.root == "" {
		return key
	}
	return filepath.Join(p.root, key)
}

func (p *posixStore) List(_ context.Context, prefix string) ([]Entry, error) {
	dir := p.path(prefix)
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // an absent input dir is simply empty
		}
		return nil, err
	}
	out := make([]Entry, 0, len(ents))
	for _, e := range ents {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		key := prefix
		if key != "" && !strings.HasSuffix(key, "/") {
			key += "/"
		}
		out = append(out, Entry{Key: key + e.Name(), Size: info.Size(), ModTime: info.ModTime()})
	}
	return out, nil
}

func (p *posixStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	f, err := os.Open(p.path(key))
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
	return f, err
}

func (p *posixStore) Put(_ context.Context, key string, body io.Reader, _ Meta) error {
	full := p.path(key)
	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(full)+"-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename
	if _, err := io.Copy(tmp, body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, full)
}

func (p *posixStore) Stat(_ context.Context, key string) (Entry, error) {
	info, err := os.Stat(p.path(key))
	if os.IsNotExist(err) {
		return Entry{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, err
	}
	return Entry{Key: key, Size: info.Size(), ModTime: info.ModTime()}, nil
}

func (p *posixStore) Move(_ context.Context, srcKey, dstKey string) error {
	dst := p.path(dstKey)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	err := os.Rename(p.path(srcKey), dst)
	if os.IsNotExist(err) {
		return ErrNotFound
	}
	return err
}

func (p *posixStore) Delete(_ context.Context, key string) error {
	err := os.Remove(p.path(key))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (p *posixStore) Close() error { return nil }

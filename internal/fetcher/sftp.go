package fetcher

import (
	"context"
	"fmt"
	"net"
	"path"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/pgvanniekerk/baasparse/internal/storage"
	"github.com/pgvanniekerk/baasparse/internal/store"
)

const (
	defaultSFTPPort = 22
	dialTimeout     = 30 * time.Second
)

// sftpConn is a live SFTP session (ssh transport + sftp subsystem).
type sftpConn struct {
	ssh  *ssh.Client
	sftp *sftp.Client
}

// dialSFTP opens an SFTP session to r using password and/or private-key auth and
// the given host-key verifier. The ctx bounds the TCP dial; the session itself
// outlives it (transfers use their own context).
func dialSFTP(ctx context.Context, r store.RemoteConfig, verify hostKeyVerifier) (*sftpConn, error) {
	auth, err := authMethods(r)
	if err != nil {
		return nil, err
	}
	if len(auth) == 0 {
		return nil, fmt.Errorf("sftp: no credentials for %s@%s (need password or key)", r.User, r.Host)
	}
	port := r.Port
	if port == 0 {
		port = defaultSFTPPort
	}
	addr := net.JoinHostPort(r.Host, fmt.Sprintf("%d", port))

	cfg := &ssh.ClientConfig{
		User:            r.User,
		Auth:            auth,
		HostKeyCallback: verify.callback,
		Timeout:         dialTimeout,
	}

	d := net.Dialer{Timeout: dialTimeout}
	tcp, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("sftp: dial %s: %w", addr, err)
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(tcp, addr, cfg)
	if err != nil {
		_ = tcp.Close()
		return nil, fmt.Errorf("sftp: ssh handshake %s: %w", addr, err)
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	sc, err := sftp.NewClient(client)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("sftp: open subsystem %s: %w", addr, err)
	}
	return &sftpConn{ssh: client, sftp: sc}, nil
}

func authMethods(r store.RemoteConfig) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	if r.Key != "" {
		signer, err := ssh.ParsePrivateKey([]byte(r.Key))
		if err != nil {
			return nil, fmt.Errorf("sftp: parse private key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if r.Password != "" {
		methods = append(methods, ssh.Password(r.Password))
	}
	return methods, nil
}

// Close releases the sftp and ssh resources.
func (c *sftpConn) Close() {
	if c.sftp != nil {
		_ = c.sftp.Close()
	}
	if c.ssh != nil {
		_ = c.ssh.Close()
	}
}

// list returns the names of regular files under remoteDir (directories and
// non-regular entries are skipped).
func (c *sftpConn) list(ctx context.Context, remoteDir string) ([]string, error) {
	if remoteDir == "" {
		remoteDir = "."
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	infos, err := c.sftp.ReadDir(remoteDir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(infos))
	for _, fi := range infos {
		if fi.IsDir() || !fi.Mode().IsRegular() {
			continue
		}
		names = append(names, fi.Name())
	}
	return names, nil
}

// stream copies a remote file into the input-area store at inputKey. storage.Put
// is atomic (temp-then-rename on posix; atomic PutObject on s3), so a partially
// transferred file never appears in the input area for the watcher to pick up.
func (c *sftpConn) stream(ctx context.Context, remotePath string, sg storage.Store, inputKey string) error {
	rf, err := c.sftp.Open(remotePath)
	if err != nil {
		return fmt.Errorf("open remote %s: %w", remotePath, err)
	}
	defer rf.Close()
	if err := sg.Put(ctx, inputKey, rf, storage.Meta{}); err != nil {
		return fmt.Errorf("put %s: %w", inputKey, err)
	}
	return nil
}

// postFetch applies the remote disposition after a successful transfer:
// "delete" removes the remote file, "move" relocates it under MovePath, and
// "leave"/"" leaves it in place (the fetcher's dedup guards against re-fetching).
func (c *sftpConn) postFetch(ctx context.Context, r store.RemoteConfig, remotePath, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch r.PostFetch {
	case "delete":
		return c.sftp.Remove(remotePath)
	case "move":
		if r.MovePath == "" {
			return fmt.Errorf("post-fetch move: no MovePath configured")
		}
		_ = c.sftp.MkdirAll(r.MovePath)
		dst := path.Join(r.MovePath, name)
		if err := c.sftp.PosixRename(remotePath, dst); err != nil {
			// fall back to a plain rename for servers without the posix-rename ext
			if rerr := c.sftp.Rename(remotePath, dst); rerr != nil {
				return fmt.Errorf("move %s -> %s: %w", remotePath, dst, rerr)
			}
		}
		return nil
	default: // "leave" or ""
		return nil
	}
}

package utils

import (
	"context"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/net/proxy"
)

type sshDialer struct {
	ssh *ssh.Client
}

func (x *sshDialer) DialTimeout(network, address string, timeout time.Duration) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), timeout)
	defer cancel()
	return dialContext(ctx, x.ssh, network, address)
}

func (x *sshDialer) DialContext(ctx context.Context, network string, address string) (net.Conn, error) {
	return dialContext(ctx, x.ssh, network, address)
}

func (x *sshDialer) DialContextDatabase(ctx context.Context, address string) (net.Conn, error) {
	return dialContext(ctx, x.ssh, "tcp", address)
}

func dialContext(ctx context.Context, d proxy.Dialer, network, address string) (net.Conn, error) {
	var (
		conn net.Conn
		done = make(chan struct{}, 1)
		err  error
	)

	go func() {
		conn, err = d.Dial(network, address)
		close(done)
		if conn != nil && ctx.Err() != nil {
			_ = conn.Close()
		}
	}()

	select {
	case <-ctx.Done():
		err = ctx.Err()
	case <-done:
	}
	return conn, err
}

func (x *sshDialer) Dial(network, address string) (net.Conn, error) {
	return x.ssh.Dial(network, address)
}

func SSHDialer(network, address, user, key, keyPassword string) (*sshDialer, error) {
	pkBytes, err := os.ReadFile(key)
	if err != nil {
		return nil, err
	}
	fromPem, err := signerFromPem(pkBytes, []byte(keyPassword))
	if err != nil {
		return nil, err
	}
	hostKeys, err := defaultKnownHosts()
	if err != nil {
		return nil, err
	}
	cfg := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(fromPem),
		},
		HostKeyCallback: hostKeys,
	}
	dial, err := ssh.Dial(network, address, cfg)
	if err != nil {
		return nil, err
	}
	return &sshDialer{
		ssh: dial,
	}, err
}

// defaultKnownHosts builds a host key callback from the user's known_hosts file.
// A host that is absent from it, or whose key has changed, fails the dial.
func defaultKnownHosts() (ssh.HostKeyCallback, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return knownhosts.New(filepath.Join(home, ".ssh", "known_hosts"))
}

func signerFromPem(pemBytes []byte, password []byte) (ssh.Signer, error) {
	if pemBlock, _ := pem.Decode(pemBytes); pemBlock == nil {
		return nil, errors.New("pem decode failed, no key found")
	}
	signer, err := ssh.ParsePrivateKey(pemBytes)
	if err == nil {
		return signer, nil
	}
	// Only a missing passphrase is retryable; anything else is a real parse failure.
	var passphraseMissing *ssh.PassphraseMissingError
	if !errors.As(err, &passphraseMissing) {
		return nil, err
	}
	return ssh.ParsePrivateKeyWithPassphrase(pemBytes, password)
}

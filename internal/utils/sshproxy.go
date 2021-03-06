package utils

import (
	"context"
	"encoding/pem"
	"net"
	"os"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
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

func (x *sshDialer) DialContextMYSQL(ctx context.Context, address string) (net.Conn, error) {
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
	cfg := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(fromPem),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	dial, err := ssh.Dial(network, address, cfg)
	if err != nil {
		return nil, err
	}
	return &sshDialer{
		ssh: dial,
	}, err
}

func signerFromPem(pemBytes []byte, password []byte) (ssh.Signer, error) {
	// read pem block
	err := errors.New("Pem decode failed, no key found")
	pemBlock, _ := pem.Decode(pemBytes)
	if pemBlock == nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(pemBytes)
	if err != nil {
		if err.Error() == (&ssh.PassphraseMissingError{}).Error() {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(pemBytes, password)
			if err != nil {
				return nil, err
			}
		}
	}
	return signer, nil
}

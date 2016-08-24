package retracer

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
	"math/big"
	"net"
	"os"
	"path"
	"sync"

	"h12.me/errors"
)

func fakeSecureConn(conn net.Conn, host string, pool CertPool) (net.Conn, error) {
	cert, err := pool.Get(host)
	if err != nil {
		return nil, err
	}
	return tls.Server(conn, &tls.Config{
		Certificates: []tls.Certificate{*cert},
		ServerName:   host,
	}), nil
}

type CertPool interface {
	Get(host string) (*tls.Certificate, error)
}

type certPool struct {
	ca    *tls.Certificate
	dir   string
	data  map[string]*tls.Certificate
	mutex sync.Mutex
}

func NewCertPool(dir string) (CertPool, error) {
	poolDir := path.Join(dir, "pool")
	if err := os.MkdirAll(poolDir, 0755); err != nil && !os.IsExist(err) {
		return nil, errors.Wrap(err)
	}
	ca, err := tls.LoadX509KeyPair(path.Join(dir, "crt"), path.Join(dir, "key"))
	if err != nil {
		return nil, errors.Wrap(err)
	}

	return &certPool{
		dir:  poolDir,
		ca:   &ca,
		data: make(map[string]*tls.Certificate),
	}, nil
}

func (pool *certPool) Get(host string) (*tls.Certificate, error) {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()

	if c, ok := pool.data[host]; ok {
		return c, nil
	}

	certFile := path.Join(pool.dir, host+".crt")
	der, err := ioutil.ReadFile(certFile)
	if err == nil {
		rcert, err := tls.X509KeyPair(pool.ca.Certificate[0], der)
		if err == nil {
			pool.data[host] = &rcert
			return &rcert, errors.Wrap(err)
		}
	}

	cert, err := pool.gen(host)
	if err != nil {
		return nil, err
	}
	pool.data[host] = cert
	if err := saveCertFile(cert, certFile); err != nil {
		return nil, err
	}
	return cert, nil
}
func saveCertFile(cert *tls.Certificate, file string) error {
	f, err := os.Create(file)
	if err != nil {
		return errors.Wrap(err)
	}
	defer f.Close()
	for _, c := range cert.Certificate {
		err = pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: c})
		if err != nil {
			defer os.Remove(file)
			return errors.Wrap(err)
		}
	}
	return nil
}

func (pool *certPool) gen(host string) (*tls.Certificate, error) {
	signer, err := x509.ParseCertificate(pool.ca.Certificate[0])
	if err != nil {
		return nil, errors.Wrap(err)
	}
	signer.Subject.CommonName = host

	hash := sha1.Sum([]byte(host))
	signee := &x509.Certificate{
		SerialNumber:          new(big.Int).SetBytes(hash[:]),
		Issuer:                signer.Issuer,
		Subject:               signer.Subject,
		NotBefore:             signer.NotBefore,
		NotAfter:              signer.NotAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	key := pool.ca.PrivateKey.(*rsa.PrivateKey)
	der, err := x509.CreateCertificate(rand.Reader, signee, signer, &key.PublicKey, key)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	return &tls.Certificate{
		Certificate: [][]byte{der, pool.ca.Certificate[0]},
		PrivateKey:  key,
	}, nil
}

package pki

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"time"
)

func generateCACert(filename string, privateKey *ecdsa.PrivateKey) (*x509.Certificate, error) {
	serialNumber, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	caTemplate := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   "My CA",
			Organization: []string{"My Org"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	certDer, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, err
	}
	file, err := os.Create(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	err = pem.Encode(file, &pem.Block{Type: "CERTIFICATE", Bytes: certDer})
	if err != nil {
		return nil, err
	}

	cert, err := x509.ParseCertificate(certDer)
	if err != nil {
		return nil, err
	}
	return cert, nil
}

func loadCACert(filename string) (*x509.Certificate, error) {
	certPem, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var block *pem.Block
	block, _ = pem.Decode(certPem)
	if block == nil {
		return nil, errors.New("no pem block decoded")
	}
	if block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
		return nil, errors.New("invalid block type")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	return cert, nil
}

func generatePrivateKey(filename string) (*ecdsa.PrivateKey, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return nil, err
	}
	keyFile, err := os.Create(filename)
	if err != nil {
		return nil, err
	}
	defer keyFile.Close()
	keyBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	return privateKey, nil
}

func loadPrivateKey(filename string) (*ecdsa.PrivateKey, error) {
	keyBytes, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(keyBytes)
	if block == nil {
		return nil, err
	}
	if block.Type != "EC PRIVATE KEY" || len(block.Headers) != 0 {
		return nil, errors.New("invalid block type")
	}

	return x509.ParseECPrivateKey(block.Bytes)
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}

type Provider struct {
	eePrivateKey *ecdsa.PrivateKey
	caPrivateKey *ecdsa.PrivateKey
	caCert       *x509.Certificate
}

// if files do not exist, provider will attempt to create them.
func NewProvider(eeKeyfile string, caKeyfile string, caCertfile string) (*Provider, error) {
	var p Provider
	var err error

	if fileExists(caKeyfile) {
		p.caPrivateKey, err = loadPrivateKey(caKeyfile)
		if err != nil {
			return nil, fmt.Errorf("could not load %q: %w", caKeyfile, err)
		}
	} else if !fileExists(caCertfile) {
		p.caPrivateKey, err = generatePrivateKey(caKeyfile)
		if err != nil {
			return nil, fmt.Errorf("could not save %q: %w", caKeyfile, err)
		}
	} else {
		return nil, fmt.Errorf("cannot generate new ca key %q because new ca cert would overwrite %q", caKeyfile, caCertfile)
	}
	if fileExists(eeKeyfile) {
		p.eePrivateKey, err = loadPrivateKey(eeKeyfile)
		if err != nil {
			return nil, fmt.Errorf("could not load %q: %w", eeKeyfile, err)
		}
	} else {
		p.eePrivateKey, err = generatePrivateKey(eeKeyfile)
		if err != nil {
			return nil, fmt.Errorf("could not save %q: %w", eeKeyfile, err)
		}
	}
	if fileExists(caCertfile) {
		p.caCert, err = loadCACert(caCertfile)
		if err != nil {
			return nil, fmt.Errorf("could not load %q: %w", caCertfile, err)
		}
	} else {
		p.caCert, err = generateCACert(caCertfile, p.caPrivateKey)
		if err != nil {
			return nil, fmt.Errorf("could not save %q: %w", caCertfile, err)
		}
	}
	return &p, nil
}

func (p *Provider) EEPrivateKey() crypto.PrivateKey {
	return p.eePrivateKey
}

func (p *Provider) SignCSR(der []byte) ([]byte, error) {
	csr, err := x509.ParseCertificateRequest(der)
	if err != nil {
		return nil, err
	}
	serialNumber, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	serverTemplate := x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      csr.Subject,
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certDer, err := x509.CreateCertificate(rand.Reader, &serverTemplate, p.caCert, csr.PublicKey, p.caPrivateKey)
	if err != nil {
		return nil, err
	}
	return certDer, nil
}

func (p *Provider) CACertPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(p.caCert)
	return pool
}

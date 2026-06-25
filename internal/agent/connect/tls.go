package connect

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	agentconfig "laz/internal/agent/config"
)

func mtlsConfig(cfg agentconfig.MTLS) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, err
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}
	if cfg.CAFile != "" {
		caRaw, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caRaw) {
			return nil, fmt.Errorf("failed to parse CA bundle")
		}
		tlsConfig.RootCAs = pool
	}
	return tlsConfig, nil
}

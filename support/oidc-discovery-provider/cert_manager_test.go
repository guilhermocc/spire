package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/spiffe/spire/pkg/common/x509util"
	"github.com/spiffe/spire/test/testkey"
	"github.com/stretchr/testify/require"
)

var (
	oidcServerKey    = testkey.MustEC256()
	oidcServerKeyNew = testkey.MustEC256()
	certFilePath     = "/oidcServerCert.pem"
	keyFilePath      = "/oidcServerKey.pem"
)

func TestTLSConfig(t *testing.T) {
	logger, logHook := test.NewNullLogger()

	oidcServerKeyDer, err := x509.MarshalECPrivateKey(oidcServerKey)
	require.NoError(t, err)

	certTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(0),
		NotAfter:     time.Now().Add(time.Hour),
		Subject: pkix.Name{
			Country:    []string{"BR"},
			CommonName: "oidc-provider-discovery.example.com",
		},
	}
	oidcServerCert, err := x509util.CreateCertificate(certTmpl, certTmpl, oidcServerKey.Public(), oidcServerKey)
	require.NoError(t, err)
	require.NotNilf(t, oidcServerCert, "oidcServerCert is nil")

	oidcServerKeyPem := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: oidcServerKeyDer,
	})

	oidcServerCertPem := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: oidcServerCert.Raw,
	})

	certTmpl.Subject.Country = []string{"AR"}
	oidcServerCertUpdated1, err := x509util.CreateCertificate(certTmpl, certTmpl, oidcServerKey.Public(), oidcServerKey)
	require.NoError(t, err)

	oidcServerKeyNewDer, err := x509.MarshalECPrivateKey(oidcServerKeyNew)
	require.NoError(t, err)

	oidcServerCertUpdated2, err := x509util.CreateCertificate(certTmpl, certTmpl, oidcServerKeyNew.Public(), oidcServerKeyNew)
	require.NoError(t, err)

	certTmpl.Subject.Country = []string{"US"}

	tmpDir := t.TempDir()

	writeFile(t, tmpDir+keyFilePath, oidcServerKeyPem)
	writeFile(t, tmpDir+certFilePath, oidcServerCertPem)
	writeFile(t, tmpDir+"/oidcServerKeyInvalid.pem", []byte{1})
	writeFile(t, tmpDir+"/oidcServerCertInvalid.pem", []byte{1})

	chInfo := &tls.ClientHelloInfo{
		ServerName: "oidc-provider-discovery.example.com",
	}

	ctx, cancelFn := context.WithCancel(context.Background())
	certManager, err := NewDiskCertManager(&ServingCertFileConfig{
		CertFilePath:     tmpDir + certFilePath,
		KeyFilePath:      tmpDir + keyFilePath,
		FileSyncInterval: 10 * time.Millisecond,
	}, logger)
	require.NoError(t, err)

	go func() {
		certManager.WatchFileChanges(ctx)
	}()

	tlsConfig := certManager.TLSConfig()

	t.Run("error when configuration does not contain serving cert file settings", func(t *testing.T) {
		_, err := NewDiskCertManager(nil, logger)
		require.EqualError(t, err, "missing serving cert file configuration")
	})

	t.Run("error when provided cert path do not exist", func(t *testing.T) {
		_, err := NewDiskCertManager(&ServingCertFileConfig{
			CertFilePath: tmpDir + "/nonexistent_cert.pem",
			KeyFilePath:  tmpDir + "/oidcServerKey.pem",
		}, logger)

		assertFileDontExist(t, tmpDir+"/nonexistent_cert.pem", err)
	})

	t.Run("error when provided key path do not exist", func(t *testing.T) {
		_, err := NewDiskCertManager(&ServingCertFileConfig{
			CertFilePath: tmpDir + certFilePath,
			KeyFilePath:  tmpDir + "/nonexistent_key.pem",
		}, logger)

		assertFileDontExist(t, tmpDir+"/nonexistent_key.pem", err)
	})

	t.Run("error when provided cert is invalid", func(t *testing.T) {
		_, err := NewDiskCertManager(&ServingCertFileConfig{
			CertFilePath: tmpDir + "/oidcServerCertInvalid.pem",
			KeyFilePath:  tmpDir + "/oidcServerKey.pem",
		}, logger)

		require.EqualError(t, err, "failed to load certificate: tls: failed to find any PEM data in certificate input")
	})

	t.Run("error when provided key is invalid", func(t *testing.T) {
		_, err := NewDiskCertManager(&ServingCertFileConfig{
			CertFilePath: tmpDir + certFilePath,
			KeyFilePath:  tmpDir + "/oidcServerKeyInvalid.pem",
		}, logger)

		require.EqualError(t, err, "failed to load certificate: tls: failed to find any PEM data in key input")
	})

	t.Run("success loading initial certificate from disk", func(t *testing.T) {
		cert, err := tlsConfig.GetCertificate(chInfo)
		require.NoError(t, err)
		require.Len(t, cert.Certificate, 1)
		x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
		require.NoError(t, err)
		require.Equal(t, oidcServerCert, x509Cert)
	})

	t.Run("success watching cert file changes", func(t *testing.T) {
		oidcServerCertUpdatedPem := pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: oidcServerCertUpdated1.Raw,
		})
		writeFile(t, tmpDir+certFilePath, oidcServerCertUpdatedPem)

		require.Eventuallyf(t, func() bool {
			cert, err := tlsConfig.GetCertificate(chInfo)
			if err != nil {
				return false
			}
			require.Len(t, cert.Certificate, 1)
			x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
			if err != nil {
				return false
			}
			return reflect.DeepEqual(oidcServerCertUpdated1, x509Cert)
		}, 10*time.Second, 101*time.Millisecond, "Failed to assert updated certificate")
	})

	t.Run("success watching to key file changes", func(t *testing.T) {
		writeFile(t, tmpDir+keyFilePath, pem.EncodeToMemory(&pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: oidcServerKeyNewDer,
		}))

		writeFile(t, tmpDir+certFilePath, pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: oidcServerCertUpdated2.Raw,
		}))

		require.Eventuallyf(t, func() bool {
			cert, err := tlsConfig.GetCertificate(chInfo)
			if err != nil {
				return false
			}
			require.Len(t, cert.Certificate, 1)
			x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
			if err != nil {
				return false
			}
			return reflect.DeepEqual(oidcServerCertUpdated2, x509Cert)
		}, 10*time.Second, 101*time.Millisecond, "Failed to assert updated certificate")
	})

	t.Run("update cert file with an invalid cert start error log loop", func(t *testing.T) {
		writeFile(t, tmpDir+certFilePath, []byte("invalid-cert"))

		errLogs := map[time.Time]struct{}{}

		// Assert error logs that will keep triggering until the cert is valid again
		require.Eventuallyf(t, func() bool {
			for _, entry := range logHook.AllEntries() {
				if entry.Level == logrus.ErrorLevel && strings.Contains(entry.Message, "Failed to load certificate: tls: failed to find any PEM data in certificate input") {
					errLogs[entry.Time] = struct{}{}
				}
			}
			return len(errLogs) >= 5
		}, 10*time.Second, 10*time.Millisecond, "Failed to assert error logs")

		// New cert is not loaded because it is invalid.
		cert, err := tlsConfig.GetCertificate(chInfo)
		require.NoError(t, err)
		require.Len(t, cert.Certificate, 1)
		x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
		require.NoError(t, err)
		require.Equal(t, oidcServerCertUpdated2, x509Cert)
	})

	t.Run("update key file with an invalid key start error log loop", func(t *testing.T) {
		writeFile(t, tmpDir+certFilePath, pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: oidcServerCertUpdated2.Raw,
		}))

		writeFile(t, tmpDir+keyFilePath, []byte("invalid-key"))

		// Assert error logs that will keep triggering until the cert is valid again.
		errLogs := map[time.Time]struct{}{}

		require.Eventuallyf(t, func() bool {
			for _, entry := range logHook.AllEntries() {
				if entry.Level == logrus.ErrorLevel && strings.Contains(entry.Message, "Failed to load certificate: tls: failed to find any PEM data in key input") {
					errLogs[entry.Time] = struct{}{}
				}
			}
			return len(errLogs) >= 5
		}, 10*time.Second, 10*time.Millisecond, "Failed to assert error logs")

		// New cert is not loaded because it is invalid.
		cert, err := tlsConfig.GetCertificate(chInfo)
		require.NoError(t, err)
		require.Len(t, cert.Certificate, 1)
		x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
		require.NoError(t, err)
		require.Equal(t, oidcServerCertUpdated2, x509Cert)
	})

	t.Run("stop logging error when update to valid certificate and key", func(t *testing.T) {
		writeFile(t, tmpDir+keyFilePath, oidcServerKeyPem)
		writeFile(t, tmpDir+certFilePath, oidcServerCertPem)

		require.Eventuallyf(t, func() bool {
			cert, err := tlsConfig.GetCertificate(chInfo)
			if err != nil {
				return false
			}
			require.Len(t, cert.Certificate, 1)
			x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
			if err != nil {
				return false
			}
			return reflect.DeepEqual(oidcServerCert, x509Cert)
		}, 10*time.Second, 100*time.Millisecond, "Failed to assert updated certificate")
	})

	t.Run("delete cert files start error log loop", func(t *testing.T) {
		removeFile(t, tmpDir+keyFilePath)

		// Assert error logs that will keep triggering until the key is created again.
		errLogs := map[time.Time]struct{}{}
		require.Eventuallyf(t, func() bool {
			for _, entry := range logHook.AllEntries() {
				if entry.Level == logrus.ErrorLevel && strings.Contains(entry.Message, fmt.Sprintf("Failed to get file info, file path %q does not exist anymore; please check if the path is correct", tmpDir+keyFilePath)) {
					errLogs[entry.Time] = struct{}{}
				}
			}
			return len(errLogs) >= 5
		}, 10*time.Second, 10*time.Millisecond, "Failed to assert non-existing key error logs")

		removeFile(t, tmpDir+certFilePath)

		// Assert error logs that will keep triggering until the cert is created again.
		errLogs = map[time.Time]struct{}{}
		require.Eventuallyf(t, func() bool {
			for _, entry := range logHook.AllEntries() {
				if entry.Level == logrus.ErrorLevel && strings.Contains(entry.Message, fmt.Sprintf("Failed to get file info, file path %q does not exist anymore; please check if the path is correct", tmpDir+certFilePath)) {
					errLogs[entry.Time] = struct{}{}
				}
			}
			return len(errLogs) >= 5
		}, 10*time.Second, 10*time.Millisecond, "Failed to assert non-existing cert error logs")

		writeFile(t, tmpDir+keyFilePath, oidcServerKeyPem)

		writeFile(t, tmpDir+certFilePath, oidcServerCertPem)

		require.Eventuallyf(t, func() bool {
			return logHook.LastEntry().Message == "Loaded provided certificate with success"
		}, 10*time.Second, 10*time.Millisecond, "Failed to assert updated certificate")

		cert, err := tlsConfig.GetCertificate(chInfo)
		require.NoError(t, err)
		require.Len(t, cert.Certificate, 1)
		x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
		require.NoError(t, err)
		require.Equal(t, oidcServerCert, x509Cert)
	})

	t.Run("stop file watcher when context is canceled", func(t *testing.T) {
		cancelFn()

		require.Eventuallyf(t, func() bool {
			lastEntry := logHook.LastEntry()
			return lastEntry.Level == logrus.InfoLevel && lastEntry.Message == "Stopping file watcher"
		}, 10*time.Second, 10*time.Millisecond, "Failed to assert file watcher stop log")
	})
}

func writeFile(t *testing.T, name string, data []byte) {
	err := os.WriteFile(name, data, 0600)
	require.NoError(t, err)
}

func removeFile(t *testing.T, name string) {
	err := os.Remove(name)
	require.NoError(t, err)
}

func assertFileDontExist(t *testing.T, filePath string, err error) {
	switch runtime.GOOS {
	case "windows":
		require.EqualError(t, err, "failed to load certificate: open "+filePath+": The system cannot find the file specified.")
	default:
		require.EqualError(t, err, "failed to load certificate: open "+filePath+": no such file or directory")
	}
}

package agent

import (
	"os"
	"path/filepath"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/sirupsen/logrus"
)

// loadOrCreateP2PIdentity ensures we reuse a persisted libp2p private key when AutoTLS is enabled.
// When the file does not exist a new Ed25519 key is generated and stored with 0400 permissions.
func loadOrCreateP2PIdentity(path string, logger *logrus.Logger) (crypto.PrivKey, error) {
	if path == "" {
		priv, _, err := crypto.GenerateKeyPair(crypto.Ed25519, -1)
		return priv, err
	}

	if _, err := os.Stat(path); err == nil {
		return readP2PIdentity(path)
	} else if os.IsNotExist(err) {
		logger.Infof("Generating peer identity in %s", path)
		return generateP2PIdentity(path)
	} else {
		return nil, err
	}
}

func readP2PIdentity(path string) (crypto.PrivKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return crypto.UnmarshalPrivateKey(data)
}

func generateP2PIdentity(path string) (crypto.PrivKey, error) {
	priv, _, err := crypto.GenerateKeyPair(crypto.Ed25519, 0)
	if err != nil {
		return nil, err
	}

	data, err := crypto.MarshalPrivateKey(priv)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0o400); err != nil {
		return nil, err
	}

	return priv, nil
}

package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/pachyderm/pachyderm/v2/src/client"
	log "github.com/sirupsen/logrus"
)

var errSkipped = errors.New("skipped step")

// skipIfNotExist loads the contents of a config file, or returns
// an errSkipped if the file doesn't exist.
func skipIfNotExist(path string) ([]byte, error) {
	fullPath := filepath.Join(configRoot, path)
	data, err := ioutil.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w - no file %s", errSkipped, fullPath)
		}
		return nil, err
	}
	return data, nil
}

func loadRootToken() ([]byte, error) {
	return skipIfNotExist(rootTokenPath)
}

func loadEnterpriseRootToken() ([]byte, error) {
	return skipIfNotExist(enterpriseRootTokenPath)
}

func loadEnterpriseServerAddress() ([]byte, error) {
	return skipIfNotExist(enterpriseServerAddress)
}

func loadYAML(path string, target interface{}) error {
	data, err := skipIfNotExist(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, &target)
}

func resolveIfEnvVar(v string) (string, error) {
	if strings.HasPrefix(v, "$") {
		val, isset := os.LookupEnv(strings.TrimPrefix(v, "$"))
		if !isset {
			return "", fmt.Errorf("expected environment variable, %s, is not set", strings.TrimPrefix(v, "$"))
		}
		return val, nil
	}
	return v, nil
}

func connectToPach(addr string) *client.APIClient {
	options := make([]client.Option, 0)
	if sslDir, ok := os.LookupEnv("OPENSSL_DIR"); ok {
		options = append(options, client.WithRootCAs(sslDir))
	}
	c, err := client.NewFromURI(addr, options...)
	if err != nil {
		log.WithError(err).Error("failed to connect to pachyderm")
		os.Exit(1)
	}
	return c
}

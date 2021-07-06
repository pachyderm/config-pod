package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/ghodss/yaml"
)

type errSkipped struct {
	msg string
}

func (e errSkipped) Error() string {
	return e.msg
}

// skipIfNotExist loads the contents of a config file, or returns
// an errSkipped if the file doesn't exist.
func skipIfNotExist(path string) ([]byte, error) {
	fullPath := filepath.Join(configRoot, path)
	data, err := ioutil.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errSkipped{fmt.Sprintf("no file %q", fullPath)}
		}
		return nil, err
	}
	return data, nil
}

func loadRootToken() ([]byte, error) {
	return skipIfNotExist(rootTokenPath)
}

func loadYAML(path string, target interface{}) error {
	data, err := skipIfNotExist(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, &target)
}

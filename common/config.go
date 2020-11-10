package common

import (
	"fmt"
	"os"

	"github.com/jinzhu/configor"
)

// LoadConfigFromFile is helper function to load config.
func LoadConfigFromFile(path string, x interface{}) error {

	// pre-checks
	if fileInfo, err := os.Stat(path); err != nil {
		return fmt.Errorf("cannot get stat of configuration file. [path = \"%v\"] (err = \"%v\")", path, err)
	} else if !fileInfo.Mode().IsRegular() {
		return fmt.Errorf("invalid irregular configuration file. [path = \"%v\"]", path)
	}

	if err := configor.New(&configor.Config{
		Debug: false,
	}).Load(x, path); err != nil {
		return fmt.Errorf("failed to load configuration. (err = \"%v\")", err)
	}

	return nil
}

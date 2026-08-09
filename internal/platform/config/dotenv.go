package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

func LoadDotEnv(filename string) error {
	if err := godotenv.Load(filename); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("load dotenv %q: %w", filename, err)
	}
	return nil
}

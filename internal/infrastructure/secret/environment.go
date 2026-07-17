package secret

import (
	"fmt"
	"os"
)

type Environment struct{}

func (Environment) Resolve(reference string) (string, error) {
	value, ok := os.LookupEnv(reference)
	if !ok || value == "" {
		return "", fmt.Errorf("credential environment variable %q is not set", reference)
	}
	return value, nil
}

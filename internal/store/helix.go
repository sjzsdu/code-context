package store

import (
	"fmt"
	"os"
	"strings"
)

func NewHelixStore(opts HelixOptions) (Store, error) {
	if strings.TrimSpace(opts.URL) == "" {
		return nil, fmt.Errorf("helix store url is required")
	}
	if opts.APIKey == "" && opts.APIKeyEnv != "" {
		opts.APIKey = os.Getenv(opts.APIKeyEnv)
	}
	return nil, ErrHelixStoreNotImplemented
}

package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type Config struct {
	Addr        string
	DatabaseURL string
	Issuer      string
	Audience    string
	JWKSURL     string
}

func Load(lookup func(string) string) (Config, error) {
	cfg := Config{
		Addr:        lookup("AIRLANCE_SERVER_ADDR"),
		DatabaseURL: lookup("AIRLANCE_SERVER_DATABASE_URL"),
		Issuer:      strings.TrimRight(lookup("AIRLANCE_SERVER_OIDC_ISSUER"), "/"),
		Audience:    lookup("AIRLANCE_SERVER_OIDC_AUDIENCE"),
		JWKSURL:     lookup("AIRLANCE_SERVER_OIDC_JWKS_URL"),
	}
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:8090"
	}

	if cfg.JWKSURL == "" && cfg.Issuer != "" {
		cfg.JWKSURL = cfg.Issuer + "/oauth2/jwks"
	}

	var errs []error
	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("AIRLANCE_SERVER_DATABASE_URL is required"))
	}

	if cfg.Audience == "" {
		errs = append(errs, errors.New("AIRLANCE_SERVER_OIDC_AUDIENCE is required"))
	}

	if parsed, err := url.Parse(cfg.Issuer); err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		errs = append(errs, errors.New("AIRLANCE_SERVER_OIDC_ISSUER must be an https URL"))
	}

	if parsed, err := url.Parse(cfg.JWKSURL); err != nil || parsed.Host == "" {
		errs = append(errs, errors.New("AIRLANCE_SERVER_OIDC_JWKS_URL must be a URL"))
	}

	if err := errors.Join(errs...); err != nil {
		return Config{}, fmt.Errorf("configuration: %w", err)
	}

	return cfg, nil
}

package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type Config struct {
	Addr         string
	DatabaseURL  string
	Issuer       string
	Audience     string
	JWKSURL      string
	WireAuthKeys map[uint32]ed25519.PrivateKey
	S3           S3
	MeiliURL     string
	MeiliKey     string
}

type S3 struct {
	Endpoint  string
	PublicURL string
	AccessKey string
	SecretKey string
	Bucket    string
}

func loadS3(lookup func(string) string) (S3, []error) {
	cfg := S3{
		Endpoint:  lookup("AIRLANCE_SERVER_S3_ENDPOINT"),
		PublicURL: lookup("AIRLANCE_SERVER_S3_PUBLIC_URL"),
		AccessKey: lookup("AIRLANCE_SERVER_S3_ACCESS_KEY"),
		SecretKey: lookup("AIRLANCE_SERVER_S3_SECRET_KEY"),
		Bucket:    lookup("AIRLANCE_SERVER_S3_BUCKET"),
	}

	var errs []error

	for name, value := range map[string]string{
		"AIRLANCE_SERVER_S3_ENDPOINT":   cfg.Endpoint,
		"AIRLANCE_SERVER_S3_PUBLIC_URL": cfg.PublicURL,
	} {
		if parsed, err := url.Parse(value); err != nil || parsed.Host == "" {
			errs = append(errs, fmt.Errorf("%s must be a URL", name))
		}
	}

	if cfg.AccessKey == "" || cfg.SecretKey == "" || cfg.Bucket == "" {
		errs = append(errs, errors.New("AIRLANCE_SERVER_S3_ACCESS_KEY, _SECRET_KEY and _BUCKET are required"))
	}

	return cfg, errs
}

func LoadWireAuthKeys(raw string) (map[uint32]ed25519.PrivateKey, error) {
	keys := map[uint32]ed25519.PrivateKey{}
	for _, entry := range strings.Split(raw, ",") {
		idText, seedText, found := strings.Cut(strings.TrimSpace(entry), ":")
		if !found {
			return nil, errors.New("AIRLANCE_SERVER_WIREAUTH_KEYS must be ID:BASE64SEED, separated by commas")
		}

		id, err := strconv.ParseUint(idText, 10, 32)
		if err != nil {
			return nil, errors.New("AIRLANCE_SERVER_WIREAUTH_KEYS: a key id must be a number")
		}

		seed, err := base64.StdEncoding.DecodeString(seedText)
		if err != nil || len(seed) != ed25519.SeedSize {
			return nil, errors.New("AIRLANCE_SERVER_WIREAUTH_KEYS: a seed must be 32 bytes of base64")
		}

		keys[uint32(id)] = ed25519.NewKeyFromSeed(seed)
	}

	return keys, nil
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

	keys, err := LoadWireAuthKeys(lookup("AIRLANCE_SERVER_WIREAUTH_KEYS"))
	if err != nil {
		errs = append(errs, err)
	}

	cfg.WireAuthKeys = keys

	s3, s3Errs := loadS3(lookup)
	cfg.S3 = s3
	errs = append(errs, s3Errs...)

	cfg.MeiliURL = lookup("AIRLANCE_SERVER_MEILI_URL")
	cfg.MeiliKey = lookup("AIRLANCE_SERVER_MEILI_KEY")

	if parsed, err := url.Parse(cfg.MeiliURL); err != nil || parsed.Host == "" {
		errs = append(errs, errors.New("AIRLANCE_SERVER_MEILI_URL must be a URL"))
	}

	if cfg.MeiliKey == "" {
		errs = append(errs, errors.New("AIRLANCE_SERVER_MEILI_KEY is required"))
	}

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

package objectstore

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const region = "us-east-1"

type Config struct {
	Endpoint  string
	PublicURL string
	AccessKey string
	SecretKey string
	Bucket    string
}

type Store struct {
	internal *minio.Client
	public   *minio.Client
	bucket   string
}

func New(cfg Config) (*Store, error) {
	internal, err := newClient(cfg.Endpoint, cfg)
	if err != nil {
		return nil, fmt.Errorf("internal endpoint: %w", err)
	}

	public, err := newClient(cfg.PublicURL, cfg)
	if err != nil {
		return nil, fmt.Errorf("public url: %w", err)
	}

	return &Store{internal: internal, public: public, bucket: cfg.Bucket}, nil
}

func newClient(address string, cfg Config) (*minio.Client, error) {
	parsed, err := url.Parse(address)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	client, err := minio.New(parsed.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: parsed.Scheme == "https",
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}

	return client, nil
}

func (s *Store) EnsureBucket(ctx context.Context) error {
	exists, err := s.internal.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check bucket: %w", err)
	}

	if exists {
		return nil
	}

	if err := s.internal.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: region}); err != nil {
		return fmt.Errorf("create bucket: %w", err)
	}

	return nil
}

func (s *Store) UploadURL(ctx context.Context, id string, size int64, ttl time.Duration) (string, error) {
	headers := http.Header{"Content-Length": {strconv.FormatInt(size, 10)}}

	signed, err := s.public.PresignHeader(ctx, http.MethodPut, s.bucket, id, ttl, url.Values{}, headers)
	if err != nil {
		return "", fmt.Errorf("presign upload: %w", err)
	}

	return signed.String(), nil
}

func (s *Store) DownloadURL(ctx context.Context, id string, ttl time.Duration) (string, error) {
	signed, err := s.public.PresignedGetObject(ctx, s.bucket, id, ttl, url.Values{})
	if err != nil {
		return "", fmt.Errorf("presign download: %w", err)
	}

	return signed.String(), nil
}

func (s *Store) Put(ctx context.Context, id string, data []byte) error {
	_, err := s.internal.PutObject(
		ctx, s.bucket, id, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: "image/jpeg"},
	)
	if err != nil {
		return fmt.Errorf("put object: %w", err)
	}

	return nil
}

func (s *Store) Remove(ctx context.Context, id string) error {
	if err := s.internal.RemoveObject(ctx, s.bucket, id, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("remove object: %w", err)
	}

	return nil
}

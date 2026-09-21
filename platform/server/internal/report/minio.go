package report

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ObjectStore is the MinIO surface reports need: upload from the runner
// (python side), presign and fetch from the API.
type ObjectStore interface {
	PresignGET(ctx context.Context, bucket, key string, expiry time.Duration) (string, error)
	Open(ctx context.Context, bucket, key string) (io.ReadCloser, int64, error)
}

type minioStore struct {
	client *minio.Client
}

// NewMinioStore builds the MinIO client; endpoint is host:port without scheme.
func NewMinioStore(endpoint, accessKey, secretKey string, secure bool) (ObjectStore, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
	})
	if err != nil {
		return nil, fmt.Errorf("minio client: %w", err)
	}
	return &minioStore{client: client}, nil
}

func (m *minioStore) PresignGET(ctx context.Context, bucket, key string, expiry time.Duration) (string, error) {
	reqParams := url.Values{}
	presigned, err := m.client.PresignedGetObject(ctx, bucket, key, expiry, reqParams)
	if err != nil {
		return "", fmt.Errorf("presign %s/%s: %w", bucket, key, err)
	}
	return presigned.String(), nil
}

func (m *minioStore) Open(ctx context.Context, bucket, key string) (io.ReadCloser, int64, error) {
	obj, err := m.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, fmt.Errorf("open %s/%s: %w", bucket, key, err)
	}
	info, err := obj.Stat()
	if err != nil {
		obj.Close()
		return nil, 0, fmt.Errorf("stat %s/%s: %w", bucket, key, err)
	}
	return obj, info.Size, nil
}

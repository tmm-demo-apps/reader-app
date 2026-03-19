package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func main() {
	minioEndpoint := requireEnv("MINIO_ENDPOINT")
	minioAccessKey := requireEnv("MINIO_ACCESS_KEY")
	minioSecretKey := requireEnv("MINIO_SECRET_KEY")
	bucketName := getEnv("MINIO_BUCKET", "books-epub")
	epubsDir := getEnv("EPUBS_DIR", "/epubs")

	ctx := context.Background()

	client, err := minio.New(minioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(minioAccessKey, minioSecretKey, ""),
		Secure: false,
	})
	if err != nil {
		log.Fatalf("Failed to create MinIO client: %v", err)
	}
	log.Printf("Connected to MinIO at %s", minioEndpoint)

	exists, err := client.BucketExists(ctx, bucketName)
	if err != nil {
		log.Fatalf("Failed to check bucket: %v", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{}); err != nil {
			log.Fatalf("Failed to create bucket %s: %v", bucketName, err)
		}
		policy := `{
			"Version": "2012-10-17",
			"Statement": [{
				"Effect": "Allow",
				"Principal": {"AWS": ["*"]},
				"Action": ["s3:GetObject"],
				"Resource": ["arn:aws:s3:::` + bucketName + `/*"]
			}]
		}`
		if err := client.SetBucketPolicy(ctx, bucketName, policy); err != nil {
			log.Printf("Warning: could not set bucket policy: %v", err)
		}
		log.Printf("Created bucket: %s", bucketName)
	}

	entries, err := os.ReadDir(epubsDir)
	if err != nil {
		log.Fatalf("Failed to read epubs directory %s: %v", epubsDir, err)
	}

	var seeded, skipped, failed int

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		idStr := entry.Name()
		id, err := strconv.Atoi(idStr)
		if err != nil {
			continue
		}

		filename := fmt.Sprintf("pg%d.epub", id)
		localPath := filepath.Join(epubsDir, idStr, filename)
		minioPath := fmt.Sprintf("%d/%s", id, filename)

		if _, err := os.Stat(localPath); os.IsNotExist(err) {
			continue
		}

		_, err = client.StatObject(ctx, bucketName, minioPath, minio.StatObjectOptions{})
		if err == nil {
			skipped++
			continue
		}

		data, err := os.ReadFile(localPath)
		if err != nil {
			log.Printf("Failed to read %s: %v", localPath, err)
			failed++
			continue
		}

		_, err = client.PutObject(ctx, bucketName, minioPath, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{
			ContentType: "application/epub+zip",
		})
		if err != nil {
			log.Printf("Failed to upload %s: %v", minioPath, err)
			failed++
			continue
		}

		seeded++
		if seeded%10 == 0 {
			log.Printf("Progress: %d seeded, %d skipped so far...", seeded, skipped)
		}
	}

	total := seeded + skipped + failed
	log.Printf("Done: %d/%d seeded, %d already cached, %d failed", seeded, total, skipped, failed)

	if failed > 0 {
		os.Exit(1)
	}
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("Required environment variable %s is not set", key)
	}
	return strings.TrimSpace(v)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

package lsvd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func TestS3(t *testing.T) {
	monoRead := ulid.DefaultEntropy()

	host := os.Getenv("S3_URL")
	if host == "" {
		t.Skip("no s3 url provided to test with")
	}

	access := "admin"
	secret := "password"

	log := slog.Default()

	ctx := context.Background()

	cfg, err := config.LoadDefaultConfig(ctx, func(lo *config.LoadOptions) error {
		lo.Region = "us-east-1"
		lo.Credentials = credentials.NewStaticCredentialsProvider(access, secret, "")
		return nil
	})

	require.NoError(t, err)

	sc := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true
		o.BaseEndpoint = &host
	})

	bucketName := "lsvdtest"

	sc.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: &bucketName,
	})
	//require.NoError(t, err)

	t.Run("can read a segment", func(t *testing.T) {
		r := require.New(t)

		seg, err := ulid.New(ulid.Now(), monoRead)
		r.NoError(err)

		objName := "segments/segment." + ulid.ULID(seg).String()

		_, err = sc.PutObject(ctx, &s3.PutObjectInput{
			Bucket: &bucketName,
			Key:    &objName,
			Body:   strings.NewReader("this is a segment"),
		})
		r.NoError(err)

		defer sc.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: &bucketName,
			Key:    &objName,
		})

		s, err := NewS3Access(log, host, bucketName, cfg)
		r.NoError(err)

		or, err := s.OpenSegment(ctx, "default", SegmentId(seg))
		r.NoError(err)

		buf := make([]byte, 1024)

		n, err := or.ReadAt(buf, 0)
		r.NoError(err)

		r.Equal("this is a segment", string(buf[:n]))
	})

	t.Run("can write a segment", func(t *testing.T) {
		r := require.New(t)

		seg, err := ulid.New(ulid.Now(), monoRead)
		r.NoError(err)

		objName := "segment." + ulid.ULID(seg).String()

		defer sc.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: &bucketName,
			Key:    &objName,
		})

		s, err := NewS3Access(log, host, bucketName, cfg)
		r.NoError(err)

		w, err := s.WriteSegment(ctx, SegmentId(seg))
		r.NoError(err)

		fmt.Fprintln(w, "this is a segment")

		r.NoError(w.Close())

		or, err := s.OpenSegment(ctx, "default", SegmentId(seg))
		r.NoError(err)

		buf := make([]byte, 1024)

		n, err := or.ReadAt(buf, 0)
		r.NoError(err)

		r.Equal("this is a segment\n", string(buf[:n]))
	})

	t.Run("can remove a segment", func(t *testing.T) {
		r := require.New(t)

		seg, err := ulid.New(ulid.Now(), monoRead)
		r.NoError(err)

		objName := "segment." + ulid.ULID(seg).String()

		_, err = sc.PutObject(ctx, &s3.PutObjectInput{
			Bucket: &bucketName,
			Key:    &objName,
			Body:   strings.NewReader("this is a segment"),
		})
		r.NoError(err)

		defer sc.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: &bucketName,
			Key:    &objName,
		})

		s, err := NewS3Access(log, host, bucketName, cfg)
		r.NoError(err)

		err = s.RemoveSegment(ctx, SegmentId(seg))
		r.NoError(err)

		_, err = s.OpenSegment(ctx, "default", SegmentId(seg))
		r.Error(err)
	})

	t.Run("lists segments", func(t *testing.T) {
		r := require.New(t)

		var expected []SegmentId

		s, err := NewS3Access(log, host, bucketName, cfg)
		r.NoError(err)

		objName := "volumes/default/segments"

		defer sc.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: &bucketName,
			Key:    &objName,
		})

		for i := 0; i < 3; i++ {
			seg, err := ulid.New(ulid.Now(), monoRead)
			r.NoError(err)

			expected = append(expected, SegmentId(seg))

			err = s.AppendToSegments(ctx, "default", SegmentId(seg))
			r.NoError(err)
		}

		segs, err := s.ListSegments(ctx, "default")
		r.NoError(err)

		r.Equal(expected, segs)
	})

	t.Run("accesses metadata", func(t *testing.T) {
		r := require.New(t)

		s, err := NewS3Access(log, host, bucketName, cfg)
		r.NoError(err)

		key := "head"

		defer s.sc.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: &bucketName,
			Key:    &key,
		})

		w, err := s.WriteMetadata(ctx, "default", key)
		r.NoError(err)

		_, err = fmt.Fprintln(w, "this is metadata")
		r.NoError(err)
		r.NoError(w.Close())

		mr, err := s.ReadMetadata(ctx, "default", key)
		r.NoError(err)

		data, err := io.ReadAll(mr)
		r.NoError(err)

		r.Equal("this is metadata\n", string(data))
	})
}

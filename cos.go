package taskobserver

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/tencentyun/cos-go-sdk-v5"
)

type cosClient struct {
	client *cos.Client
	bucket string
	region string
}

func newCOSClient(bucket, region, secretID, secretKey string) *cosClient {
	u, _ := url.Parse(fmt.Sprintf("https://%s.cos.%s.myqcloud.com", bucket, region))
	c := cos.NewClient(&cos.BaseURL{BucketURL: u}, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  secretID,
			SecretKey: secretKey,
		},
	})
	return &cosClient{client: c, bucket: bucket, region: region}
}

func (c *cosClient) put(key, contentType string, data []byte, encoding string) error {
	opts := &cos.ObjectPutOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
			ContentType: contentType,
		},
	}
	if encoding != "" {
		opts.ObjectPutHeaderOptions.ContentEncoding = encoding
	}
	_, err := c.client.Object.Put(context.Background(), key, bytes.NewReader(data), opts)
	return err
}

func (c *cosClient) putString(key, contentType, content string) error {
	return c.put(key, contentType, []byte(content), "")
}

func (c *cosClient) putGzip(key string, lines []string) (string, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	for _, l := range lines {
		io.WriteString(gz, l+"\n")
	}
	gz.Close()
	if err := c.put(key, "text/plain; charset=utf-8", buf.Bytes(), "gzip"); err != nil {
		return "", err
	}
	return key, nil
}

func (c *cosClient) getJSON(path string) ([]byte, error) {
	u := fmt.Sprintf("https://%s.cos.%s.myqcloud.com/%s", c.bucket, c.region, strings.TrimPrefix(path, "/"))
	resp, err := http.Get(u)
	if err != nil || resp.StatusCode == 404 {
		return nil, nil
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	return buf.Bytes(), nil
}

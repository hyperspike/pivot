package spool

import (
	"context"
	"crypto/tls"
	"net/http"
	"time"

	"code.gitea.io/sdk/gitea"
)

type Client struct {
	*gitea.Client
	ctx context.Context
}

func NewClient(ctx context.Context, url, user, pass string) (*Client, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	c := &Client{}
	var err error

	c.Client, err = gitea.NewClient(url, gitea.SetContext(ctx), gitea.SetBasicAuth(user, pass), gitea.SetHTTPClient(&http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}))
	if err != nil {
		return nil, err
	}
	return c, nil
}

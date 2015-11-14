package zerotier

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	token string

	*http.Client
}

func NewClient(token string) (*Client, error) {
	httpc := &http.Client{}

	return &Client{
		token: token,

		Client: httpc,
	}, nil
}

func (c *Client) AuthorizeMember(network, member string) error {
	loc := fmt.Sprintf("https://my.zerotier.com/api/network/%s/member/%s", network, member)
	req, err := http.NewRequest("POST", loc, strings.NewReader(fmt.Sprintf(`{"config":{"authorized": true}, "annot": {"description": "joined %s"}}`, time.Now().Format("02-01-2006 (15:04)"))))
	if err != nil {
		return fmt.Errorf("Failed to create request: %s", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()
	if resp.StatusCode > 299 {
		return fmt.Errorf("Failed to update member details '%v': %s", req.Header, resp.Status)
	}

	return nil
}

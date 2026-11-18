package herdr

import "context"

// Notify displays an error notification through the host CLI.
func (c *Client) Notify(ctx context.Context, title, body string) error {
	return c.Call(ctx, nil, "notification", "show", title, "--body", body,
		"--position", "top-right", "--sound", "request")
}

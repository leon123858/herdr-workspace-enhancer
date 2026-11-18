package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"herdr-workspace-enhancer/repository/herdr"
)

// Finish releases dependencies and reports any command or shutdown failure once.
func (c *Controller) Finish(commandErr error, diagnostics io.Writer) error {
	err := errors.Join(commandErr, c.Shutdown())
	if err == nil {
		return nil
	}
	host := c.host
	if host == nil {
		// Validation can fail before the lazy client provider is resolved.
		binary := os.Getenv("HERDR_BIN_PATH")
		if binary == "" || os.Getenv("HERDR_SOCKET_PATH") == "" {
			return err
		}
		host = herdr.New(binary)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if notifyErr := host.Notify(ctx, "Workspace Enhancer failed", err.Error()); notifyErr != nil {
		fmt.Fprintf(diagnostics, "Could not notify Herdr: %v\n", notifyErr)
	}
	return err
}

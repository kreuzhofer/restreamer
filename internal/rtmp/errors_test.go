package rtmp

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestAcceptFailureSanitizesErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("peer included secret-key: %w", io.EOF), "connection_closed"},
		{fmt.Errorf("peer included secret-key: %w", syscall.ECONNRESET), "connection_closed"},
		{fmt.Errorf("peer included secret-key: %w", os.ErrDeadlineExceeded), "timeout"},
		{errors.New("invalid AMF body with secret-key"), "protocol_error"},
	} {
		err := acceptFailure("connect_command", tc.err)
		if err.Stage != "connect_command" || err.Reason != tc.want || strings.Contains(err.Error(), "secret-key") {
			t.Fatalf("unsafe/incorrect diagnostic: %v", err)
		}
		if !errors.Is(err, tc.err) {
			t.Fatal("lost underlying error")
		}
	}
}

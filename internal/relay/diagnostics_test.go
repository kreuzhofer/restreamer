package relay

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

type logBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *logBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }

func TestInputDiagnosticsDistinguishFailuresWithoutLeakingKeys(t *testing.T) {
	var logs logBuffer
	_, address := startRelayWithLogger(t, []config.Target{{Name: "disabled"}}, slog.New(slog.NewJSONHandler(&logs, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// An application path can itself contain a misplaced key. Never log it.
	if c, _, err := rtmp.Dial(ctx, strings.TrimSuffix(address, "live")+"misplaced-secret-key", "input-key-1234567890"); err == nil {
		c.Net.Close()
		t.Fatal("wrong application accepted")
	}
	eventually(t, func() bool { return strings.Contains(logs.String(), `"reason":"application_mismatch"`) })
	if c, _, err := rtmp.Dial(ctx, address, "wrong-secret-key"); err == nil {
		c.Net.Close()
		t.Fatal("wrong key accepted")
	}
	eventually(t, func() bool { return strings.Contains(logs.String(), `"reason":"stream_key_mismatch"`) })
	c := publishInput(t, address)
	defer c.Net.Close()
	if second, _, err := rtmp.Dial(ctx, address, "input-key-1234567890"); err == nil {
		second.Net.Close()
		t.Fatal("second publisher accepted")
	}
	eventually(t, func() bool { return strings.Contains(logs.String(), `"reason":"publisher_already_connected"`) })
	n, err := net.Dial("tcp", strings.TrimSuffix(strings.TrimPrefix(address, "rtmp://"), "/live"))
	if err != nil {
		t.Fatal(err)
	}
	n.Close()
	eventually(t, func() bool {
		return strings.Contains(logs.String(), `"stage":"handshake","reason":"connection_closed"`)
	})
	for _, secret := range []string{"input-key-1234567890", "wrong-secret-key", "misplaced-secret-key"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("diagnostics leaked a key")
		}
	}
}

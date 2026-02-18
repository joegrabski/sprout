package sprout

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var debugLogMu sync.Mutex

func debugLogFilePath() string {
	if v := strings.TrimSpace(os.Getenv("SPROUT_DEBUG_LOG")); v != "" {
		return v
	}
	return filepath.Join(os.TempDir(), "sprout-debug.log")
}

func debugLogf(format string, args ...any) {
	path := debugLogFilePath()
	if strings.TrimSpace(path) == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}

	line := fmt.Sprintf("%s %s\n", time.Now().Format(time.RFC3339Nano), fmt.Sprintf(format, args...))

	debugLogMu.Lock()
	defer debugLogMu.Unlock()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_, _ = f.WriteString(line)
	_ = f.Close()
}

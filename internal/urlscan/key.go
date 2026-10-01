package urlscan

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const KeychainService = "urlscan.io"

func Key() (string, error) {
	if k := strings.TrimSpace(os.Getenv("URLSCAN_API_KEY")); k != "" {
		return k, nil
	}
	if runtime.GOOS == "darwin" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "security", "find-generic-password",
			"-s", KeychainService, "-w").Output()
		if err == nil {
			if k := strings.TrimSpace(string(out)); k != "" {
				return k, nil
			}
		}
	}
	return "", fmt.Errorf("no urlscan.io API key: set URLSCAN_API_KEY.\n" +
		"Create one under Settings & API in your account at https://urlscan.io\n" +
		"On macOS it can instead be stored in the keychain under the service name " + KeychainService)
}

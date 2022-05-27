package thirdpartyagent

import (
	"context"
	"os"

	"github.com/alibaba/ilogtail/pkg/logger"
)

func isPathExist(p string) (bool, error) {
	_, err := os.Stat(p)
	switch {
	case err == nil:
		return true, nil
	case os.IsNotExist(err):
		return false, nil
	default:
		return false, err
	}
}

// shouldCreatePath returns true if p is not existing and no error when stat.
func shouldCreatePath(p string) bool {
	ret, err := isPathExist(p)
	if err == nil {
		return !ret
	}
	logger.Warningf(context.Background(), "SERVICE_AGENT_RUNTIME_ALARM",
		"stat path %v err: %v", p, err)
	return false
}

// makeSureDirectoryExist returns true if the directory is created just now.
func makeSureDirectoryExist(p string) (bool, error) {
	if !shouldCreatePath(p) {
		return false, nil
	}
	return true, os.MkdirAll(p, 0750)
}

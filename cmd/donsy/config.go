package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tinker-works/donsy/internal/adapters/instancelock"
)

const (
	legacyRootName   = "go-merge"
	rootName         = "donsy"
	stagingRootName  = "donsy-migration"
	tokenFileName    = "http-token"
	endpointFileName = "http-endpoint"
	defaultEndpoint  = "http://127.0.0.1:8337"
)

type endpoint struct {
	host string
	port int
}

func parseEndpoint(value string) (endpoint, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return endpoint{}, fmt.Errorf("parse HTTP endpoint: %w", err)
	}
	if !parsed.IsAbs() || parsed.Scheme != "http" {
		return endpoint{}, fmt.Errorf("HTTP endpoint must be an absolute http URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		return endpoint{}, fmt.Errorf("HTTP endpoint must not contain user info, a query, fragment, or path")
	}
	if parsed.Host == "" {
		return endpoint{}, fmt.Errorf("HTTP endpoint must include a loopback host and port")
	}
	host, portText, err := net.SplitHostPort(parsed.Host)
	if err != nil || host == "" || portText == "" {
		return endpoint{}, fmt.Errorf("HTTP endpoint must include an explicit numeric port")
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		return endpoint{}, fmt.Errorf("HTTP endpoint port must be from 0 through 65535")
	}
	host = strings.ToLower(host)
	if host == "localhost" {
		return endpoint{host: host, port: int(port)}, nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return endpoint{}, fmt.Errorf("HTTP endpoint host must be localhost or a loopback IP address")
	}
	return endpoint{host: ip.String(), port: int(port)}, nil
}

func (e endpoint) String() string {
	return "http://" + net.JoinHostPort(e.host, strconv.Itoa(e.port))
}

func (e endpoint) listenAddress() string {
	return net.JoinHostPort(e.host, strconv.Itoa(e.port))
}

func (e endpoint) withListener(listener net.Listener) (endpoint, error) {
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return endpoint{}, fmt.Errorf("listener has unexpected address %q", listener.Addr())
	}
	e.port = address.Port
	return e, nil
}

func prepareRoot(configDir string) (string, *instancelock.Lock, error) {
	legacy := filepath.Join(configDir, legacyRootName)
	root := filepath.Join(configDir, rootName)
	staging := filepath.Join(configDir, stagingRootName)

	legacyExists, err := pathExists(legacy)
	if err != nil {
		return "", nil, err
	}
	rootExists, err := pathExists(root)
	if err != nil {
		return "", nil, err
	}
	stagingExists, err := pathExists(staging)
	if err != nil {
		return "", nil, err
	}
	if stagingExists {
		return "", nil, fmt.Errorf("migration staging root %q exists; recover %q before starting donsy", staging, staging)
	}
	if legacyExists && rootExists {
		return "", nil, fmt.Errorf("both legacy root %q and donsy root %q exist; recover these paths before starting donsy", legacy, root)
	}
	if legacyExists {
		lock, err := instancelock.Acquire(filepath.Join(legacy, "go-merge.lock"))
		if err != nil {
			return "", nil, err
		}
		if err := os.Chmod(filepath.Join(legacy, "go-merge.lock"), 0o600); err != nil {
			_ = lock.Release()
			return "", nil, err
		}
		if err := os.Rename(legacy, root); err != nil {
			_ = lock.Release()
			return "", nil, fmt.Errorf("migrate daemon root %q to %q: %w", legacy, root, err)
		}
		return root, lock, nil
	}
	if !rootExists {
		if err := os.MkdirAll(root, 0o700); err != nil {
			return "", nil, err
		}
	}
	lock, err := instancelock.Acquire(filepath.Join(root, "go-merge.lock"))
	if err != nil {
		return "", nil, err
	}
	if err := os.Chmod(filepath.Join(root, "go-merge.lock"), 0o600); err != nil {
		_ = lock.Release()
		return "", nil, err
	}
	return root, lock, nil
}

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func configuredEndpoint(root, supplied string, suppliedSet bool) (endpoint, error) {
	if suppliedSet {
		if supplied == "" {
			return endpoint{}, fmt.Errorf("--endpoint must not be empty")
		}
		return parseEndpoint(supplied)
	}
	contents, err := os.ReadFile(filepath.Join(root, endpointFileName))
	if err != nil && !os.IsNotExist(err) {
		return endpoint{}, err
	}
	value := strings.TrimSpace(string(contents))
	if value == "" {
		value = defaultEndpoint
	}
	return parseEndpoint(value)
}

func persistEndpoint(root string, endpoint endpoint) error {
	path := filepath.Join(root, endpointFileName)
	if err := os.WriteFile(path, []byte(endpoint.String()), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func configuredToken(root, supplied string, suppliedSet bool) (string, error) {
	if suppliedSet {
		if supplied == "" {
			return "", fmt.Errorf("--token must not be empty")
		}
		return supplied, nil
	}
	path := filepath.Join(root, tokenFileName)
	token, err := os.ReadFile(path)
	if err == nil && len(token) > 0 {
		if err := os.Chmod(path, 0o600); err != nil {
			return "", err
		}
		return string(token), nil
	}
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token = []byte(hex.EncodeToString(bytes))
	if err := os.WriteFile(path, token, 0o600); err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", err
	}
	return string(token), nil
}

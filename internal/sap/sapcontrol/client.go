// Package sapcontrol wraps the sapcontrol command line tool. Every SAP
// platform ships the same tool with the same output format, which makes it
// the universal control API for sapkernel.
package sapcontrol

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/exec"
)

// Client talks to one sapstartsrv instance.
type Client struct {
	Runner  exec.Runner
	Path    string // sapcontrol executable
	Nr      string // instance number, two digits
	Host    string // optional remote host (-host)
	Timeout time.Duration
}

// Error is a FAIL response from sapcontrol.
type Error struct {
	Function string
	Message  string
	ExitCode int
}

func (e *Error) Error() string {
	return fmt.Sprintf("sapcontrol %s: %s (exit %d)", e.Function, e.Message, e.ExitCode)
}

// IsConnRefused reports whether err means sapstartsrv is not running.
func IsConnRefused(err error) bool {
	var e *Error
	if !errors.As(err, &e) {
		return false
	}
	return strings.Contains(e.Message, "NIECONN_REFUSED") || strings.Contains(e.Message, "Connection refused")
}

// call runs -function fn and returns the body after the OK line.
func (c *Client) call(ctx context.Context, fn string, args ...string) (string, int, error) {
	a := []string{"-nr", c.Nr}
	if c.Host != "" {
		a = append(a, "-host", c.Host)
	}
	a = append(a, "-function", fn)
	a = append(a, args...)
	res, err := c.Runner.Run(ctx, exec.Cmd{Path: c.Path, Args: a, Timeout: c.Timeout})
	if err != nil {
		return "", 0, fmt.Errorf("sapcontrol %s: %w", fn, err)
	}
	body, failMsg, ok := splitResponse(res.Stdout)
	if !ok {
		if failMsg == "" {
			failMsg = strings.TrimSpace(res.Stderr)
		}
		if failMsg == "" {
			failMsg = "unexpected output: " + strings.TrimSpace(res.Stdout)
		}
		return "", res.ExitCode, &Error{Function: fn, Message: failMsg, ExitCode: res.ExitCode}
	}
	return body, res.ExitCode, nil
}

// splitResponse strips the timestamp/function header. sapcontrol prints:
//
//	<blank>
//	20.09.2026 19:30:01
//	GetProcessList
//	OK            (or "FAIL: <message>")
//	<body>
func splitResponse(out string) (body, failMsg string, ok bool) {
	lines := strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
	for i, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case t == "OK":
			return strings.TrimRight(strings.Join(lines[i+1:], "\n"), "\n"), "", true
		case strings.HasPrefix(t, "FAIL"):
			return "", strings.TrimSpace(strings.TrimPrefix(t, "FAIL:")), false
		}
	}
	return "", "", false
}

// GetProcessList returns the instance's processes. sapcontrol exits 3 when
// every process is GREEN and 4 otherwise; both are successful calls.
func (c *Client) GetProcessList(ctx context.Context) ([]Process, error) {
	body, _, err := c.call(ctx, "GetProcessList")
	if err != nil {
		return nil, err
	}
	return ParseProcessList(body), nil
}

// GetSystemInstanceList returns all instances of the system, across hosts.
func (c *Client) GetSystemInstanceList(ctx context.Context) ([]SystemInstance, error) {
	body, _, err := c.call(ctx, "GetSystemInstanceList")
	if err != nil {
		return nil, err
	}
	return ParseSystemInstanceList(body), nil
}

// GetVersionInfo returns version rows for the instance's executables.
func (c *Client) GetVersionInfo(ctx context.Context) ([]VersionInfo, error) {
	body, _, err := c.call(ctx, "GetVersionInfo")
	if err != nil {
		return nil, err
	}
	return ParseVersionInfo(body), nil
}

// ParameterValue reads one profile parameter (e.g. DIR_CT_RUN, dbms/type).
func (c *Client) ParameterValue(ctx context.Context, name string) (string, error) {
	body, _, err := c.call(ctx, "ParameterValue", name)
	if err != nil {
		return "", err
	}
	for _, l := range strings.Split(body, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			return t, nil
		}
	}
	return "", nil
}

// GetInstanceProperties returns property name → value (INSTANCE_NAME, SAPSYSTEMNAME, ...).
func (c *Client) GetInstanceProperties(ctx context.Context) (map[string]string, error) {
	body, _, err := c.call(ctx, "GetInstanceProperties")
	if err != nil {
		return nil, err
	}
	return ParseInstanceProperties(body), nil
}

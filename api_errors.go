package main

import (
	"errors"
	"net"
	"strings"
	"syscall"
)

// qbErrorClass groups API/transport failures for plain-English UI copy.
type qbErrorClass int

const (
	qbErrConnectivity qbErrorClass = iota
	qbErrAuth
	qbErrOther
)

// classifyQBAPIError maps Login/GetTorrents errors to qbErrorClass for user-facing handling.
func classifyQBAPIError(err error) qbErrorClass {
	if err == nil {
		return qbErrOther
	}
	msg := err.Error()

	switch {
	case strings.Contains(msg, "status 403"),
		strings.Contains(msg, "status 401"):
		return qbErrAuth
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return qbErrConnectivity
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return qbErrConnectivity
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return qbErrConnectivity
	}

	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ECONNRESET) {
		return qbErrConnectivity
	}

	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "connection refused"),
		strings.Contains(low, "no such host"),
		strings.Contains(low, "timeout"),
		strings.Contains(low, "timed out"),
		strings.Contains(low, "network is unreachable"),
		strings.Contains(low, "connection reset"),
		strings.Contains(low, "tls: "),
		strings.Contains(low, "certificate"),
		strings.Contains(low, "i/o timeout"),
		strings.Contains(low, "context deadline exceeded"),
		strings.Contains(low, "login request failed"):
		return qbErrConnectivity
	}

	return qbErrOther
}

// apiErrorModalContent returns title, body lines, and whether to auto-retry with countdown for the given error.
func apiErrorModalContent(class qbErrorClass, err error) (title string, body []string, autoRetry bool) {
	switch class {
	case qbErrConnectivity:
		title = "qBittorrent server unreachable"
		body = []string{
			"The app cannot reach your qBittorrent Web UI.",
			"Check: the app is running, the address (host/port) is",
			"correct, firewall/VPN, and HTTP vs HTTPS matches.",
		}
		autoRetry = true
	case qbErrAuth:
		title = "Credentials incorrect"
		body = []string{
			"qBittorrent rejected the username or password.",
			"Edit ~/.config/qbitty/config.json or set QB_USER /",
			"QB_PASS to match your Web UI login, then press r.",
		}
		autoRetry = false
	default:
		title = "Could not talk to qBittorrent"
		body = []string{
			"Something unexpected went wrong. Details:",
			truncateErrLine(err),
		}
		autoRetry = true
	}
	return title, body, autoRetry
}

// truncateErrLine shortens a technical error for one line in the modal.
func truncateErrLine(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) > 100 {
		return s[:97] + "..."
	}
	return s
}

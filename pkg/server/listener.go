package server

import (
	"fmt"
	"net"
)

// FindAvailableListener scans ports starting from startPort up to maxPort on the given host.
// It returns the first successfully bound net.Listener and the chosen port number.
func FindAvailableListener(host string, startPort, maxPort int) (net.Listener, int, error) {
	if startPort <= 0 {
		startPort = 5210
	}
	if maxPort < startPort {
		maxPort = startPort + 100
	}

	for port := startPort; port <= maxPort; port++ {
		addr := fmt.Sprintf("%s:%d", host, port)
		listener, err := net.Listen("tcp", addr)
		if err == nil {
			return listener, port, nil
		}
	}

	return nil, 0, fmt.Errorf("no available ports found in range %d-%d on host %s", startPort, maxPort, host)
}

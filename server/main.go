package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"
)

func main() {
	go listen("0.0.0.0:80")
	go listen("0.0.0.0:81")
	go listenTLS("0.0.0.0:443", "server.crt", "server.key")

	log.Println("listening on :80 and :81")
	select {}
}

func listen(addr string) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		go handle(conn, addr)
	}
}

func handle(conn net.Conn, addr string) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	buf := make([]byte, 64*1024)
	n, err := conn.Read(buf)
	if err != nil || n == 0 {
		return
	}

	raw := buf[:n]
	obsID := randomID()

	// Best-effort parse (NOT authoritative)
	var parsedSummary string
	if req, err := http.ReadRequest(
		bufio.NewReader(bytes.NewReader(raw)),
	); err == nil {
		parsedSummary = fmt.Sprintf(
			"method=%s url=%s host=%s",
			req.Method,
			req.URL.String(),
			req.Host,
		)
	} else {
		parsedSummary = "parse_error=" + err.Error()
	}

	log.Printf(
		"[obs=%s port=%s bytes=%d] %s",
		obsID, addr, len(raw), parsedSummary,
	)

	// Minimal echo response
	body := raw
	if idx := bytes.LastIndex(raw, []byte("\r\n")); idx != -1 {
		info := conn.LocalAddr().String()
		body = append(body[:idx], append(fmt.Appendf(nil, "X-Connection-Info: %s\r\n", info), body[idx:]...)...)
	}
	resp := fmt.Sprintf(
		"HTTP/1.1 200 OK\r\n"+
			"Connection: close\r\n"+
			"Content-Type: application/octet-stream\r\n"+
			"X-Observation-Id: %s\r\n"+
			"X-Observed-Port: %s\r\n"+
			"Content-Length: %d\r\n"+
			"\r\n",
		obsID, addr, len(body),
	)
	conn.Write([]byte(resp))
	conn.Write(body)
}

func listenTLS(addr string, certFile, keyFile string) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		log.Fatal(err)
	}
	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		GetConfigForClient: func(chi *tls.ClientHelloInfo) (*tls.Config, error) {
			log.Printf(
				"[TLS hello] sni=%q alpn=%v",
				chi.ServerName,
				chi.SupportedProtos,
			)
			return nil, nil
		},
	}
	ln, err := tls.Listen("tcp", addr, cfg)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("TLS listening on", addr)
	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		go handleTLS(conn, addr)
	}
}

func handleTLS(conn net.Conn, addr string) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	var sni string
	tlsConn, ok := conn.(*tls.Conn)
	if ok {
		_ = tlsConn.Handshake()
		state := tlsConn.ConnectionState()
		sni = state.ServerName
		log.Printf(
			"[TLS state] version=%x cipher=%x sni=%q",
			state.Version,
			state.CipherSuite,
			state.ServerName,
		)
	}

	buf := make([]byte, 64*1024)
	n, err := conn.Read(buf)
	if err != nil || n == 0 {
		return
	}

	raw := buf[:n]
	obsID := randomID()

	// Optional parse (secondary)
	var parsed string
	if req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(raw))); err == nil {
		parsed = fmt.Sprintf(
			"method=%s url=%s host=%s",
			req.Method,
			req.URL.String(),
			req.Host,
		)
	} else {
		parsed = "parse_error=" + err.Error()
	}

	log.Printf(
		"[TLS obs=%s port=%s bytes=%d] %s",
		obsID, addr, len(raw), parsed,
	)
	if idx := bytes.LastIndex(raw, []byte("\r\n")); idx != -1 {
		info := conn.LocalAddr().String()
		raw = append(raw[:idx], append(fmt.Appendf(nil, "X-Connection-Info: %s\r\nX-TLS-SNI: %s\r\n", info, sni), raw[idx:]...)...)
	}

	resp := fmt.Sprintf(
		"HTTP/1.1 200 OK\r\n"+
			"Connection: close\r\n"+
			"Content-Type: application/octet-stream\r\n"+
			"X-Observation-Id: %s\r\n"+
			"X-Observed-Port: %s\r\n"+
			"Content-Length: %d\r\n"+
			"\r\n",
		obsID, addr, len(raw),
	)

	conn.Write([]byte(resp))
	conn.Write(raw)
}

func randomID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

package main

import (
	"encoding/json"
	"errors"
	"log"
	"net"
)

const (
	discoverProbe    = "media-core?"
	discoverMaxBytes = 1400
)

// serveDiscover listens for LAN UDP discovery probes and replies with helloInfo JSON.
// If addr cannot be bound it logs once and returns nil so the server carries on.
func serveDiscover(addr string) net.PacketConn {
	pc, err := net.ListenPacket("udp4", addr)
	if err != nil {
		log.Printf("discover: %v", err)
		return nil
	}
	go runDiscover(pc, helloInfo)
	return pc
}

func runDiscover(pc net.PacketConn, infoFn func() map[string]any) {
	buf := make([]byte, 2048)
	for {
		n, src, err := pc.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		if n != len(discoverProbe) || string(buf[:n]) != discoverProbe {
			continue
		}
		if !isLocalUDP(src) {
			continue
		}
		if b := discoverReply(infoFn()); len(b) > 0 {
			_, _ = pc.WriteTo(b, src)
		}
	}
}

func isLocalUDP(addr net.Addr) bool {
	ua, ok := addr.(*net.UDPAddr)
	if !ok {
		return false
	}
	ip := ua.IP.To4()
	if ip == nil {
		return false
	}
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()
}

func discoverReply(info map[string]any) []byte {
	b, err := json.Marshal(info)
	if err != nil {
		return nil
	}
	if len(b) < discoverMaxBytes {
		return b
	}
	// Drop "app" metadata first so the reply fits within one UDP packet.
	trimmed := make(map[string]any, len(info))
	for k, v := range info {
		if k != "app" {
			trimmed[k] = v
		}
	}
	b, err = json.Marshal(trimmed)
	if err != nil || len(b) >= discoverMaxBytes {
		return nil
	}
	return b
}

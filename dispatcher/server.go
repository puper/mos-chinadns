//     Copyright (C) 2020, IrineSistiana
//
//     This file is part of mos-chinadns.
//
//     mos-chinadns is free software: you can redistribute it and/or modify
//     it under the terms of the GNU General Public License as published by
//     the Free Software Foundation, either version 3 of the License, or
//     (at your option) any later version.
//
//     mos-chinadns is distributed in the hope that it will be useful,
//     but WITHOUT ANY WARRANTY; without even the implied warranty of
//     MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
//     GNU General Public License for more details.
//
//     You should have received a copy of the GNU General Public License
//     along with this program.  If not, see <https://www.gnu.org/licenses/>.

package dispatcher

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	"github.com/IrineSistiana/mos-chinadns/dispatcher/pool"
)

const (
	serverTimeout = time.Second * 30
)

// StartServer starts mos-chinadns. Will always return a non-nil err.
func (d *Dispatcher) StartServer() error {

	if len(d.config.Bind) == 0 {
		return fmt.Errorf("no address to bind")
	}

	wg := sync.WaitGroup{}
	errChan := make(chan error, 1) // must be a buffered chan to catch at least one err.

	for _, s := range d.config.Bind {
		ss := strings.Split(s, "://")
		if len(ss) != 2 {
			return fmt.Errorf("invalid bind address: %s", s)
		}
		network := ss[0]
		addr := ss[1]

		switch network {
		case "tcp":
			l, err := net.Listen("tcp", addr)
			if err != nil {
				return err
			}
			defer l.Close()
			d.entry.Infof("StartServer: tcp server started at %s", l.Addr())

			wg.Add(1)
			go func() {
				defer wg.Done()
				err := d.listenAndServeTCP(l)
				select {
				case errChan <- err:
				default:
				}
			}()
		case "udp":
			l, err := net.ListenPacket("udp", addr)
			if err != nil {
				return err
			}
			defer l.Close()
			d.entry.Infof("StartServer: udp server started at %s", l.LocalAddr())

			wg.Add(1)
			go func() {
				defer wg.Done()
				err := d.listenAndServeUDP(l)
				select {
				case errChan <- err:
				default:
				}
			}()
		default:
			return fmt.Errorf("invalid bind protocol: %s", network)
		}
	}

	listenerErr := <-errChan

	return fmt.Errorf("server listener failed and exited: %v", listenerErr)
}

// listenAndServeTCP start a tcp server at given l. Will always return non-nil err.
func (d *Dispatcher) listenAndServeTCP(l net.Listener) error {
	listenerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for {
		c, err := l.Accept()

		if err != nil {
			er, ok := err.(net.Error)
			if ok && er.Temporary() {
				d.entry.Warnf("ListenAndServe: Accept: temporary err: %v", err)
				time.Sleep(time.Millisecond * 100)
				continue
			} else {
				return fmt.Errorf("Accept: %s", err)
			}
		}

		go func() {
			defer c.Close()
			tcpConnCtx, cancel := context.WithCancel(listenerCtx)
			defer cancel()

			for {
				c.SetReadDeadline(time.Now().Add(serverTimeout))
				q, _, _, err := readMsgFromTCP(c)
				if err != nil {
					return // read err, close the conn
				}

				go func() {
					queryCtx, cancel := context.WithTimeout(tcpConnCtx, queryTimeout)
					defer cancel()

					requestLogger := pool.GetRequestLogger(d.entry.Logger, q)
					defer pool.ReleaseRequestLogger(requestLogger)

					r, err := d.ServeDNS(queryCtx, q)
					if err != nil {
						requestLogger.Warnf("query failed, %v", err)
						return // ignore it, result is empty
					}

					c.SetWriteDeadline(time.Now().Add(serverTimeout))
					_, err = writeMsgToTCP(c, r)
					if err != nil {
						requestLogger.Warnf("failed to send reply back, writeMsgToTCP: %v", err)
					}
				}()

			}
		}()
	}
}

// listenAndServeUDP start a udp server at given l. Will always return non-nil err.
func (d *Dispatcher) listenAndServeUDP(l net.PacketConn) error {

	readBuf := make([]byte, MaxUDPSize)
	for {
		n, from, err := l.ReadFrom(readBuf)
		if err != nil {
			er, ok := err.(net.Error)
			if ok && er.Temporary() {
				d.entry.Warnf("ListenAndServe: ReadFrom(): temporary err: %v", err)
				time.Sleep(time.Millisecond * 100)
				continue
			} else {
				return fmt.Errorf("ReadFrom: %s", err)
			}
		}

		// msg small than headerSize
		// do nothing, avoid ddos
		if n < 12 {
			continue
		}

		q := new(dns.Msg)
		err = q.Unpack(readBuf[:n])
		if err != nil {
			continue
		}

		go func() {
			queryCtx, cancel := context.WithTimeout(context.Background(), queryTimeout)
			defer cancel()

			requestLogger := pool.GetRequestLogger(d.entry.Logger, q)
			defer pool.ReleaseRequestLogger(requestLogger)

			r, err := d.ServeDNS(queryCtx, q)
			if err != nil {
				requestLogger.Warnf("query failed, %v", err)
				return
			}

			buf := pool.AcquirePackBuf()
			defer pool.ReleasePackBuf(buf)

			rRaw, err := r.PackBuffer(buf)
			if err != nil {
				requestLogger.Warnf("failed to send reply back, PackBuffer, %v", err)
				return
			}

			l.SetWriteDeadline(time.Now().Add(serverTimeout))
			_, err = l.WriteTo(rRaw, from)
			if err != nil {
				requestLogger.Warnf("failed to send reply back, WriteTo: %v", err)
			}
		}()
	}
}

// ListenAndServe listen on a port and start the server. Only support tcp and udp network.
// Will always return a non-nil err.
func (d *Dispatcher) ListenAndServe(network, addr string, maxUDPSize int) error {

	switch network {
	case "tcp":

	case "udp":

	}
	return fmt.Errorf("unknown network: %s", network)
}

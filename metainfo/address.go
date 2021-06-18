// Copyright 2020 xgfone
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metainfo

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"

	"github.com/eyedeekay/sam3/i2pkeys"
	"github.com/xgfone/bt/bencode"
)

// ErrInvalidAddr is returned when the compact address is invalid.
var ErrInvalidAddr = fmt.Errorf("invalid compact information of ip and port")

// Address represents a client/server listening on a UDP port implementing
// the DHT protocol.
type Address struct {
	Addr net.Addr
}

func EmtpyAddress() Address {
	var a Address
	a.Addr = &net.UDPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 0,
	}
	//a.Port = 0
	return a
}

func To4(addr net.Addr) net.IP {
	if addr == nil {
		return net.ParseIP("127.0.0.1")
	}
	rawip := net.ParseIP(Host(addr))
	if rawip == nil {
		return net.ParseIP("127.0.0.1")
	}
	return rawip.To4()
}

func To16(addr net.Addr) net.IP {
	if addr == nil {
		return net.ParseIP("127.0.0.1")
	}
	rawip := net.ParseIP(Host(addr))
	if rawip == nil {
		return net.ParseIP("127.0.0.1")
	}
	return rawip.To16()
}

func ToIP(addr net.Addr) net.IP {
	if addr == nil {
		return net.ParseIP("127.0.0.1")
	}
	return net.ParseIP(Host(addr))
}

func Host(raddr net.Addr) string {
	h, _ := SplitHostPort(raddr)
	return h
}

func SplitHostPort(raddr net.Addr) (string, int) {
	var host, port, err = net.SplitHostPort(raddr.String())
	if err != nil {
		//host = net.ParseIP(raddr.String()).String()
		port = strings.Replace(raddr.String(), ":", "", -1)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	portint, _ := strconv.Atoi(port)
	return host, portint
}

// NewAddress returns a new Address.
func NewAddress(ip net.Addr, port uint16) Address {
	addr := &Address{
		Addr: &net.UDPAddr{
			IP:   ip.(*net.UDPAddr).IP,
			Port: int(port),
		},
	}
	return *addr
}

func (a Address) Network() string {
	return a.Addr.Network()
}

// NewAddressFromString returns a new Address by the address string.
func NewAddressFromString(s string) (addr Address, err error) {
	err = addr.FromString(s)
	return
}

// NewAddressesFromString returns a list of Addresses by the address string.
func NewAddressesFromString(s string) (addrs []Address, err error) {
	shost, sport, err := net.SplitHostPort(s)
	if err != nil {
		return nil, fmt.Errorf("invalid address '%s': %s", s, err)
	}

	var port uint16
	if sport != "" {
		v, err := strconv.ParseUint(sport, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("invalid address '%s': %s", s, err)
		}
		port = uint16(v)
	}

	ips, err := net.LookupIP(shost)
	if err != nil {
		return nil, fmt.Errorf("fail to lookup the domain '%s': %s", shost, err)
	}

	addrs = make([]Address, len(ips))
	for i, ip := range ips {
		addrs[i] = Address{Addr: &net.UDPAddr{IP: ip, Port: int(port)}}
	}

	return
}

// FromString parses and sets the ip from the string addr.
func (a *Address) FromString(addr string) (err error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid address '%s': %s", addr, err)
	}

	var p int
	if port != "" {
		v, err := strconv.ParseUint(port, 10, 16)
		if err != nil {
			return fmt.Errorf("invalid address '%s': %s", addr, err)
		}
		p = int(v)
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("fail to lookup the domain '%s': %s", host, err)
	} else if len(ips) == 0 {
		return fmt.Errorf("the domain '%s' has no ips", host)
	}

	a.Addr = &net.UDPAddr{IP: ips[0], Port: p}
	log.Println(a)
	if ip := To4(a.Addr); len(ip) > 0 {
		a.Addr = &net.UDPAddr{IP: ip, Port: p}
	}
	log.Println(a)
	return
}

// FromUDPAddr sets the ip from net.UDPAddr.
func (a *Address) FromUDPAddr(ua *net.UDPAddr) {
	a.Addr = ua
}

// FromI2PAddr sets the ip from net.UDPAddr.
func (a *Address) FromI2PAddr(ua *i2pkeys.I2PAddr) {
	a.Addr = ua
}

// UDPAddr creates a new net.UDPAddr.
func (a Address) UDPAddr() *net.UDPAddr {
	return &net.UDPAddr{
		IP:   ToIP(a.Addr),
		Port: int(a.Port()),
	}
}

func (a *Address) Port() uint16 {
	switch a.Addr.(type) {
	case i2pkeys.I2PAddr:
		return 6881
	default:
		_, port := SplitHostPort(a.Addr)
		return uint16(port)
	}
}

func (a Address) String() string {
	if a.Port() == 0 {
		host, _ := SplitHostPort(a.Addr)
		return host
	}
	return a.Addr.String()
	/*host, port := SplitHostPort(a.Addr)
	if port != 0 {
	  a.Port = uint16(port)
	}
	if a.Port == 0 {
		return host
	}
	r := net.JoinHostPort(host, strconv.FormatUint(uint64(a.Port), 10))*/
	//return r
}

// Equal reports whether n is equal to o, which is equal to
//   n.HasIPAndPort(o.IP, o.Port)
func (a Address) Equal(o Address) bool {
	if a.String() == o.String() {
		return a.Port() == o.Port()
	}
	return false
}

// HasIPAndPort reports whether the current node has the ip and the port.
func (a Address) HasIPAndPort(ip net.IP, port uint16) bool {
	return port == a.Port() && ToIP(a.Addr).Equal(ip)
}

// WriteBinary is the same as MarshalBinary, but writes the result into w
// instead of returning.
func (a Address) WriteBinary(w io.Writer) (m int, err error) {
	if m, err = w.Write([]byte(To4(a.Addr))); err == nil {
		if err = binary.Write(w, binary.BigEndian, a.Port()); err == nil {
			m += 2
		}
	}
	return
}

// UnmarshalBinary implements the interface binary.BinaryUnmarshaler.
func (a *Address) UnmarshalBinary(b []byte) (err error) {
	_len := len(b) - 2
	switch _len {
	case net.IPv4len, net.IPv6len:
	default:
		return ErrInvalidAddr
	}

	ip := make(net.IP, _len)
	copy(ip, b[:_len])
	a.Addr = &net.UDPAddr{IP: ip, Port: int(binary.BigEndian.Uint16(b[_len:]))}
	//	a.Port = binary.BigEndian.Uint16(b[_len:])
	return
}

// MarshalBinary implements the interface binary.BinaryMarshaler.
func (a Address) MarshalBinary() (data []byte, err error) {
	buf := bytes.NewBuffer(nil)
	log.Println("Marshal IP", a)
	buf.Grow(20)
	if _, err = a.WriteBinary(buf); err == nil {
		data = buf.Bytes()
	}
	return
}

func (a *Address) decode(vs []interface{}) (err error) {
	defer func() {
		switch e := recover().(type) {
		case nil:
		case error:
			err = e
		default:
			err = fmt.Errorf("%v", e)
		}
	}()

	ip := net.ParseIP(vs[0].(string))

	if len(ip) == 0 {
		return ErrInvalidAddr
	} else if ipv4 := ip.To4(); len(ipv4) > 0 {
		a.Addr = &net.UDPAddr{IP: ipv4, Port: int(vs[1].(int64))}
	}

	//a.Port = uint16(vs[1].(int64))
	return
}

// UnmarshalBencode implements the interface bencode.Unmarshaler.
func (a *Address) UnmarshalBencode(b []byte) (err error) {
	var iface interface{}
	if err = bencode.NewDecoder(bytes.NewBuffer(b)).Decode(&iface); err != nil {
		return
	}

	switch v := iface.(type) {
	case string:
		err = a.FromString(v)
	case []interface{}:
		err = a.decode(v)
	default:
		err = fmt.Errorf("unsupported type: %T", iface)
	}

	return
}

// MarshalBencode implements the interface bencode.Marshaler.
func (a Address) MarshalBencode() (b []byte, err error) {
	buf := bytes.NewBuffer(nil)
	buf.Grow(32)
	switch a.Addr.(type) {
	case i2pkeys.I2PAddr:
		err = bencode.NewEncoder(buf).Encode([]interface{}{a.Addr.String(), a.Port()})
	case *net.UDPAddr:
		err = bencode.NewEncoder(buf).Encode([]interface{}{a.Addr.(*net.UDPAddr).IP.String(), a.Port()})
	case *net.IPAddr:
		err = bencode.NewEncoder(buf).Encode([]interface{}{a.Addr.(*net.IPAddr).IP.String(), a.Port()})
	default:
		err = bencode.NewEncoder(buf).Encode([]interface{}{a.String()}) //, a.Port()})
	}

	if err == nil {
		b = buf.Bytes()
	}
	return
}

/// >>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>

// HostAddress is the same as the Address, but the host part may be
// either a domain or a ip.
type HostAddress struct {
	Host string
	Port uint16
}

// NewHostAddress returns a new host addrress.
func NewHostAddress(host string, port uint16) HostAddress {
	return HostAddress{Host: host, Port: port}
}

// NewHostAddressFromString returns a new host address by the string.
func NewHostAddressFromString(s string) (addr HostAddress, err error) {
	err = addr.FromString(s)
	return
}

// FromString parses and sets the host from the string addr.
func (a *HostAddress) FromString(addr string) (err error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid address '%s': %s", addr, err)
	} else if host == "" {
		return fmt.Errorf("invalid address '%s': missing host", addr)
	}

	if port != "" {
		v, err := strconv.ParseUint(port, 10, 16)
		if err != nil {
			return fmt.Errorf("invalid address '%s': %s", addr, err)
		}
		a.Port = uint16(v)
	}

	a.Host = host
	return
}

func (a HostAddress) String() string {
	if a.Port == 0 {
		return a.Host
	}
	return net.JoinHostPort(a.Host, strconv.FormatUint(uint64(a.Port), 10))
}

// Addresses parses the host address to a list of Addresses.
func (a HostAddress) Addresses() (addrs []Address, err error) {
	if ip := net.ParseIP(a.Host); len(ip) > 0 {
		return []Address{NewAddress(&net.UDPAddr{IP: ip}, a.Port)}, nil
	}

	ips, err := net.LookupIP(a.Host)
	if err != nil {
		err = fmt.Errorf("fail to lookup the domain '%s': %s", a.Host, err)
	} else {
		addrs = make([]Address, len(ips))
		for i, ip := range ips {
			addrs[i] = NewAddress(&net.UDPAddr{IP: ip}, a.Port)
		}
	}

	return
}

// Equal reports whether a is equal to o.
func (a HostAddress) Equal(o HostAddress) bool {
	return a.Port == o.Port && a.Host == o.Host
}

func (a *HostAddress) decode(vs []interface{}) (err error) {
	defer func() {
		switch e := recover().(type) {
		case nil:
		case error:
			err = e
		default:
			err = fmt.Errorf("%v", e)
		}
	}()

	log.Println("FUUUUUUUCK", vs)

	a.Host = vs[0].(string)
	a.Port = uint16(vs[1].(int64))
	return
}

// UnmarshalBencode implements the interface bencode.Unmarshaler.
func (a *HostAddress) UnmarshalBencode(b []byte) (err error) {
	var iface interface{}
	if err = bencode.NewDecoder(bytes.NewBuffer(b)).Decode(&iface); err != nil {
		return
	}

	switch v := iface.(type) {
	case string:
		err = a.FromString(v)
	case []interface{}:
		err = a.decode(v)
	default:
		err = fmt.Errorf("unsupported type: %T", iface)
	}

	return
}

// MarshalBencode implements the interface bencode.Marshaler.
func (a HostAddress) MarshalBencode() (b []byte, err error) {
	buf := bytes.NewBuffer(nil)
	buf.Grow(32)
	err = bencode.NewEncoder(buf).Encode([]interface{}{a.Host, a.Port})
	if err == nil {
		b = buf.Bytes()
	}
	return
}

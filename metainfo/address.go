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
	"net"
	"strconv"

	"github.com/xgfone/bt/bencode"
	"github.com/eyedeekay/sam3/i2pkeys"
)

func to4(ip net.Addr) (net.IP, net.Addr) {
	switch v := ip.(type) {
		case *net.IPAddr:
			return v.IP.To4(), v
		case *i2pkeys.I2PAddr:
			return net.ParseIP(""), nil
		case *net.TCPAddr:
			return v.IP.To4(), v
		case *net.UDPAddr:
			return v.IP.To4(), v
	}
	return net.ParseIP(""), nil
}

// ErrInvalidAddr is returned when the compact address is invalid.
var ErrInvalidAddr = fmt.Errorf("invalid compact information of ip and port")

// Address represents a client/server listening on a UDP port implementing
// the DHT protocol.
type Address struct {
	IP   net.Addr // For IPv4, its length must be 4.
//	Port uint16
}

// NewAddress returns a new Address.
func NewAddress(ip net.Addr, port uint16) Address {
	if ipv4, ad := to4(ip); len(ipv4) > 0 {
		ip = ad
	}
	return Address{IP: ip}
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
		if ipv4, ad := to4(&net.IPAddr{IP:ip}); len(ipv4) != 0 {
			addrs[i] = Address{IP: ad}
		} else {
			addrs[i] = Address{IP: ad}
		}
	}

	return
}

// FromString parses and sets the ip from the string addr.
func (a *Address) FromString(addr string) (err error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid address '%s': %s", addr, err)
	}

	if port != "" {
		v, err := strconv.ParseUint(port, 10, 16)
		if err != nil {
			return fmt.Errorf("invalid address '%s': %s", addr, err)
		}
//		a.Port = uint16(v)
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("fail to lookup the domain '%s': %s", host, err)
	} else if len(ips) == 0 {
		return fmt.Errorf("the domain '%s' has no ips", host)
	}

	a.IP = &net.IPAddr{IP:ips[0]}
	if ip, ad := to4(a.IP); len(ip) > 0 {
		a.IP = ad
	}

	return
}

// FromUDPAddr sets the ip from net.UDPAddr.
func (a *Address) FromUDPAddr(ua *net.UDPAddr) {
//	a.Port = uint16(ua.Port)
	a.IP = &net.IPAddr{ua.IP}
	if ipv4 := to4(&net.IPAddr{a.IP}); len(ipv4) != 0 {
		a.IP = ipv4
	}
}

// UDPAddr creates a new net.UDPAddr.
func (a Address) UDPAddr() *net.UDPAddr {
	return &net.UDPAddr{
		IP:   a.IP,
		Port: int(a.Port),
	}
}

func (a Address) String() string {
	if a.Port == 0 {
		return a.IP.String()
	}
	return net.JoinHostPort(a.IP.String(), strconv.FormatUint(uint64(a.Port), 10))
}

// Equal reports whether n is equal to o, which is equal to
//   n.HasIPAndPort(o.IP, o.Port)
func (a Address) Equal(o Address) bool {
	return a.Port == o.Port && a.IP.Equal(o.IP)
}

// HasIPAndPort reports whether the current node has the ip and the port.
func (a Address) HasIPAndPort(ip net.Addr, port uint16) bool {
	return port == a.Port && a.IP.Equal(ip)
}

// WriteBinary is the same as MarshalBinary, but writes the result into w
// instead of returning.
func (a Address) WriteBinary(w io.Writer) (m int, err error) {
	if m, err = w.Write(a.IP); err == nil {
		if err = binary.Write(w, binary.BigEndian, a.Port); err == nil {
			m += 2
		}
	}
	return
}

// UnmarshalBinary implements the interface binary.BinaryUnmarshaler.
func (a *Address) UnmarshalBinary(b []byte) (err error) {
	_len := len(b) - 2
	switch _len {
	case net.Addrv4len, net.Addrv6len:
	default:
		return ErrInvalidAddr
	}

	a.IP = make(net.Addr, _len)
	copy(a.IP, b[:_len])
	a.Port = binary.BigEndian.Uint16(b[_len:])
	return
}

// MarshalBinary implements the interface binary.BinaryMarshaler.
func (a Address) MarshalBinary() (data []byte, err error) {
	buf := bytes.NewBuffer(nil)
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

	host := vs[0].(string)
	if a.IP = net.ParseIP(host); len(a.IP) == 0 {
		return ErrInvalidAddr
	} else if ip := to4(a.IP); len(ip) != 0 {
		a.IP = ip
	}

	a.Port = uint16(vs[1].(int64))
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
	err = bencode.NewEncoder(buf).Encode([]interface{}{a.IP.String(), a.Port})
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
	return HostAddress{Host: host}
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
	if ip := net.ParseIP(a.Host); len(ip) != 0 {
		return []Address{NewAddress(ip, a.Port)}, nil
	}

	ips, err := net.LookupIP(a.Host)
	if err != nil {
		err = fmt.Errorf("fail to lookup the domain '%s': %s", a.Host, err)
	} else {
		addrs = make([]Address, len(ips))
		for i, ip := range ips {
			addrs[i] = NewAddress(ip, a.Port)
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

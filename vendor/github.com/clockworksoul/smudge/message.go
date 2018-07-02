/*
Copyright 2016 The Smudge Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package smudge

import (
	"errors"
	"hash/adler32"
	"net"
)

// Message contents
// ---[ Base message (11 bytes)]---
// Bytes 00-03 Checksum (32-bit)
// Bytes 04    Verb (one of {PING|ACK|PINGREQ|NFPING})
// Bytes 05-06 Sender response port
// Bytes 07-10 Sender current heartbeat
// ---[ Per member (23 bytes)]---
// Bytes 00    Member status byte
// Bytes 01-16 Member host IP (01-04 for IPv4)
// Bytes 17-18 Member host response port (05-06 for IPv4)
// Bytes 19-22 Sender current heartbeat (07-10 for IPv4)
// ---[ Per broadcast (1 allowed) (23+N bytes) ]
// Bytes 00-15 Origin IP (00-03 for IPv4)
// Bytes 16-17 Origin response port (04-05 for IPv4)
// Bytes 18-21 Origin broadcast counter (06-09 for IPv4)
// Bytes 22-23 Payload length (bytes) (10-11 for IPv4)
// Bytes 24-NN Payload (12-NN for IPv4)

type message struct {
	sender          *Node
	senderHeartbeat uint32
	verb            messageVerb
	members         []*messageMember
	broadcast       *Broadcast
}

// Represents a "member" of a message; i.e., a node that the sender knows
// about, about which it wishes to notify the downstream recipient.
type messageMember struct {
	// The source of the gossip about the member.
	source *Node

	// The last known heartbeat of node.
	heartbeat uint32

	// The subject of the gossip.
	node *Node

	// The status that the gossip is conveying.
	status NodeStatus
}

// Convenience function. Creates a new message instance.
func newMessage(verb messageVerb, sender *Node, senderHeartbeat uint32) message {
	return message{
		sender:          sender,
		senderHeartbeat: senderHeartbeat,
		verb:            verb,
	}
}

// Adds a broadcast to this message. Only one broadcast is allowed; subsequent
// calls will replace an existing broadcast.
func (m *message) addBroadcast(broadcast *Broadcast) {
	m.broadcast = broadcast
}

// Adds a member status update to this message. The maximum number of allowed
// members is 2^6 - 1 = 63, though it is incredibly unlikely that this maximum
// will be reached without an absurdly high lambda. There aren't yet many
// 88 billion node clusters (assuming lambda of 2.5).
func (m *message) addMember(node *Node, status NodeStatus, heartbeat uint32, gossipSource *Node) error {
	if m.members == nil {
		m.members = make([]*messageMember, 0, 32)
	} else if len(m.members) >= 63 {
		return errors.New("member list overflow")
	}

	messageMember := messageMember{
		heartbeat: heartbeat,
		node:      node,
		status:    status,
		source:    gossipSource,
	}

	m.members = append(m.members, &messageMember)

	return nil
}

// Message contents
// ---[ Base message (12 bytes)]---
// Bytes 00-03 Checksum (32-bit)
// Bytes 04    Verb (one of {PING|ACK|PINGREQ|NFPING})
// Bytes 05-06 Sender response port
// Bytes 07-10 Sender ID Code
// ---[ Per member (23 bytes, 17 bytes for IPv4)]---
// Bytes 00    Member status byte
// Bytes 01-16 Member host IP (01-04 for IPv4)
// Bytes 17-18 Member host response port (05-06 for IPv4)
// Bytes 19-22 Member heartbeat (07-10 for IPv4)
// Bytes 23-38 Gossip source IP (11-14 fit IPv4)
// Bytes 39-40 Gossip source response port (15-16 for IPv4)

func (m *message) encode() []byte {
	// Pre-calculate the message size. Each message prefix is 11 bytes.
	// Each member has a constant size of 9 bytes, plus 2 times the length of
	// the IP (4 for IPv4, 16 for IPv6).
	size := 11 + (len(m.members) * (9 + ipLen + ipLen))

	if m.broadcast != nil {
		size += 8 + ipLen + len(m.broadcast.bytes)
	}

	bytes := make([]byte, size, size)

	// An index pointer (start at 4 to accommodate checksum)
	p := 4

	// Byte 00
	// Rightmost 2 bits: verb (one of {P|A|F|N})
	// Leftmost 6 bits: number of members in payload
	verbByte := byte(len(m.members))
	verbByte = (verbByte << 2) | byte(m.verb)
	p += encodeByte(verbByte, bytes, p)

	// Bytes 01-02 Sender response port
	p += encodeUint16(m.sender.port, bytes, p)

	// Bytes 03-06 ID Code
	p += encodeUint32(m.senderHeartbeat, bytes, p)

	// Each member data requires 23 bytes (11 for IPv4).
	for _, member := range m.members {
		mnode := member.node
		mstatus := member.status
		mcode := member.heartbeat
		snode := member.source

		// Byte p + 00
		bytes[p] = byte(mstatus)
		p++

		var ipb net.IP

		// Member host IP
		// IPv4: Bytes (p + 01) to (p + 04)
		// IPv6: Bytes (p + 01) to (p + 16)
		if ipLen == net.IPv4len {
			ipb = mnode.ip.To4()
		} else if ipLen == net.IPv6len {
			ipb = mnode.ip.To16()
		}

		for i := 0; i < ipLen; i++ {
			bytes[p+i] = ipb[i]
		}
		p += ipLen

		// Member host response port
		// IPv4: Bytes (p + 05) to (p + 06)
		// IPv6: Bytes (p + 17) to (p + 18)
		p += encodeUint16(mnode.port, bytes, p)

		// Member heartbeat
		// IPv4: Bytes (p + 07) to (p + 10)
		// IPv6: Bytes (p + 19) to (p + 22)
		p += encodeUint32(mcode, bytes, p)

		if snode != nil {
			// Gossip source host IP
			// IPv4: Bytes (p + 11) to (p + 14)
			// IPv6: Bytes (p + 23) to (p + 39)
			if ipLen == net.IPv4len {
				ipb = snode.ip.To4()
			} else if ipLen == net.IPv6len {
				ipb = snode.ip.To16()
			}

			for i := 0; i < ipLen; i++ {
				bytes[p+i] = ipb[i]
			}

			p += ipLen

			// Gossip source host response port
			// IPv4: Bytes (p + 15) to (p + 16)
			// IPv6: Bytes (p + 40) to (p + 41)
			p += encodeUint16(snode.port, bytes, p)
		} else {
			p += ipLen + 2
		}
	}

	if m.broadcast != nil {
		bbytes := m.broadcast.encode()
		for i, v := range bbytes {
			bytes[p+i] = v
		}
	}

	checksum := adler32.Checksum(bytes[4:])
	encodeUint32(checksum, bytes, 0)

	return bytes
}

// If members exist on this message, and that message has the "forward to"
// status, this function returns it; otherwise it returns nil.
func (m *message) getForwardTo() *messageMember {
	if len(m.members) > 0 && m.members[0].status == StatusForwardTo {
		return m.members[0]
	}

	return nil
}

// Parses the bytes received in a UDP message.
// If the address:port from the message can't be associated with a known
// (live) node, then an instance of message.sender will be created from
// available data but not explicitly added to the known nodes.
func decodeMessage(sourceIP net.IP, bytes []byte) (message, error) {
	var err error

	// An index pointer
	p := 0

	// Bytes 00-03 Checksum (32-bit)
	checksumStated, p := decodeUint32(bytes, p)
	checksumCalculated := adler32.Checksum(bytes[4:])
	if checksumCalculated != checksumStated {
		return newMessage(255, nil, 0),
			errors.New("checksum failure from " + sourceIP.String())
	}

	// Byte 04
	// Rightmost 2 bits: verb (one of {P|A|F|N})
	// Leftmost 6 bits: number of members in payload
	v, p := decodeByte(bytes, p)
	verb := messageVerb(v & 0x03)

	memberCount := int(v >> 2)

	// Bytes 05-06 Sender response port
	senderPort, p := decodeUint16(bytes, p)

	// Bytes 07-10 Sender ID Code
	senderHeartbeat, p := decodeUint32(bytes, p)

	// Now that we have the IP and port, we can find the Node.
	sender := knownNodes.getByIP(sourceIP, senderPort)

	// We don't know this node, so create a new one!
	if sender == nil {
		sender, _ = CreateNodeByIP(sourceIP, senderPort)
	}

	// Now that we have the verb, node, and code, we can build the mesage
	m := newMessage(verb, sender, senderHeartbeat)

	memberLastIndex := p + (memberCount * (9 + ipLen + ipLen))

	if len(bytes) > p {
		m.members = decodeMembers(memberCount, bytes[p:memberLastIndex])
	}

	if len(bytes) > memberLastIndex {
		m.broadcast, err = decodeBroadcast(bytes[memberLastIndex:])
	}

	return m, err
}

func decodeMembers(memberCount int, bytes []byte) []*messageMember {
	// Bytes 00    Member status byte
	// Bytes 01-16 Member host IP (01-04 for IPv4)
	// Bytes 17-18 Member host response port (05-06 for IPv4)
	// Bytes 19-22 Member heartbeat (07-10 for IPv4)

	members := make([]*messageMember, 0, 1)

	// An index pointer
	p := 0

	for p < len(bytes) {
		var mstatus NodeStatus
		var mip net.IP
		var mport uint16
		var mcode uint32
		var mnode *Node
		var sip net.IP
		var sport uint16
		var snode *Node

		// Byte 00 Member status byte
		mstatus = NodeStatus(bytes[p])
		p++

		if ipLen == net.IPv6len {
			// Bytes 01-16 member IP
			mip = make(net.IP, net.IPv6len)
			copy(mip, bytes[p:p+16])
		} else {
			// Bytes 01-04 member IPv4
			mip = net.IPv4(bytes[p+0], bytes[p+1], bytes[p+2], bytes[p+3])
		}
		p += ipLen

		// Bytes 17-18 member response port
		mport, p = decodeUint16(bytes, p)

		// Bytes 19-22 member heartbeat
		mcode, p = decodeUint32(bytes, p)

		if len(mip) > 0 {
			// Find the sender by the address associated with the message
			mnode = knownNodes.getByIP(mip, mport)

			// We still don't know this node, so create a new one!
			if mnode == nil {
				mnode, _ = CreateNodeByIP(mip, mport)
			}
		}

		if ipLen == net.IPv6len {
			// Bytes 01-16 member IP
			sip = make(net.IP, net.IPv6len)
			copy(sip, bytes[p:p+16])
		} else {
			// Bytes 01-04 member IPv4
			sip = net.IPv4(bytes[p+0], bytes[p+1], bytes[p+2], bytes[p+3])
		}
		p += ipLen

		// Bytes 17-18 member response port
		sport, p = decodeUint16(bytes, p)

		if len(sip) > 0 {
			// Find the sender by the address associated with the message
			snode = knownNodes.getByIP(sip, sport)

			// We still don't know this node, so create a new one!
			if snode == nil {
				snode, _ = CreateNodeByIP(sip, sport)
			}
		}

		member := messageMember{
			heartbeat: mcode,
			node:      mnode,
			source:    snode,
			status:    mstatus,
		}

		members = append(members, &member)
	}

	return members
}

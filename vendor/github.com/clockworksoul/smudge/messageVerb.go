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

type messageVerb byte

const (
	// VerbPing represents a simple ping. If this ping is not responded to with
	// an ack within a timeout period, the pinging host will attempt to ping
	// indirectly via one or more additional hosts with a ping request.
	verbPing messageVerb = iota

	// VerbAck represents a response to a ping request.
	verbAck

	// VerbPingRequest represents a request made by one host to another to ping
	// a third host whose live status is in question.
	verbPingRequest

	// VerbNonForwardingPing represents a ping in response to a ping request.
	// If the ping times out, the host does not follow up with a ping request
	// to any other hosts.
	verbNonForwardingPing
)

func (v messageVerb) String() string {
	switch v {
	case verbPing:
		return "PING"
	case verbAck:
		return "ACK"
	case verbPingRequest:
		return "PINGREQ"
	case verbNonForwardingPing:
		return "NFPING"
	default:
		return "UNDEFINED"
	}
}

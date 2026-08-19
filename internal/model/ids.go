package model

import "sync/atomic"

// NetworkID is the stable identity of one configured network connection. It is
// assigned when the network is configured and never changes, so it survives
// reconnects, server renames and the user editing the network's display name.
// Every Event and every Change carries one.
type NetworkID string

// BufferID names a buffer. It is the *only* correct buffer key: the network it
// belongs to plus the casefolded name (proto.ServerProps.Fold applied with the
// server's advertised CASEMAPPING). Never key anything by the display name -
// the server may re-case it, draft/channel-rename may change it, and two nicks
// that differ only in case are one person.
//
// The zero BufferID means "no buffer" (a network-scoped Change). A buffer with
// an empty Folded name but a non-empty Network is the network's own status
// buffer; see ServerBuffer.
type BufferID struct {
	Network NetworkID
	Folded  string
}

// ServerBuffer is the id of a network's status buffer: the one that holds
// connection notices, MOTD and unrouted numerics.
func ServerBuffer(n NetworkID) BufferID { return BufferID{Network: n} }

// IsZero reports whether b names no buffer at all.
func (b BufferID) IsZero() bool { return b.Network == "" && b.Folded == "" }

// IsServer reports whether b is a network status buffer (see ServerBuffer).
func (b BufferID) IsServer() bool { return b.Network != "" && b.Folded == "" }

// String renders the id for logs and test failures. It is not a wire format.
func (b BufferID) String() string {
	if b.IsZero() {
		return "<none>"
	}
	return string(b.Network) + "/" + b.Folded
}

// MessageID is a message's stable local identity, minted by the store the
// moment the message exists - including for a locally echoed message that has
// no server msgid yet. The UI, the archive and the render cache all key on it.
// The zero value means "not assigned".
type MessageID uint64

// IDSeq mints MessageIDs. It is a value you own and pass around: there is no
// package-level sequence, so two stores in one process (and two tests in one
// package) never share numbering. Safe for concurrent use, though in practice
// only the single-writer store calls it.
type IDSeq struct{ n atomic.Uint64 }

// NewIDSeq returns a sequence whose first Next is start+1. Pass 0 for a fresh
// sequence; pass the highest id restored from the archive to continue one.
func NewIDSeq(start uint64) *IDSeq {
	s := &IDSeq{}
	s.n.Store(start)
	return s
}

// Next returns the next unused MessageID. It never returns 0.
func (s *IDSeq) Next() MessageID { return MessageID(s.n.Add(1)) }

// Last returns the most recently issued id, or 0 if none has been issued.
func (s *IDSeq) Last() MessageID { return MessageID(s.n.Load()) }

package nbd

// See https://github.com/NetworkBlockDevice/nbd/blob/master/doc/proto.md and https://github.com/abligh/gonbdserver/

const (
	NEGOTIATION_MAGIC_OLDSTYLE = uint64(0x4e42444d41474943)
	NEGOTIATION_MAGIC_OPTION   = uint64(0x49484156454F5054)
	NEGOTIATION_MAGIC_REPLY    = uint64(0x3e889045565a9)

	NEGOTIATION_HANDSHAKE_FLAG_FIXED_NEWSTYLE = uint16(1 << 0)

	NEGOTIATION_ID_OPTION_ABORT = uint32(2)
	NEGOTIATION_ID_OPTION_LIST  = uint32(3)
	NEGOTIATION_ID_OPTION_INFO  = uint32(6)
	NEGOTIATION_ID_OPTION_GO    = uint32(7)

	NEGOTIATION_TYPE_REPLY_ACK             = uint32(1)
	NEGOTIATION_TYPE_REPLY_SERVER          = uint32(2)
	NEGOTIATION_TYPE_REPLY_INFO            = uint32(3)
	NEGOTIATION_TYPE_REPLY_ERR_UNSUPPORTED = uint32(1 | uint32(1<<31))
	NEGOTIATION_TYPE_REPLY_ERR_UNKNOWN     = uint32(6 | uint32(1<<31))

	NEGOTIATION_TYPE_INFO_EXPORT      = uint16(0)
	NEGOTIATION_TYPE_INFO_NAME        = uint16(1)
	NEGOTIATION_TYPE_INFO_DESCRIPTION = uint16(2)
	NEGOTIATION_TYPE_INFO_BLOCKSIZE   = uint16(3)

	NEGOTIATION_REPLY_FLAGS_HAS_FLAGS      = uint16((1 << 0))
	NEGOTIATION_REPLY_FLAGS_CAN_MULTI_CONN = uint16((1 << 8))

	NEGO_FLAG_READONLY          = uint16(1 << 1)
	NEGO_FLAG_SEND_FLUSH        = uint16(1 << 2)
	NEGO_FLAG_SEND_FUA          = uint16(1 << 3)
	NEGO_FLAG_ROTATIONAL        = uint16(1 << 4)
	NEGO_FLAG_SEND_TRIM         = uint16(1 << 5)
	NEGO_FLAG_SEND_WRITE_ZEROES = uint16(1 << 6)
	NEGO_FLAG_SEND_DF           = uint16(1 << 7)
	NEGO_FLAG_SEND_RESIZE       = uint16(1 << 9)
	NEGO_FLAG_SEND_CACHE        = uint16(1 << 10)
	NEGO_FLAG_FAST_ZERO         = uint16(1 << 11)
	NEGO_FLAG_BLOCK_STATUS      = uint16(1 << 12)
)

type NegotiationNewstyleHeader struct {
	OldstyleMagic  uint64
	OptionMagic    uint64
	HandshakeFlags uint16
}

type NegotiationOptionHeader struct {
	OptionMagic uint64
	ID          uint32
	Length      uint32
}

type NegotiationReplyHeader struct {
	ReplyMagic uint64
	ID         uint32
	Type       uint32
	Length     uint32
}

type NegotiationReplyInfo struct {
	Type              uint16
	Size              uint64
	TransmissionFlags uint16
}

type NegotiationReplyNameHeader struct {
	Type uint16
}

type NegotiationReplyDescriptionHeader NegotiationReplyNameHeader

type NegotiationReplyBlockSize struct {
	Type               uint16
	MinimumBlockSize   uint32
	PreferredBlockSize uint32
	MaximumBlockSize   uint32
}

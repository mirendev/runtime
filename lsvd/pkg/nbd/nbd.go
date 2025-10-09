package nbd

import (
	"bytes"
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/pkg/errors"
)

var (
	ErrInvalidMagic     = errors.New("invalid magic")
	ErrInvalidBlocksize = errors.New("invalid blocksize")
)

const (
	defaultMaximumRequestSize = 32 * 1024 * 1024 // Support for a 32M maximum packet size is expected: https://sourceforge.net/p/nbd/mailman/message/35081223/
)

type Export struct {
	Name        string
	Description string

	BackendOpen BackendOpen
	Backend     Backend
}

type Options struct {
	ReadOnly bool

	MinimumBlockSize   uint32
	PreferredBlockSize uint32
	MaximumBlockSize   uint32

	MaximumRequestSize int
	SupportsMultiConn  bool

	RawFile *os.File
}

type filer interface {
	File() (*os.File, error)
}

func Handle(log *slog.Logger, conn net.Conn, exports []*Export, options *Options) error {
	if options == nil {
		options = &Options{
			ReadOnly:          false,
			SupportsMultiConn: true,
		}
	}

	if options.MinimumBlockSize == 0 {
		options.MinimumBlockSize = 1
	}

	if options.PreferredBlockSize == 0 {
		options.PreferredBlockSize = 4096
	}

	if options.MaximumBlockSize == 0 {
		options.MaximumBlockSize = defaultMaximumRequestSize
	}

	if options.MaximumRequestSize == 0 {
		options.MaximumRequestSize = defaultMaximumRequestSize
	}

	// Negotiation
	if err := binary.Write(conn, binary.BigEndian, NegotiationNewstyleHeader{
		OldstyleMagic:  NEGOTIATION_MAGIC_OLDSTYLE,
		OptionMagic:    NEGOTIATION_MAGIC_OPTION,
		HandshakeFlags: NEGOTIATION_HANDSHAKE_FLAG_FIXED_NEWSTYLE,
	}); err != nil {
		return errors.Wrapf(err, "unable to negation newstyle header")
	}

	var clientFlags uint32

	err := binary.Read(conn, binary.BigEndian, &clientFlags)
	if err != nil {
		return errors.Wrapf(err, "reading client flags")
	}

	log.Debug("client flags", "value", clientFlags)

	log.Debug("sent negotation header, reading options")

	var export *Export
	var backend Backend

nego:
	for {
		var optionHeader NegotiationOptionHeader
		if err := binary.Read(conn, binary.BigEndian, &optionHeader); err != nil {
			return errors.Wrapf(err, "reading negation option")
		}

		if optionHeader.OptionMagic != NEGOTIATION_MAGIC_OPTION {
			return ErrInvalidMagic
		}

		log.Debug("negoation option", "id", optionHeader.ID, "len", optionHeader.Length)

		switch optionHeader.ID {
		case NEGOTIATION_ID_OPTION_INFO, NEGOTIATION_ID_OPTION_GO:
			var exportNameLength uint32
			if err := binary.Read(conn, binary.BigEndian, &exportNameLength); err != nil {
				return errors.Wrapf(err, "reading export name length")
			}

			exportName := make([]byte, exportNameLength)
			if _, err := io.ReadFull(conn, exportName); err != nil {
				return errors.Wrapf(err, "reading export name")
			}

			log.Debug("looking for export", "name", string(exportName))

			for _, candidate := range exports {
				if candidate.Name == string(exportName) {
					export = candidate

					break
				}
			}

			if export == nil {
				log.Error("no export found", "name", exportName)

				if length := int64(optionHeader.Length) - 4 - int64(exportNameLength); length > 0 { // Discard the option's data, minus the export name length and export name we've already read
					_, err := io.CopyN(io.Discard, conn, length)
					if err != nil {
						return errors.Wrapf(err, "discarding option data")
					}
				}

				if err := binary.Write(conn, binary.BigEndian, NegotiationReplyHeader{
					ReplyMagic: NEGOTIATION_MAGIC_REPLY,
					ID:         optionHeader.ID,
					Type:       NEGOTIATION_TYPE_REPLY_ERR_UNKNOWN,
					Length:     0,
				}); err != nil {
					return errors.Wrapf(err, "writing nego reply")
				}

				break
			}

			if export.BackendOpen != nil {
				backend = export.BackendOpen.Open()
				defer export.BackendOpen.Close(backend)
			} else {
				backend = export.Backend
			}

			size, err := backend.Size()
			if err != nil {
				return errors.Wrapf(err, "getting device size")
			}

			{
				var informationRequestCount uint16
				if err := binary.Read(conn, binary.BigEndian, &informationRequestCount); err != nil {
					return err
				}

				_, err := io.CopyN(io.Discard, conn, 2*int64(informationRequestCount)) // Discard information requests (uint16s)
				if err != nil {
					return errors.Wrapf(err, "discarding information requests")
				}
			}

			log.Debug("reporting device size", "size", size)

			{
				transmissionFlags := NEGOTIATION_REPLY_FLAGS_HAS_FLAGS |
					NEGO_FLAG_SEND_WRITE_ZEROES |
					NEGO_FLAG_SEND_FLUSH |
					NEGO_FLAG_SEND_TRIM

				if options.SupportsMultiConn {
					transmissionFlags |= NEGOTIATION_REPLY_FLAGS_CAN_MULTI_CONN
				}

				log.Debug("sending transmission flags", "flags", uint64(transmissionFlags))

				info := &bytes.Buffer{}
				if err := binary.Write(info, binary.BigEndian, NegotiationReplyInfo{
					Type:              NEGOTIATION_TYPE_INFO_EXPORT,
					Size:              uint64(size),
					TransmissionFlags: transmissionFlags,
				}); err != nil {
					return err
				}

				if err := binary.Write(conn, binary.BigEndian, NegotiationReplyHeader{
					ReplyMagic: NEGOTIATION_MAGIC_REPLY,
					ID:         optionHeader.ID,
					Type:       NEGOTIATION_TYPE_REPLY_INFO,
					Length:     uint32(info.Len()),
				}); err != nil {
					return err
				}

				if _, err := io.Copy(conn, info); err != nil {
					return err
				}
			}

			{
				info := &bytes.Buffer{}
				if err := binary.Write(info, binary.BigEndian, NegotiationReplyNameHeader{
					Type: NEGOTIATION_TYPE_INFO_NAME,
				}); err != nil {
					return err
				}

				if _, err := info.Write([]byte(exportName)); err != nil {
					return err
				}

				if err := binary.Write(conn, binary.BigEndian, NegotiationReplyHeader{
					ReplyMagic: NEGOTIATION_MAGIC_REPLY,
					ID:         optionHeader.ID,
					Type:       NEGOTIATION_TYPE_REPLY_INFO,
					Length:     uint32(info.Len()),
				}); err != nil {
					return err
				}

				if _, err := io.Copy(conn, info); err != nil {
					return err
				}
			}

			{
				info := &bytes.Buffer{}
				if err := binary.Write(info, binary.BigEndian, NegotiationReplyDescriptionHeader{
					Type: NEGOTIATION_TYPE_INFO_DESCRIPTION,
				}); err != nil {
					return err
				}

				if err := binary.Write(info, binary.BigEndian, []byte(export.Description)); err != nil {
					return err
				}

				if err := binary.Write(conn, binary.BigEndian, NegotiationReplyHeader{
					ReplyMagic: NEGOTIATION_MAGIC_REPLY,
					ID:         optionHeader.ID,
					Type:       NEGOTIATION_TYPE_REPLY_INFO,
					Length:     uint32(info.Len()),
				}); err != nil {
					return err
				}

				if _, err := io.Copy(conn, info); err != nil {
					return err
				}
			}

			{
				info := &bytes.Buffer{}
				if err := binary.Write(info, binary.BigEndian, NegotiationReplyBlockSize{
					Type:               NEGOTIATION_TYPE_INFO_BLOCKSIZE,
					MinimumBlockSize:   options.MinimumBlockSize,
					PreferredBlockSize: options.PreferredBlockSize,
					MaximumBlockSize:   options.MaximumBlockSize,
				}); err != nil {
					return err
				}

				if err := binary.Write(conn, binary.BigEndian, NegotiationReplyHeader{
					ReplyMagic: NEGOTIATION_MAGIC_REPLY,
					ID:         optionHeader.ID,
					Type:       NEGOTIATION_TYPE_REPLY_INFO,
					Length:     uint32(info.Len()),
				}); err != nil {
					return err
				}

				if _, err := io.Copy(conn, info); err != nil {
					return err
				}
			}

			if err := binary.Write(conn, binary.BigEndian, NegotiationReplyHeader{
				ReplyMagic: NEGOTIATION_MAGIC_REPLY,
				ID:         optionHeader.ID,
				Type:       NEGOTIATION_TYPE_REPLY_ACK,
				Length:     0,
			}); err != nil {
				return err
			}

			log.Debug("entering transmission mode")

			if optionHeader.ID == NEGOTIATION_ID_OPTION_GO {
				break nego
			}
		case NEGOTIATION_ID_OPTION_ABORT:
			if err := binary.Write(conn, binary.BigEndian, NegotiationReplyHeader{
				ReplyMagic: NEGOTIATION_MAGIC_REPLY,
				ID:         optionHeader.ID,
				Type:       NEGOTIATION_TYPE_REPLY_ACK,
				Length:     0,
			}); err != nil {
				return err
			}

			return nil
		case NEGOTIATION_ID_OPTION_LIST:
			{
				info := &bytes.Buffer{}

				for _, export := range exports {
					exportName := []byte(export.Name)

					if err := binary.Write(info, binary.BigEndian, uint32(len(exportName))); err != nil {
						return err
					}

					if err := binary.Write(info, binary.BigEndian, exportName); err != nil {
						return err
					}
				}

				if err := binary.Write(conn, binary.BigEndian, NegotiationReplyHeader{
					ReplyMagic: NEGOTIATION_MAGIC_REPLY,
					ID:         optionHeader.ID,
					Type:       NEGOTIATION_TYPE_REPLY_SERVER,
					Length:     uint32(info.Len()),
				}); err != nil {
					return err
				}

				if _, err := io.Copy(conn, info); err != nil {
					return err
				}
			}

			if err := binary.Write(conn, binary.BigEndian, NegotiationReplyHeader{
				ReplyMagic: NEGOTIATION_MAGIC_REPLY,
				ID:         optionHeader.ID,
				Type:       NEGOTIATION_TYPE_REPLY_ACK,
				Length:     0,
			}); err != nil {
				return err
			}
		default:
			_, err := io.CopyN(io.Discard, conn, int64(optionHeader.Length)) // Discard the unknown option's data
			if err != nil {
				return err
			}

			if err := binary.Write(conn, binary.BigEndian, NegotiationReplyHeader{
				ReplyMagic: NEGOTIATION_MAGIC_REPLY,
				ID:         optionHeader.ID,
				Type:       NEGOTIATION_TYPE_REPLY_ERR_UNSUPPORTED,
				Length:     0,
			}); err != nil {
				return err
			}
		}
	}

	return HandleTransport(log, conn, backend, options)
}

func HandleTransport(log *slog.Logger, conn net.Conn, backend Backend, options *Options) error {
	// We used to use filer here to get the File back out, but it seems to break polling?
	// It's very odd so for now we allow the caller to pass the file in, which they probably
	// already have because we use nbdnl.Lookback() which creates files first anyway.
	sc := options.RawFile
	if sc != nil {
		log.Info("enabling use of sendfile(2) for reads")
	}

	// Transmission
	b := []byte{}

	e := binary.BigEndian

	request := make([]byte, binary.Size(TransmissionRequestHeader{}))
	reply := make([]byte, binary.Size(TransmissionReplyHeader{}))
	e.PutUint32(reply, TRANSMISSION_MAGIC_REPLY)

	sendReply := func(e uint32, h uint64) error {
		binary.BigEndian.PutUint32(reply[4:], e)
		binary.BigEndian.PutUint64(reply[8:], h)
		_, err := conn.Write(reply)
		return err
	}

	lastTick := time.Now()

	for {
		now := time.Now()

		if now.Sub(lastTick) >= 10*time.Second {
			backend.Tick()

			now = time.Now()
			lastTick = now
		}

		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))

		if _, err := io.ReadFull(conn, request); err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				continue
			}

			return err
		}

		conn.SetReadDeadline(time.Time{})

		magic := e.Uint32(request)
		// flags := e.Uint16(request[4:])
		typ := e.Uint16(request[6:])
		handle := e.Uint64(request[8:])
		offset := e.Uint64(request[16:])
		length := e.Uint32(request[24:])

		if magic != TRANSMISSION_MAGIC_REQUEST {
			return ErrInvalidMagic
		}

		if typ != TRANSMISSION_TYPE_REQUEST_TRIM {
			if length > defaultMaximumRequestSize {
				return ErrInvalidBlocksize
			}

			if length > uint32(len(b)) {
				b = make([]byte, length)
			}
		}

		switch typ {
		case TRANSMISSION_TYPE_REQUEST_READ:
			if err := sendReply(0, handle); err != nil {
				return err
			}

			if sc != nil {
				backend.ReadIntoConn(b[:length], int64(offset), sc)
			} else {
				for left := length; left > 0; {
					n, err := backend.ReadAt(b[:left], int64(offset))
					if err != nil {
						return err
					}

					if _, err := conn.Write(b[:n]); err != nil {
						return err
					}

					b = b[n:]
					left -= uint32(n)
				}
			}
		case TRANSMISSION_TYPE_REQUEST_WRITE:
			if options.ReadOnly {
				_, err := io.CopyN(io.Discard, conn, int64(length)) // Discard the write command's data
				if err != nil {
					return err
				}

				if err := sendReply(TRANSMISSION_ERROR_EPERM, handle); err != nil {
					return err
				}

				break
			}

			n, err := io.ReadAtLeast(conn, b[:length], int(length))
			if err != nil {
				return err
			}

			if _, err := backend.WriteAt(b[:n], int64(offset)); err != nil {
				return err
			}

			if err := sendReply(0, handle); err != nil {
				return err
			}
		case TRANSMISSION_TYPE_REQUEST_WRITEZ:
			if options.ReadOnly {
				_, err := io.CopyN(io.Discard, conn, int64(length)) // Discard the write command's data
				if err != nil {
					return err
				}

				if err := sendReply(TRANSMISSION_ERROR_EPERM, handle); err != nil {
					return err
				}

				break
			}

			if err := backend.ZeroAt(int64(offset), int64(length)); err != nil {
				return err
			}

			if err := sendReply(0, handle); err != nil {
				return err
			}
		case TRANSMISSION_TYPE_REQUEST_TRIM:
			if options.ReadOnly {
				_, err := io.CopyN(io.Discard, conn, int64(length)) // Discard the write command's data
				if err != nil {
					return err
				}

				if err := sendReply(TRANSMISSION_ERROR_EPERM, handle); err != nil {
					return err
				}

				break
			}

			if err := backend.Trim(int64(offset), int64(length)); err != nil {
				return err
			}

			if err := sendReply(0, handle); err != nil {
				return err
			}
		case TRANSMISSION_TYPE_REQUEST_FLUSH:
			if !options.ReadOnly {
				if err := backend.Sync(); err != nil {
					return err
				}
			}

			if err := sendReply(0, handle); err != nil {
				return err
			}
		case TRANSMISSION_TYPE_REQUEST_DISC:
			if !options.ReadOnly {
				if err := backend.Sync(); err != nil {
					return err
				}
			}

			return nil
		default:
			log.Warn("unknown request type", "type", typ)

			_, err := io.CopyN(io.Discard, conn, int64(length)) // Discard the unknown command's data
			if err != nil {
				return err
			}

			if err := sendReply(TRANSMISSION_ERROR_EINVAL, handle); err != nil {
				return err
			}
		}
	}
}

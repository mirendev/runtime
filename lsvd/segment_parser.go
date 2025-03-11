package lsvd

import (
	"bufio"
	"io"
)

func ReadSegmentHeaders(r io.Reader) (SegmentHeader, []ExtentHeader, error) {
	br := bufio.NewReader(r)

	var hdr SegmentHeader

	err := hdr.Read(br)
	if err != nil {
		return hdr, nil, err
	}

	var headers []ExtentHeader

	for i := uint32(0); i < hdr.ExtentCount; i++ {
		var eh ExtentHeader

		_, err := eh.Read(br)
		if err != nil {
			return hdr, nil, err
		}

		headers = append(headers, eh)
	}

	return hdr, headers, nil
}

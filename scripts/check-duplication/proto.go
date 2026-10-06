package main

import (
	"errors"
	"fmt"
)

const (
	wireVarint    = 0
	wireFixed64   = 1
	wireBytes     = 2
	wireFixed32   = 5
	maxFieldBytes = 1 << 26
)

type field struct {
	number int
	wire   int
	value  uint64
	data   []byte
}

var errTruncated = errors.New("truncated protobuf data")

func readVarint(data []byte, pos int) (uint64, int, error) {
	var value uint64
	for shift := uint(0); shift < 64; shift += 7 {
		if pos >= len(data) {
			return 0, pos, errTruncated
		}
		b := data[pos]
		pos++
		value |= uint64(b&0x7f) << shift
		if b < 0x80 {
			return value, pos, nil
		}
	}
	return 0, pos, errors.New("varint overflows 64 bits")
}

func decodeMessage(data []byte) ([]field, error) {
	var fields []field
	for pos := 0; pos < len(data); {
		key, next, err := readVarint(data, pos)
		if err != nil {
			return nil, err
		}
		pos = next
		item := field{number: int(key >> 3), wire: int(key & 7)}
		switch item.wire {
		case wireVarint:
			item.value, pos, err = readVarint(data, pos)
		case wireFixed64:
			pos, err = skip(data, pos, 8)
		case wireFixed32:
			pos, err = skip(data, pos, 4)
		case wireBytes:
			item.data, pos, err = readBytes(data, pos)
		default:
			err = fmt.Errorf("field %d has wire type %d, which no report carries", item.number, item.wire)
		}
		if err != nil {
			return nil, err
		}
		fields = append(fields, item)
	}
	return fields, nil
}

func skip(data []byte, pos, size int) (int, error) {
	if pos+size > len(data) {
		return pos, errTruncated
	}
	return pos + size, nil
}

func readBytes(data []byte, pos int) ([]byte, int, error) {
	length, next, err := readVarint(data, pos)
	if err != nil {
		return nil, pos, err
	}
	if length > maxFieldBytes || next+int(length) > len(data) {
		return nil, pos, errTruncated
	}
	return data[next : next+int(length)], next + int(length), nil
}

func decodeDelimited(data []byte) ([][]byte, error) {
	var messages [][]byte
	for pos := 0; pos < len(data); {
		message, next, err := readBytes(data, pos)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
		pos = next
	}
	return messages, nil
}

func packedVarints(data []byte) ([]uint64, error) {
	var values []uint64
	for pos := 0; pos < len(data); {
		value, next, err := readVarint(data, pos)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
		pos = next
	}
	return values, nil
}

func expectWire(item field, wire int, record string) error {
	if item.wire != wire {
		return fmt.Errorf("%s field %d has wire type %d where the layout expects %d", record, item.number, item.wire, wire)
	}
	return nil
}

func varintField(item field, record string) (int, error) {
	if err := expectWire(item, wireVarint, record); err != nil {
		return 0, err
	}
	return int(item.value), nil
}

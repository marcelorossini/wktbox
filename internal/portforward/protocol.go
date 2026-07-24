package portforward

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	TokenSize       = 32
	maxMappingName  = 63
	protocolVersion = 1
	responseOK      = 0
	responseError   = 1
)

var protocolMagic = []byte{'W', 'K', 'T', 'P'}

func WritePreamble(destination io.Writer, token []byte, name string) error {
	if len(token) != TokenSize {
		return fmt.Errorf("relay token must contain exactly %d bytes", TokenSize)
	}
	if len(name) == 0 || len(name) > maxMappingName {
		return fmt.Errorf(
			"relay mapping name must contain from 1 to %d bytes",
			maxMappingName,
		)
	}
	body := make([]byte, 0, len(protocolMagic)+2+TokenSize+len(name))
	body = append(body, protocolMagic...)
	body = append(body, protocolVersion, byte(len(name)))
	body = append(body, token...)
	body = append(body, name...)
	if _, err := destination.Write(body); err != nil {
		return fmt.Errorf("write relay preamble: %w", err)
	}
	return nil
}

func ReadPreamble(source io.Reader) ([]byte, string, error) {
	header := make([]byte, len(protocolMagic)+2+TokenSize)
	if _, err := io.ReadFull(source, header); err != nil {
		return nil, "", fmt.Errorf("read relay preamble: %w", err)
	}
	if !bytes.Equal(header[:len(protocolMagic)], protocolMagic) {
		return nil, "", errors.New("invalid relay protocol magic")
	}
	if header[len(protocolMagic)] != protocolVersion {
		return nil, "", fmt.Errorf(
			"unsupported relay protocol version %d",
			header[len(protocolMagic)],
		)
	}
	nameLength := int(header[len(protocolMagic)+1])
	if nameLength < 1 || nameLength > maxMappingName {
		return nil, "", fmt.Errorf("invalid relay mapping name length %d", nameLength)
	}
	tokenStart := len(protocolMagic) + 2
	token := append([]byte(nil), header[tokenStart:tokenStart+TokenSize]...)
	name := make([]byte, nameLength)
	if _, err := io.ReadFull(source, name); err != nil {
		return nil, "", fmt.Errorf("read relay mapping name: %w", err)
	}
	return token, string(name), nil
}

func writeRelayResponse(destination io.Writer, status byte, message string) error {
	if len(message) > 65535 {
		message = message[:65535]
	}
	header := []byte{status, 0, 0}
	binary.BigEndian.PutUint16(header[1:], uint16(len(message)))
	if _, err := destination.Write(header); err != nil {
		return err
	}
	if message != "" {
		_, err := io.WriteString(destination, message)
		return err
	}
	return nil
}

func readRelayResponse(source io.Reader) error {
	header := make([]byte, 3)
	if _, err := io.ReadFull(source, header); err != nil {
		return fmt.Errorf("read relay response: %w", err)
	}
	message := make([]byte, int(binary.BigEndian.Uint16(header[1:])))
	if _, err := io.ReadFull(source, message); err != nil {
		return fmt.Errorf("read relay response message: %w", err)
	}
	if header[0] != responseOK {
		if len(message) == 0 {
			return errors.New("relay rejected connection")
		}
		return errors.New(string(message))
	}
	return nil
}

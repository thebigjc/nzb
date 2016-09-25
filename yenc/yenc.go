package yenc

import (
	"bufio"
	"bytes"
	"hash"
	"hash/crc32"
	"io"
	"log"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

type Yenc struct {
	begin   int
	end     int
	part    int
	total   int
	pcrc32  uint32
	crc32   uint32
	line    int
	Size    int
	EndSize int
	endPart int
	Name    string
	crcHash hash.Hash32
	body    []byte
}

func mustParse(s string) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		log.Fatalln("Failed to parse int", s, err)
	}

	return v
}

func mustParseHex(s string) uint32 {
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		log.Fatalln("Failed to parse int", s, err)
	}

	return uint32(v)
}

func (y *Yenc) parseYBegin(line string) {
	splits := strings.Split(line, " ")
	for _, split := range splits {
		kv := strings.SplitN(split, "=", 2)
		k, v := kv[0], kv[1]
		switch k {
		case "part":
			y.part = mustParse(v)
		case "line":
			y.line = mustParse(v)
		case "total":
			y.total = mustParse(v)
		case "size":
			y.Size = mustParse(v)
		case "name":
			y.Name = v
		}
	}
}

func (y *Yenc) parseYPart(line string) {
	splits := strings.Split(line, " ")
	for _, split := range splits {
		kv := strings.SplitN(split, "=", 2)
		if len(kv) < 2 {
			continue
		}

		k, v := kv[0], kv[1]
		switch k {
		case "begin":
			y.begin = mustParse(v)
		case "end":
			y.end = mustParse(v)
		}
	}
}

func (y *Yenc) parseYEnd(line string) {
	splits := strings.Split(line, " ")
	for _, split := range splits {
		kv := strings.SplitN(split, "=", 2)
		if len(kv) < 2 {
			continue
		}

		k, v := kv[0], kv[1]
		switch k {
		case "size":
			y.EndSize = mustParse(v)
		case "part":
			y.endPart = mustParse(v)
		case "pcrc32":
			y.pcrc32 = mustParseHex(v)
		case "crc32":
			y.crc32 = mustParseHex(v)

		}
	}
}

func ReadYenc(r io.Reader) (*Yenc, error) {
	var lines []byte
	var y Yenc
	y.crcHash = crc32.NewIEEE()
	outside := true

	br := bufio.NewReader(r)

	for {
		line, err := br.ReadBytes('\n')

		if err == io.EOF {
			y.body = lines
			return &y, nil
		}

		if err != nil {
			return nil, err
		}

		line = bytes.TrimRight(line, "\r\n")

		if len(line) == 0 {
			continue
		}

		if line[0] == '=' {
			lineStr := string(line)
			switch {
			case strings.HasPrefix(lineStr, "=ybegin "):
				y.parseYBegin(lineStr)
				continue
			case strings.HasPrefix(lineStr, "=ypart "):
				y.parseYPart(lineStr)
				outside = false
				continue
			case strings.HasPrefix(lineStr, "=yend "):
				y.parseYEnd(lineStr)
				outside = true
				continue
			}
		}

		if outside {
			continue
		}

		line = y.decode(line)
		y.crcHash.Write(line)
		lines = append(lines, line...)
	}
}

var crlf = []byte{'\r', '\n'}

func (y *Yenc) decode(line []byte) []byte {
	i, j := 0, 0
	escaped := false
	for ; i < len(line); i, j = i+1, j+1 {
		switch {
		case escaped:
			line[j] = (((line[i] - 42) & 255) - 64) & 255
			escaped = false
		case line[i] == '=':
			escaped = true
			j--
		case line[i] == '\n':
			log.Fatalf("LF")
		case line[i] == '\r':
			log.Fatalf("CR")
		default:
			line[j] = (line[i] - 42) & 255
		}
	}

	return line[:len(line)-(i-j)]
}

func (y *Yenc) SaveBody(w io.WriterAt) error {
	sum := y.crcHash.Sum32()

	if y.EndSize != len(y.body) {
		return errors.Errorf("Body size doesn't match. Expected:%d, got %d", y.EndSize, len(y.body))
	}

	if y.pcrc32 != sum {
		return errors.Errorf("Part crc32 doesn't match %d != %d", y.pcrc32, sum)
	}

	_, err := w.WriteAt(y.body, int64(y.begin-1))

	return err
}

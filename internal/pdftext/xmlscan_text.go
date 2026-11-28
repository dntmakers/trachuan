package pdftext

import (
	"bytes"
	"context"
	"strconv"
	"unicode/utf8"
)

func validXMLRune(r rune) bool {
	return r == 9 || r == 10 || r == 13 || r >= 0x20 && r <= 0xd7ff || r >= 0xe000 && r <= 0xfffd || r >= 0x10000 && r <= 0x10ffff
}

func scanXMLText(ctx context.Context, out *bytes.Buffer, part []byte, word, refs, replace bool) (int, error) {
	count := 0
	nextCheck := 0
	for i := 0; i < len(part); {
		if i >= nextCheck {
			if ctx.Err() != nil {
				return 0, ErrFile
			}
			nextCheck = i + 4096
		}
		r, size := utf8.DecodeRune(part[i:])
		if r == utf8.RuneError && size == 1 {
			return 0, ErrFile
		}
		if r == 7 {
			if !word || !replace {
				return 0, ErrFile
			}
			out.WriteRune('�')
			count++
			i += size
			continue
		}
		if !validXMLRune(r) {
			return 0, ErrFile
		}
		if refs && r == '&' {
			j := i + 1
			for j < len(part) && part[j] != ';' {
				c := part[j]
				validChar := c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '#'
				if !validChar {
					return 0, ErrFile
				}
				j++
				if j%4096 == 0 && ctx.Err() != nil {
					return 0, ErrFile
				}
			}
			if j == len(part) {
				return 0, ErrFile
			}
			ref := string(part[i+1 : j])
			switch ref {
			case "amp", "lt", "gt", "apos", "quot":
			default:
				if len(ref) < 2 || ref[0] != '#' {
					return 0, ErrFile
				}
				number := ref[1:]
				base := 10
				if len(number) > 0 && number[0] == 'x' {
					base = 16
					number = number[1:]
				}
				n, err := strconv.ParseUint(number, base, 32)
				if err != nil || !validXMLRune(rune(n)) {
					return 0, ErrFile
				}
			}
			out.Write(part[i : j+1])
			i = j + 1
			continue
		}
		out.Write(part[i : i+size])
		i += size
	}
	return count, nil
}

package pdftext

import (
	"bytes"
	"context"
	"strings"
)

func scanFind(ctx context.Context, data, needle []byte) (int, error) {
	for i := 0; i+len(needle) <= len(data); i++ {
		if i%4096 == 0 && ctx.Err() != nil {
			return -1, ErrFile
		}
		if bytes.Equal(data[i:i+len(needle)], needle) {
			return i, nil
		}
	}
	return -1, nil
}

func scanBBox(ctx context.Context, data []byte, allowReplacement bool) ([]byte, int, error) {
	var output bytes.Buffer
	var stack []string
	count := 0
	write := func(part []byte, word, refs bool) error {
		n, err := scanXMLText(ctx, &output, part, word, refs, allowReplacement)
		if err != nil {
			return err
		}
		count += n
		return nil
	}
	for i := 0; i < len(data); {
		if ctx.Err() != nil {
			return nil, 0, ErrFile
		}
		if data[i] != '<' {
			end, err := scanFind(ctx, data[i:], []byte("<"))
			if err != nil {
				return nil, 0, err
			}
			if end < 0 {
				end = len(data) - i
			}
			word := len(stack) > 0 && stack[len(stack)-1] == "word"
			if err := write(data[i:i+end], word, true); err != nil {
				return nil, 0, err
			}
			i += end
			continue
		}
		if bytes.HasPrefix(data[i:], []byte("<![CDATA[")) {
			end, err := scanFind(ctx, data[i+9:], []byte("]]>"))
			if err != nil {
				return nil, 0, err
			}
			if end < 0 {
				return nil, 0, ErrFile
			}
			end += i + 9
			if err := write(data[i:i+9], false, false); err != nil {
				return nil, 0, err
			}
			word := len(stack) > 0 && stack[len(stack)-1] == "word"
			if err := write(data[i+9:end], word, false); err != nil {
				return nil, 0, err
			}
			if err := write(data[end:end+3], false, false); err != nil {
				return nil, 0, err
			}
			i = end + 3
			continue
		}
		if bytes.HasPrefix(data[i:], []byte("<!--")) {
			end, err := scanFind(ctx, data[i+4:], []byte("-->"))
			if err != nil {
				return nil, 0, err
			}
			if end < 0 {
				return nil, 0, ErrFile
			}
			end += i + 7
			if err := write(data[i:end], false, false); err != nil {
				return nil, 0, err
			}
			i = end
			continue
		}
		end := i + 1
		var quote byte
		for ; end < len(data); end++ {
			if (end-i)%4096 == 0 && ctx.Err() != nil {
				return nil, 0, ErrFile
			}
			c := data[end]
			if quote != 0 {
				if c == quote {
					quote = 0
				}
				continue
			}
			if c == '\'' || c == '"' {
				quote = c
				continue
			}
			if c == '>' {
				break
			}
		}
		if end >= len(data) {
			return nil, 0, ErrFile
		}
		tag := data[i : end+1]
		if err := write(tag, false, true); err != nil {
			return nil, 0, err
		}
		text := string(tag[1 : len(tag)-1])
		if !strings.HasPrefix(text, "!") && !strings.HasPrefix(text, "?") {
			closing := strings.HasPrefix(text, "/")
			if closing {
				text = text[1:]
			}
			name := strings.Fields(strings.TrimSuffix(text, "/"))
			if len(name) == 0 {
				return nil, 0, ErrFile
			}
			local := name[0]
			if j := strings.LastIndexByte(local, ':'); j >= 0 {
				local = local[j+1:]
			}
			if closing {
				if len(stack) == 0 {
					return nil, 0, ErrFile
				}
				stack = stack[:len(stack)-1]
			} else if !strings.HasSuffix(strings.TrimSpace(text), "/") {
				if len(stack) >= 64 {
					return nil, 0, ErrFile
				}
				stack = append(stack, local)
			}
		}
		i = end + 1
	}
	return output.Bytes(), count, nil
}

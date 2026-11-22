package classify

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

var ErrOpenAISchemaLimit = errors.New("OpenAI schema limit exceeded")

func openAIArticleLimit(articles []Article) error {
	if len(articles) > 1250 {
		return ErrOpenAISchemaLimit
	}
	size := 0
	for _, article := range articles {
		if !utf8.ValidString(article.Anchor) {
			return fmt.Errorf("invalid article anchor UTF-8")
		}
		size += utf8.RuneCountInString(article.Anchor) + len("codescodeconfidence")
	}
	if size > 120000 {
		return ErrOpenAISchemaLimit
	}
	return nil
}

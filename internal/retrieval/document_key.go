package retrieval

import "danny.vn/trachuan/internal/lawkey"

func ValidDocumentKey(key string) bool { return lawkey.DocumentKey(key) }

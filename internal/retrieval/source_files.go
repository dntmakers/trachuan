package retrieval

import (
	"context"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
)

type SourceFile struct {
	Name      string     `json:"name"`
	URL       *string    `json:"url"`
	Hash      string     `json:"hash"`
	Size      int64      `json:"size"`
	Role      string     `json:"role"`
	FetchedAt *time.Time `json:"fetched_at"`
}

func publicSourceURL(source string, raw *string) bool {
	if source != "chinhphu" || raw == nil || strings.ContainsAny(*raw, "?#\\\\") || strings.IndexFunc(*raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return false
	}
	u, err := url.Parse(*raw)
	if err != nil || strings.IndexFunc(u.Host, func(r rune) bool { return r > 127 }) >= 0 {
		return false
	}
	_, authorityPath, ok := strings.Cut(*raw, "://")
	pathAt := strings.IndexByte(authorityPath, '/')
	if !ok || pathAt < 0 || !validSourcePath(authorityPath[pathAt:]) {
		return false
	}
	return strings.EqualFold(u.Scheme, "https") && strings.EqualFold(u.Hostname(), "datafiles.chinhphu.vn") && (u.Port() == "" || u.Port() == "443") && u.User == nil && strings.HasPrefix(u.Path, "/") && u.Path != "" && !strings.HasSuffix(u.Host, ":")
}

func validSourcePath(path string) bool {
	for i := 0; i < len(path); i++ {
		b := path[i]
		if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.ContainsRune("-._~!$&'()*+,;=:@/", rune(b)) {
			continue
		}
		if b == '%' && i+2 < len(path) && isHex(path[i+1]) && isHex(path[i+2]) {
			i += 2
			continue
		}
		return false
	}
	return true
}

func isHex(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F'
}

func readSourceFiles(ctx context.Context, tx pgx.Tx, documentID int64) ([]SourceFile, int, error) {
	rows, err := tx.Query(ctx, `SELECT source,name,url,hash,size,role,fetched_at,count(*) OVER () FROM source_file WHERE document_id=$1 ORDER BY name,hash LIMIT 21`, documentID)
	if err != nil {
		return nil, 0, databaseFailure("query source files", err)
	}
	defer rows.Close()
	files := []SourceFile{}
	total := 0
	for rows.Next() {
		var file SourceFile
		var source string
		if err = rows.Scan(&source, &file.Name, &file.URL, &file.Hash, &file.Size, &file.Role, &file.FetchedAt, &total); err != nil {
			return nil, 0, databaseFailure("scan source file", err)
		}
		if !publicSourceURL(source, file.URL) {
			file.URL = nil
		}
		files = append(files, file)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, databaseFailure("read source files", err)
	}
	return files, total, nil
}

package retrieval

import "github.com/jackc/pgx/v5/pgxpool"

func (e *Engine) WithPool(pool *pgxpool.Pool) *Engine {
	copy := *e
	copy.db = pool
	return &copy
}

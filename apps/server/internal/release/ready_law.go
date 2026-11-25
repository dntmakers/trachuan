package release

import "strings"

func (r *Resolver) ReadyLaw(database string) *Lease {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.entries[database]
	if r.closed || r.active["vn"] != database || entry == nil || entry.opening || entry.retired || entry.engine == nil {
		return nil
	}
	return r.leaseLocked(entry, strings.TrimPrefix(database, "corpus_vn_"), r.generation)
}

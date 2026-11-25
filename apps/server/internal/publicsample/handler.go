package publicsample

import (
	"errors"
	"io"
	"net/http"

	"danny.vn/trachuan/apps/server/internal/apierror"
	"danny.vn/trachuan/apps/server/internal/publicsummary"
)

type Handler struct {
	enabled bool
	gate    *publicsummary.Admission
	flights *flights
}

func NewHandler(enabled bool, gate *publicsummary.Admission) *Handler {
	return &Handler{enabled: enabled, gate: gate}
}

func (h *Handler) WithFlights(f *Flights) *Handler {
	h.flights = f
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery || hasBody(r) {
		apierror.Write(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if !h.enabled {
		apierror.Write(w, http.StatusNotFound, "not_found")
		return
	}
	if !h.gate.TrySample() {
		w.Header().Set("Retry-After", "5")
		apierror.Write(w, http.StatusServiceUnavailable, "busy")
		return
	}
	defer h.gate.DoneSample()
	if h.flights == nil {
		apierror.Write(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	body, date, err := h.flights.Get(r.Context())
	if errors.Is(err, errBusy) {
		w.Header().Set("Retry-After", "5")
		apierror.Write(w, http.StatusServiceUnavailable, "busy")
		return
	}
	if err != nil {
		apierror.Write(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Sample-Status-Date", date)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func hasBody(r *http.Request) bool {
	if r.Body == nil || r.Body == http.NoBody {
		return false
	}
	if r.ContentLength > 0 {
		return true
	}
	var b [1]byte
	n, err := r.Body.Read(b[:])
	return n > 0 || err != io.EOF
}

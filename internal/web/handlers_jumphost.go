package web

import (
	"errors"
	"net/http"

	"pantau/internal/store"
)

// writeHostStoreError answers a failed host write. A rejected bastion choice is the caller's mistake (400);
// anything else is a server fault (500).
func writeHostStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrJumpHostInvalid) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

package utility

import (
	"encoding/json"
	"net/http"
)

func JSONError(w http.ResponseWriter, status int, m string) {
	w.Header().Set("Content-type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": m,
	})

}
